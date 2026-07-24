package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct {
	db   *sql.DB
	path string
}

func OpenStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &Store{db: db, path: filepath.Clean(path)}
	if err := store.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Path() string {
	return s.path
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`PRAGMA foreign_keys = ON;
CREATE TABLE IF NOT EXISTS directories (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT NOT NULL UNIQUE,
  name TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'idle',
  last_scanned_at TEXT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS creators (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  directory_id INTEGER NOT NULL,
  name TEXT NOT NULL,
  work_count INTEGER NOT NULL DEFAULT 0,
  bilibili_uid TEXT NOT NULL DEFAULT '',
  bilibili_sec_uid TEXT NOT NULL DEFAULT '',
  bilibili_profile_url TEXT NOT NULL DEFAULT '',
  online_identity_status TEXT NOT NULL DEFAULT 'missing',
  avatar_url TEXT NOT NULL DEFAULT '',
  avatar_file TEXT NOT NULL DEFAULT '',
  bio TEXT NOT NULL DEFAULT '',
  profile_updated_at TEXT NOT NULL DEFAULT '',
  search_text TEXT NOT NULL DEFAULT '',
  UNIQUE(directory_id, name),
  FOREIGN KEY(directory_id) REFERENCES directories(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS works (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  directory_id INTEGER NOT NULL,
  creator_id INTEGER NOT NULL,
  aweme_id TEXT NOT NULL,
  work_type TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  music_title TEXT NOT NULL DEFAULT '',
  source_url TEXT NOT NULL DEFAULT '',
  cover_url TEXT NOT NULL DEFAULT '',
  cover_file TEXT NOT NULL DEFAULT '',
  tags_json TEXT NOT NULL DEFAULT '[]',
  published_at TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  search_text TEXT NOT NULL DEFAULT '',
  UNIQUE(directory_id, creator_id, aweme_id),
  FOREIGN KEY(directory_id) REFERENCES directories(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS works_creator_published_idx ON works(creator_id, published_at);
CREATE TABLE IF NOT EXISTS media (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  work_id INTEGER NOT NULL,
  type TEXT NOT NULL,
  file_path TEXT NOT NULL UNIQUE,
  file_name TEXT NOT NULL,
  ordinal INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  username TEXT NOT NULL UNIQUE,
  password_salt TEXT NOT NULL,
  password_hash TEXT NOT NULL,
  password_iterations INTEGER NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0,
  role TEXT NOT NULL DEFAULT 'user',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL,
  token_hash TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  expires_at TEXT NOT NULL,
  last_used_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id);
CREATE TABLE IF NOT EXISTS favorite_folders (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL DEFAULT 0,
  type TEXT NOT NULL CHECK(type IN ('work', 'creator')),
  name TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS work_favorites (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  folder_id INTEGER NOT NULL,
  work_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(folder_id, work_id),
  FOREIGN KEY(folder_id) REFERENCES favorite_folders(id) ON DELETE CASCADE,
  FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS creator_favorites (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  folder_id INTEGER NOT NULL,
  creator_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(folder_id, creator_id),
  FOREIGN KEY(folder_id) REFERENCES favorite_folders(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS update_queues (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  cron TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  next_run_at TEXT NOT NULL DEFAULT '',
  last_run_summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS update_queues_one_default ON update_queues(is_default) WHERE is_default=1;
CREATE TABLE IF NOT EXISTS update_queue_creators (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_id INTEGER NOT NULL,
  creator_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(queue_id, creator_id),
  FOREIGN KEY(queue_id) REFERENCES update_queues(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS update_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_id INTEGER NOT NULL,
  queue_name TEXT NOT NULL,
  enqueued_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at TEXT NOT NULL DEFAULT '',
  finished_at TEXT NOT NULL DEFAULT '',
  trigger_source TEXT NOT NULL DEFAULT 'manual',
  total_creators INTEGER NOT NULL DEFAULT 0,
  succeeded_creators INTEGER NOT NULL DEFAULT 0,
  failed_creators INTEGER NOT NULL DEFAULT 0,
  added_works INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'queued',
  summary TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(queue_id) REFERENCES update_queues(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS update_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  creator_id INTEGER NOT NULL,
  queue_id INTEGER NOT NULL DEFAULT 0,
  run_id INTEGER NOT NULL DEFAULT 0,
  retry_of_job_id INTEGER NOT NULL DEFAULT 0,
  attempt INTEGER NOT NULL DEFAULT 1,
  status TEXT NOT NULL DEFAULT 'pending',
  trigger_source TEXT NOT NULL DEFAULT 'manual',
  added_works INTEGER NOT NULL DEFAULT 0,
  total_works INTEGER NOT NULL DEFAULT 0,
  processed_works INTEGER NOT NULL DEFAULT 0,
  downloaded_media INTEGER NOT NULL DEFAULT 0,
  skipped_works INTEGER NOT NULL DEFAULT 0,
  download_speed_bps INTEGER NOT NULL DEFAULT 0,
  progress_message TEXT NOT NULL DEFAULT '',
  download_directory TEXT NOT NULL DEFAULT '',
  download_notice TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at TEXT NOT NULL DEFAULT '',
  finished_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS update_jobs_status_idx ON update_jobs(status, id);
CREATE TABLE IF NOT EXISTS update_cookie_settings (
  id INTEGER PRIMARY KEY CHECK(id=1),
  cookie_value TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL DEFAULT '',
  validation_status TEXT NOT NULL DEFAULT 'unknown',
  validation_message TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS subtitle_records (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  work_id INTEGER NOT NULL UNIQUE,
  media_id INTEGER NOT NULL DEFAULT 0,
  creator_id INTEGER NOT NULL,
  text_path TEXT NOT NULL DEFAULT '',
  srt_path TEXT NOT NULL DEFAULT '',
  json_path TEXT NOT NULL DEFAULT '',
  token TEXT NOT NULL UNIQUE,
  status TEXT NOT NULL DEFAULT 'ready',
  source TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS subtitle_records_creator_idx ON subtitle_records(creator_id, status);
CREATE TABLE IF NOT EXISTS subtitle_queues (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  name TEXT NOT NULL UNIQUE,
  cron TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  enabled INTEGER NOT NULL DEFAULT 1,
  next_run_at TEXT NOT NULL DEFAULT '',
  last_run_summary TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS subtitle_queues_one_default ON subtitle_queues(is_default) WHERE is_default=1;
CREATE TABLE IF NOT EXISTS subtitle_queue_creators (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_id INTEGER NOT NULL,
  creator_id INTEGER NOT NULL,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE(queue_id, creator_id),
  FOREIGN KEY(queue_id) REFERENCES subtitle_queues(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS subtitle_runs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  queue_id INTEGER NOT NULL,
  queue_name TEXT NOT NULL,
  enqueued_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  total_creators INTEGER NOT NULL DEFAULT 0,
  succeeded_creators INTEGER NOT NULL DEFAULT 0,
  failed_creators INTEGER NOT NULL DEFAULT 0,
  subtitle_works INTEGER NOT NULL DEFAULT 0,
  status TEXT NOT NULL DEFAULT 'queued',
  summary TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(queue_id) REFERENCES subtitle_queues(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS subtitle_jobs (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  creator_id INTEGER NOT NULL,
  work_id INTEGER NOT NULL DEFAULT 0,
  queue_id INTEGER NOT NULL DEFAULT 0,
  run_id INTEGER NOT NULL DEFAULT 0,
  retry_of_job_id INTEGER NOT NULL DEFAULT 0,
  attempt INTEGER NOT NULL DEFAULT 1,
  source TEXT NOT NULL DEFAULT 'manual',
  status TEXT NOT NULL DEFAULT 'pending',
  total_works INTEGER NOT NULL DEFAULT 0,
  processed_works INTEGER NOT NULL DEFAULT 0,
  subtitle_count INTEGER NOT NULL DEFAULT 0,
  skipped_works INTEGER NOT NULL DEFAULT 0,
  progress_message TEXT NOT NULL DEFAULT '',
  output_directory TEXT NOT NULL DEFAULT '',
  error TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  started_at TEXT NOT NULL DEFAULT '',
  finished_at TEXT NOT NULL DEFAULT '',
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS subtitle_jobs_status_idx ON subtitle_jobs(status, id);
CREATE TABLE IF NOT EXISTS creator_link_groups (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS creator_link_members (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  group_id INTEGER NOT NULL,
  creator_id INTEGER NOT NULL UNIQUE,
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(group_id) REFERENCES creator_link_groups(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE TABLE IF NOT EXISTS work_view_history (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id INTEGER NOT NULL DEFAULT 0,
  work_id INTEGER NOT NULL,
  creator_id INTEGER NOT NULL,
  event_type TEXT NOT NULL DEFAULT 'view',
  position_sec REAL NOT NULL DEFAULT 0,
  duration_sec REAL NOT NULL DEFAULT 0,
  media_type TEXT NOT NULL DEFAULT '',
  detail_text TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
  FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE,
  FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS work_view_history_work_idx ON work_view_history(work_id, created_at);
CREATE INDEX IF NOT EXISTS work_view_history_creator_idx ON work_view_history(creator_id, created_at);
CREATE INDEX IF NOT EXISTS work_view_history_creator_event_idx ON work_view_history(creator_id, event_type);
CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  actor_user_id INTEGER NOT NULL DEFAULT 0,
  actor_name TEXT NOT NULL DEFAULT '',
  event_type TEXT NOT NULL,
  target_type TEXT NOT NULL DEFAULT '',
  target_id INTEGER NOT NULL DEFAULT 0,
  message TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE TABLE IF NOT EXISTS system_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  event_type TEXT NOT NULL,
  severity TEXT NOT NULL DEFAULT 'info',
  message TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
INSERT INTO update_queues(name, cron, is_default, enabled, next_run_at, created_at, updated_at)
SELECT '默认更新队列', '0 3 * * *', 1, 1, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM update_queues);
INSERT INTO update_cookie_settings(id, validation_status)
SELECT 1, 'unknown' WHERE NOT EXISTS (SELECT 1 FROM update_cookie_settings WHERE id=1);
INSERT INTO subtitle_queues(name, cron, is_default, enabled, next_run_at, created_at, updated_at)
SELECT '默认字幕队列', '30 3 * * *', 1, 1, '', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP
WHERE NOT EXISTS (SELECT 1 FROM subtitle_queues);
`)
	if err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		def  string
	}{
		{"bilibili_uid", "TEXT NOT NULL DEFAULT ''"},
		{"bilibili_sec_uid", "TEXT NOT NULL DEFAULT ''"},
		{"bilibili_profile_url", "TEXT NOT NULL DEFAULT ''"},
		{"online_identity_status", "TEXT NOT NULL DEFAULT 'missing'"},
		{"avatar_url", "TEXT NOT NULL DEFAULT ''"},
		{"avatar_file", "TEXT NOT NULL DEFAULT ''"},
		{"bio", "TEXT NOT NULL DEFAULT ''"},
		{"profile_updated_at", "TEXT NOT NULL DEFAULT ''"},
		{"search_text", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing("creators", column.name, column.def); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		def  string
	}{
		{"search_text", "TEXT NOT NULL DEFAULT ''"},
		{"source_url", "TEXT NOT NULL DEFAULT ''"},
		{"cover_url", "TEXT NOT NULL DEFAULT ''"},
		{"cover_file", "TEXT NOT NULL DEFAULT ''"},
		{"work_type", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing("works", column.name, column.def); err != nil {
			return err
		}
	}
	if err := s.addColumnIfMissing("users", "role", "TEXT NOT NULL DEFAULT 'user'"); err != nil {
		return err
	}
	if err := s.addColumnIfMissing("favorite_folders", "user_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	for _, column := range []struct {
		name string
		def  string
	}{
		{"trigger_source", "TEXT NOT NULL DEFAULT 'manual'"},
		{"retry_of_job_id", "INTEGER NOT NULL DEFAULT 0"},
		{"attempt", "INTEGER NOT NULL DEFAULT 1"},
		{"total_works", "INTEGER NOT NULL DEFAULT 0"},
		{"processed_works", "INTEGER NOT NULL DEFAULT 0"},
		{"downloaded_media", "INTEGER NOT NULL DEFAULT 0"},
		{"skipped_works", "INTEGER NOT NULL DEFAULT 0"},
		{"download_speed_bps", "INTEGER NOT NULL DEFAULT 0"},
		{"progress_message", "TEXT NOT NULL DEFAULT ''"},
		{"download_directory", "TEXT NOT NULL DEFAULT ''"},
		{"download_notice", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := s.addColumnIfMissing("update_jobs", column.name, column.def); err != nil {
			return err
		}
	}
	for _, column := range []struct {
		name string
		def  string
	}{
		{"started_at", "TEXT NOT NULL DEFAULT ''"},
		{"finished_at", "TEXT NOT NULL DEFAULT ''"},
		{"trigger_source", "TEXT NOT NULL DEFAULT 'manual'"},
	} {
		if err := s.addColumnIfMissing("update_runs", column.name, column.def); err != nil {
			return err
		}
	}
	if _, err := s.db.Exec(`DROP INDEX IF EXISTS favorite_folders_one_default`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS favorite_folders_one_default ON favorite_folders(user_id, type) WHERE is_default=1`); err != nil {
		return err
	}
	if _, err := s.db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS favorite_folders_user_name_idx ON favorite_folders(user_id, type, name)`); err != nil {
		return err
	}
	if err := s.backfillSearchText(); err != nil {
		return err
	}
	if err := s.ensureBootstrapAdmin(); err != nil {
		return err
	}
	if err := s.migrateUserScopedFavorites(); err != nil {
		return err
	}
	return s.backfillUserRoles()
}

func (s *Store) addColumnIfMissing(table, column, definition string) error {
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return err
		}
		if name == column {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	_, err = s.db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + definition)
	return err
}

func (s *Store) backfillSearchText() error {
	rows, err := s.db.Query(`SELECT id, name, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, bio FROM creators WHERE search_text=''`)
	if err != nil {
		return err
	}
	type creatorIndex struct {
		id         int64
		searchText string
	}
	var creators []creatorIndex
	for rows.Next() {
		var id int64
		var name, uid, secUID, profileURL, bio string
		if err := rows.Scan(&id, &name, &uid, &secUID, &profileURL, &bio); err != nil {
			_ = rows.Close()
			return err
		}
		creators = append(creators, creatorIndex{id: id, searchText: buildSearchText(name, uid, secUID, profileURL, bio)})
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, creator := range creators {
		if _, err := s.db.Exec(`UPDATE creators SET search_text=? WHERE id=?`, creator.searchText, creator.id); err != nil {
			return err
		}
	}

	workRows, err := s.db.Query(`SELECT w.id, w.title, w.description, w.music_title, w.tags_json, c.name
FROM works w JOIN creators c ON c.id=w.creator_id WHERE w.search_text=''`)
	if err != nil {
		return err
	}
	type workIndex struct {
		id         int64
		searchText string
	}
	var works []workIndex
	for workRows.Next() {
		var id int64
		var title, description, musicTitle, tagsJSON, creatorName string
		if err := workRows.Scan(&id, &title, &description, &musicTitle, &tagsJSON, &creatorName); err != nil {
			_ = workRows.Close()
			return err
		}
		works = append(works, workIndex{id: id, searchText: buildSearchText(creatorName, title, description, musicTitle, tagsJSON)})
	}
	if err := workRows.Close(); err != nil {
		return err
	}
	for _, work := range works {
		if _, err := s.db.Exec(`UPDATE works SET search_text=? WHERE id=?`, work.searchText, work.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) backfillUserRoles() error {
	_, err := s.db.Exec(`UPDATE users SET role=? WHERE role='' AND is_admin=1`, userRoleSuperAdmin)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE users SET role=? WHERE role='' AND is_admin=0`, userRoleUser)
	return err
}

func (s *Store) migrateUserScopedFavorites() error {
	superID, err := s.superAdminUserID()
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`UPDATE favorite_folders SET user_id=? WHERE user_id=0`, superID); err != nil {
		return err
	}
	rows, err := s.db.Query(`SELECT id FROM users ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var userIDs []int64
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, userID := range userIDs {
		if err := s.ensureDefaultFavoriteFoldersForUser(userID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) superAdminUserID() (int64, error) {
	var id int64
	err := s.db.QueryRow(`SELECT id FROM users WHERE role=? ORDER BY id LIMIT 1`, userRoleSuperAdmin).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	err = s.db.QueryRow(`SELECT id FROM users WHERE is_admin=1 ORDER BY id LIMIT 1`).Scan(&id)
	return id, err
}

func (s *Store) ensureDefaultFavoriteFoldersForUser(userID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	for _, folder := range []struct {
		kind string
		name string
	}{
		{kind: "work", name: "默认作品收藏夹"},
		{kind: "creator", name: "默认作者收藏夹"},
	} {
		if _, err := s.db.Exec(`INSERT INTO favorite_folders(user_id, type, name, is_default, created_at, updated_at)
SELECT ?, ?, ?, 1, ?, ?
WHERE NOT EXISTS (SELECT 1 FROM favorite_folders WHERE user_id=? AND type=? AND is_default=1)`,
			userID, folder.kind, folder.name, now, now, userID, folder.kind); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) SaveDirectory(path string) (Directory, error) {
	clean := filepath.Clean(path)
	name := filepath.Base(clean)
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.Exec(`INSERT INTO directories(path, name, created_at) VALUES(?, ?, ?)
ON CONFLICT(path) DO UPDATE SET name=excluded.name`, clean, name, now)
	if err != nil {
		return Directory{}, err
	}
	return s.DirectoryByPath(clean)
}

func (s *Store) DirectoryByPath(path string) (Directory, error) {
	row := s.db.QueryRow(`SELECT id, path, name, status, last_scanned_at, created_at FROM directories WHERE path=?`, filepath.Clean(path))
	return scanDirectory(row)
}

func (s *Store) Directory(id int64) (Directory, error) {
	row := s.db.QueryRow(`SELECT id, path, name, status, last_scanned_at, created_at FROM directories WHERE id=?`, id)
	return scanDirectory(row)
}

func (s *Store) Directories() ([]Directory, error) {
	rows, err := s.db.Query(`SELECT id, path, name, status, last_scanned_at, created_at FROM directories ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	dirs := make([]Directory, 0)
	for rows.Next() {
		dir, err := scanDirectory(rows)
		if err != nil {
			return nil, err
		}
		dirs = append(dirs, dir)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range dirs {
		counts, err := s.DirectoryCounts(dirs[i].ID)
		if err != nil {
			return nil, err
		}
		dirs[i].CreatorCount = counts.creators
		dirs[i].WorkCount = counts.works
	}
	return dirs, nil
}

type dirCounts struct {
	creators int64
	works    int64
}

func (s *Store) DirectoryCounts(id int64) (dirCounts, error) {
	var counts dirCounts
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM creators WHERE directory_id=?`, id).Scan(&counts.creators); err != nil {
		return counts, err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM works WHERE directory_id=?`, id).Scan(&counts.works); err != nil {
		return counts, err
	}
	return counts, nil
}

func (s *Store) CreatorWorkCount(id int64) (int, error) {
	var count int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM works WHERE creator_id=?`, id).Scan(&count)
	return count, err
}

func (s *Store) DeleteDirectory(id int64) error {
	_, err := s.db.Exec(`DELETE FROM directories WHERE id=?`, id)
	return err
}

func (s *Store) DeleteCreator(id int64, deleteMedia bool) error {
	if id <= 0 {
		return sql.ErrNoRows
	}
	if deleteMedia {
		target, err := s.creatorDirectoryPath(id)
		if err != nil {
			return err
		}
		info, err := os.Stat(target)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				return err
			}
		} else {
			if !info.IsDir() {
				return errors.New("creator media path is not a directory")
			}
			if err := os.RemoveAll(target); err != nil {
				return err
			}
		}
	} else if _, err := s.Creator(id); err != nil {
		return err
	}
	if err := s.unlinkCreator(id); err != nil {
		return err
	}
	result, err := s.db.Exec(`DELETE FROM creators WHERE id=?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *Store) creatorDirectoryPath(id int64) (string, error) {
	var root, name string
	err := s.db.QueryRow(`SELECT d.path, c.name FROM creators c JOIN directories d ON d.id=c.directory_id WHERE c.id=?`, id).Scan(&root, &name)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(filepath.Clean(filepath.Join(rootAbs, name)))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("creator media directory is outside saved directory")
	}
	return targetAbs, nil
}

func (s *Store) MarkDirectoryStatus(id int64, status string) error {
	_, err := s.db.Exec(`UPDATE directories SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) MarkDirectoryScanned(id int64) error {
	_, err := s.db.Exec(`UPDATE directories SET status='idle', last_scanned_at=? WHERE id=?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

type WorkInput struct {
	AwemeID     string
	WorkType    string
	Title       string
	Description string
	MusicTitle  string
	SourceURL   string
	CoverURL    string
	CoverFile   string
	Tags        []string
	PublishedAt string
	Media       []MediaInput
}

type MediaInput struct {
	Type     string
	FilePath string
	FileName string
	Ordinal  int
}

type CreatorIdentity struct {
	Name       string
	UID        string
	SecUID     string
	ProfileURL string
	AvatarURL  string
	AvatarFile string
	Bio        string
}

func (s *Store) UpsertCreatorWithWorks(directoryID int64, name string, works []WorkInput) (Creator, int, error) {
	return s.UpsertCreatorWithWorksWithIdentity(directoryID, name, CreatorIdentity{}, works)
}

func (s *Store) UpsertCreatorWithWorksWithIdentity(directoryID int64, name string, identity CreatorIdentity, works []WorkInput) (Creator, int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Creator{}, 0, err
	}
	defer tx.Rollback()

	status := "missing"
	if strings.TrimSpace(identity.ProfileURL) != "" || strings.TrimSpace(identity.SecUID) != "" || strings.TrimSpace(identity.UID) != "" {
		status = "ready"
	}
	_, err = tx.Exec(`INSERT INTO creators(directory_id, name, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, online_identity_status, avatar_url, avatar_file, bio, profile_updated_at, search_text) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(directory_id, name) DO NOTHING`, directoryID, name, identity.UID, identity.SecUID, identity.ProfileURL, status, identity.AvatarURL, identity.AvatarFile, identity.Bio, time.Now().UTC().Format(time.RFC3339), buildSearchText(name, identity.UID, identity.SecUID, identity.ProfileURL, identity.Bio))
	if err != nil {
		return Creator{}, 0, err
	}
	if status == "ready" {
		if _, err := tx.Exec(`UPDATE creators SET
  bilibili_uid=CASE WHEN ?<>'' THEN ? ELSE bilibili_uid END,
  bilibili_sec_uid=CASE WHEN ?<>'' THEN ? ELSE bilibili_sec_uid END,
  bilibili_profile_url=CASE WHEN ?<>'' THEN ? ELSE bilibili_profile_url END,
  avatar_url=CASE WHEN ?<>'' THEN ? ELSE avatar_url END,
  avatar_file=CASE WHEN ?<>'' THEN ? ELSE avatar_file END,
  bio=CASE WHEN ?<>'' THEN ? ELSE bio END,
  profile_updated_at=?,
  online_identity_status='ready'
WHERE directory_id=? AND name=?`, identity.UID, identity.UID, identity.SecUID, identity.SecUID, identity.ProfileURL, identity.ProfileURL, identity.AvatarURL, identity.AvatarURL, identity.AvatarFile, identity.AvatarFile, identity.Bio, identity.Bio, time.Now().UTC().Format(time.RFC3339), directoryID, name); err != nil {
			return Creator{}, 0, err
		}
	}
	var creator Creator
	err = tx.QueryRow(`SELECT id, directory_id, name, work_count, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, online_identity_status, avatar_url, avatar_file, bio, profile_updated_at FROM creators WHERE directory_id=? AND name=?`, directoryID, name).
		Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus, &creator.AvatarURL, &creator.AvatarFile, &creator.Bio, &creator.ProfileUpdatedAt)
	if err != nil {
		return Creator{}, 0, err
	}
	if _, err := tx.Exec(`UPDATE creators SET search_text=? WHERE id=?`, buildSearchText(creator.Name, creator.BilibiliUID, creator.BilibiliSecUID, creator.BilibiliProfileURL, creator.Bio), creator.ID); err != nil {
		return Creator{}, 0, err
	}

	insertedWorks := 0
	for _, work := range works {
		tagsJSON, _ := json.Marshal(work.Tags)
		workType := normalizeWorkType(work.WorkType, work.Media)
		searchText := buildSearchText(creator.Name, work.Title, work.Description, work.MusicTitle, string(tagsJSON))
		_, err = tx.Exec(`INSERT INTO works(directory_id, creator_id, aweme_id, work_type, title, description, music_title, source_url, cover_url, cover_file, tags_json, published_at, search_text)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(directory_id, creator_id, aweme_id) DO UPDATE SET
  work_type=excluded.work_type,
  title=excluded.title,
  description=excluded.description,
  music_title=excluded.music_title,
  source_url=excluded.source_url,
  cover_url=excluded.cover_url,
  cover_file=excluded.cover_file,
  tags_json=excluded.tags_json,
  published_at=excluded.published_at,
  search_text=excluded.search_text`, directoryID, creator.ID, work.AwemeID, workType, work.Title, work.Description, work.MusicTitle, work.SourceURL, work.CoverURL, work.CoverFile, string(tagsJSON), work.PublishedAt, searchText)
		if err != nil {
			return Creator{}, 0, err
		}
		var workID int64
		err = tx.QueryRow(`SELECT id FROM works WHERE directory_id=? AND creator_id=? AND aweme_id=?`, directoryID, creator.ID, work.AwemeID).Scan(&workID)
		if err != nil {
			return Creator{}, 0, err
		}
		for _, media := range work.Media {
			_, err = tx.Exec(`INSERT INTO media(work_id, type, file_path, file_name, ordinal) VALUES(?, ?, ?, ?, ?)
ON CONFLICT(file_path) DO UPDATE SET work_id=excluded.work_id, type=excluded.type, file_name=excluded.file_name, ordinal=excluded.ordinal`,
				workID, media.Type, media.FilePath, media.FileName, media.Ordinal)
			if err != nil {
				return Creator{}, 0, err
			}
		}
		insertedWorks++
	}
	_, err = tx.Exec(`UPDATE creators SET work_count=(SELECT COUNT(*) FROM works WHERE creator_id=?) WHERE id=?`, creator.ID, creator.ID)
	if err != nil {
		return Creator{}, 0, err
	}
	if err := tx.Commit(); err != nil {
		return Creator{}, 0, err
	}
	creator.WorkCount = int64(len(works))
	return creator, insertedWorks, nil
}

func normalizeWorkType(value string, media []MediaInput) string {
	value = strings.TrimSpace(value)
	switch value {
	case "video_long", "video_short", "image_text":
		return value
	}
	for _, item := range media {
		if item.Type == "image" {
			return "image_text"
		}
	}
	return "video_long"
}

func (s *Store) CreatorByOnlineIdentity(directoryID int64, secUID string, profileURL string) (Creator, bool, error) {
	secUID = strings.TrimSpace(secUID)
	profileURL = strings.TrimSpace(profileURL)
	if secUID == "" && profileURL == "" {
		return Creator{}, false, nil
	}
	where := []string{}
	args := []any{}
	if secUID != "" {
		where = append(where, "bilibili_sec_uid=?")
		args = append(args, secUID)
	}
	if profileURL != "" {
		where = append(where, "bilibili_profile_url=?")
		args = append(args, profileURL)
	}
	query := `SELECT id, directory_id, name, work_count, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, online_identity_status FROM creators WHERE ` + strings.Join(where, " OR ") + ` ORDER BY CASE WHEN directory_id=? THEN 0 ELSE 1 END, id LIMIT 1`
	args = append(args, directoryID)
	row := s.db.QueryRow(query, args...)
	var creator Creator
	err := row.Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return Creator{}, false, nil
	}
	if err != nil {
		return Creator{}, false, err
	}
	return creator, true, nil
}

func (s *Store) CreatorByBulkImportIdentity(directoryID int64, name string, secUID string, profileURL string) (Creator, bool, error) {
	name = preferredCreatorName(name)
	secUID = strings.TrimSpace(secUID)
	profileURL = strings.TrimSpace(profileURL)
	if name == "" {
		return Creator{}, false, nil
	}

	where := "directory_id=? AND name=?"
	args := []any{directoryID, name}
	if secUID != "" || profileURL != "" {
		identityWhere := []string{}
		if secUID != "" {
			identityWhere = append(identityWhere, "bilibili_sec_uid=?")
			args = append(args, secUID)
		}
		if profileURL != "" {
			identityWhere = append(identityWhere, "bilibili_profile_url=?")
			args = append(args, profileURL)
		}
		where += " AND (" + strings.Join(identityWhere, " OR ") + ")"
	}

	row := s.db.QueryRow(`SELECT id FROM creators WHERE `+where+` ORDER BY id LIMIT 1`, args...)
	var id int64
	err := row.Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		if existing, ok, identityErr := s.CreatorByOnlineIdentity(directoryID, secUID, profileURL); identityErr != nil {
			return Creator{}, false, identityErr
		} else if ok && isLikelyPlaceholderCreator(existing) {
			return existing, true, nil
		}
		if existing, ok, similarErr := s.correctableCreatorByBulkImportName(directoryID, name); similarErr != nil {
			return Creator{}, false, similarErr
		} else if ok {
			return existing, true, nil
		}
		return Creator{}, false, nil
	}
	if err != nil {
		return Creator{}, false, err
	}
	creator, err := s.Creator(id)
	if err != nil {
		return Creator{}, false, err
	}
	return creator, true, nil
}

func (s *Store) correctableCreatorByBulkImportName(directoryID int64, name string) (Creator, bool, error) {
	name = preferredCreatorName(name)
	comparable := comparableCreatorName(name)
	if comparable == "" {
		return Creator{}, false, nil
	}
	rows, err := s.db.Query(`SELECT id, directory_id, name, work_count, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, online_identity_status, avatar_url, avatar_file, bio, profile_updated_at
FROM creators
WHERE directory_id=? AND work_count=0 AND name<>?
ORDER BY id`, directoryID, name)
	if err != nil {
		return Creator{}, false, err
	}
	defer rows.Close()
	for rows.Next() {
		var creator Creator
		if err := rows.Scan(
			&creator.ID,
			&creator.DirectoryID,
			&creator.Name,
			&creator.WorkCount,
			&creator.BilibiliUID,
			&creator.BilibiliSecUID,
			&creator.BilibiliProfileURL,
			&creator.OnlineIdentityStatus,
			&creator.AvatarURL,
			&creator.AvatarFile,
			&creator.Bio,
			&creator.ProfileUpdatedAt,
		); err != nil {
			return Creator{}, false, err
		}
		if comparableCreatorName(creator.Name) == comparable {
			return creator, true, nil
		}
	}
	return Creator{}, false, rows.Err()
}

func (s *Store) ImportCreatorPlaceholder(directoryID int64, profileURL string) (Creator, error) {
	normalized, secUID, err := normalizeBilibiliProfileURL(context.Background(), profileURL, nil)
	if err != nil {
		return Creator{}, err
	}
	if creator, ok, err := s.CreatorByOnlineIdentity(directoryID, secUID, normalized); err != nil || ok {
		return creator, err
	}
	if _, err := s.Directory(directoryID); err != nil {
		return Creator{}, err
	}
	name, err := s.nextPlaceholderCreatorName(directoryID, secUID)
	if err != nil {
		return Creator{}, err
	}
	creator, _, err := s.UpsertCreatorWithWorksWithIdentity(directoryID, name, CreatorIdentity{SecUID: secUID, ProfileURL: normalized}, nil)
	if err != nil {
		return Creator{}, err
	}
	return s.Creator(creator.ID)
}

func (s *Store) ImportCreatorPlaceholders(directoryID int64, profileURLs []string) ([]Creator, error) {
	seen := map[string]bool{}
	creators := []Creator{}
	for _, raw := range profileURLs {
		normalized, _, err := normalizeBilibiliProfileURL(context.Background(), raw, nil)
		if err != nil {
			return nil, err
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		creator, err := s.ImportCreatorPlaceholder(directoryID, normalized)
		if err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, nil
}

func (s *Store) ImportResolvedCreatorProfiles(directoryID int64, profiles []CreatorProfile) ([]Creator, error) {
	if _, err := s.Directory(directoryID); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	creators := []Creator{}
	for _, profile := range profiles {
		name := preferredCreatorName(profile.Name)
		if name == "" {
			continue
		}
		profileURL := strings.TrimSpace(profile.ProfileURL)
		secUID := strings.TrimSpace(profile.SecUID)
		if profileURL == "" && secUID != "" {
			profileURL = "https://www.bilibili.com/user/" + secUID
		}
		if secUID == "" {
			secUID = extractBilibiliSecUID(profileURL)
		}
		key := firstNonEmpty(profileURL, secUID, name)
		if seen[key] {
			continue
		}
		seen[key] = true
		identity := CreatorIdentity{
			UID:        strings.TrimSpace(profile.UID),
			SecUID:     secUID,
			ProfileURL: profileURL,
			AvatarURL:  strings.TrimSpace(profile.AvatarURL),
			Bio:        strings.TrimSpace(profile.Bio),
		}
		if existing, ok, err := s.CreatorByBulkImportIdentity(directoryID, name, secUID, profileURL); err != nil {
			return nil, err
		} else if ok {
			if err := s.ReplaceCreatorOnlineProfile(existing.ID, identity); err != nil {
				return nil, err
			}
			updated, err := s.Creator(existing.ID)
			if err != nil {
				return nil, err
			}
			if shouldPromotePlaceholderName(updated, name) || shouldCorrectNearMatchCreatorName(updated, name) {
				updated, err = s.RenameCreator(updated.ID, name)
				if err != nil {
					return nil, err
				}
			}
			creators = append(creators, updated)
			continue
		}
		createName, err := s.nextAvailableCreatorName(directoryID, 0, name)
		if err != nil {
			return nil, err
		}
		creator, _, err := s.UpsertCreatorWithWorksWithIdentity(directoryID, createName, identity, nil)
		if err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, nil
}

func (s *Store) PlaceholderCreators(limit int) ([]Creator, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id, directory_id, name, work_count, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, online_identity_status, avatar_url, avatar_file, bio, profile_updated_at
FROM creators
WHERE bilibili_sec_uid<>'' AND (work_count=0 OR avatar_url='' OR bio='' OR profile_updated_at='')
ORDER BY id DESC
LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	creators := []Creator{}
	for rows.Next() {
		var creator Creator
		if err := rows.Scan(
			&creator.ID,
			&creator.DirectoryID,
			&creator.Name,
			&creator.WorkCount,
			&creator.BilibiliUID,
			&creator.BilibiliSecUID,
			&creator.BilibiliProfileURL,
			&creator.OnlineIdentityStatus,
			&creator.AvatarURL,
			&creator.AvatarFile,
			&creator.Bio,
			&creator.ProfileUpdatedAt,
		); err != nil {
			return nil, err
		}
		if isLikelyPlaceholderCreator(creator) {
			creators = append(creators, creator)
		}
	}
	return creators, rows.Err()
}

func (s *Store) nextPlaceholderCreatorName(directoryID int64, secUID string) (string, error) {
	suffix := secUID
	if len([]rune(secUID)) > 8 {
		runes := []rune(secUID)
		suffix = string(runes[len(runes)-8:])
	}
	base := suffix
	name := base
	for i := 2; ; i++ {
		var existing int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM creators WHERE directory_id=? AND name=?`, directoryID, name).Scan(&existing)
		if err != nil {
			return "", err
		}
		if existing == 0 {
			return name, nil
		}
		name = base + "-" + strconv.Itoa(i)
	}
}

func (s *Store) nextAvailableCreatorName(directoryID, excludeID int64, base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", errors.New("creator name is required")
	}
	name := base
	for i := 2; ; i++ {
		var existing int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM creators WHERE directory_id=? AND name=? AND id<>?`, directoryID, name, excludeID).Scan(&existing)
		if err != nil {
			return "", err
		}
		if existing == 0 {
			return name, nil
		}
		name = base + "-" + strconv.Itoa(i)
	}
}

func (s *Store) RenameCreator(id int64, desiredName string) (Creator, error) {
	desiredName = strings.TrimSpace(desiredName)
	if desiredName == "" {
		return Creator{}, errors.New("creator name is required")
	}
	creator, err := s.Creator(id)
	if err != nil {
		return Creator{}, err
	}
	if creator.Name == desiredName {
		return creator, nil
	}
	name, err := s.nextAvailableCreatorName(creator.DirectoryID, creator.ID, desiredName)
	if err != nil {
		return Creator{}, err
	}
	if _, err := s.db.Exec(
		`UPDATE creators SET name=?, search_text=? WHERE id=?`,
		name,
		buildSearchText(name, creator.BilibiliUID, creator.BilibiliSecUID, creator.BilibiliProfileURL, creator.Bio),
		creator.ID,
	); err != nil {
		return Creator{}, err
	}
	return s.Creator(id)
}

func (s *Store) UpdateCreatorOnlineProfile(id int64, identity CreatorIdentity) error {
	return s.updateCreatorOnlineProfile(id, identity, false)
}

func (s *Store) ReplaceCreatorOnlineProfile(id int64, identity CreatorIdentity) error {
	return s.updateCreatorOnlineProfile(id, identity, true)
}

func (s *Store) updateCreatorOnlineProfile(id int64, identity CreatorIdentity, replaceIdentity bool) error {
	creator, err := s.Creator(id)
	if err != nil {
		return err
	}
	status := creator.OnlineIdentityStatus
	if strings.TrimSpace(identity.ProfileURL) != "" || strings.TrimSpace(identity.SecUID) != "" || strings.TrimSpace(identity.UID) != "" {
		status = "ready"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	nextUID := firstNonEmpty(identity.UID, creator.BilibiliUID)
	nextSecUID := firstNonEmpty(identity.SecUID, creator.BilibiliSecUID)
	nextProfileURL := firstNonEmpty(identity.ProfileURL, creator.BilibiliProfileURL)
	if replaceIdentity {
		nextUID = firstNonEmpty(identity.UID, nextUID)
		nextSecUID = firstNonEmpty(identity.SecUID, nextSecUID)
		nextProfileURL = firstNonEmpty(identity.ProfileURL, nextProfileURL)
	}
	nextAvatarURL := firstNonEmpty(identity.AvatarURL, creator.AvatarURL)
	nextAvatarFile := firstNonEmpty(identity.AvatarFile, creator.AvatarFile)
	nextBio := firstNonEmpty(identity.Bio, creator.Bio)
	_, err = s.db.Exec(`UPDATE creators SET
  bilibili_uid=?,
  bilibili_sec_uid=?,
  bilibili_profile_url=?,
  online_identity_status=?,
  avatar_url=?,
  avatar_file=?,
  bio=?,
  profile_updated_at=?,
  search_text=?
WHERE id=?`,
		nextUID,
		nextSecUID,
		nextProfileURL,
		status,
		nextAvatarURL,
		nextAvatarFile,
		nextBio,
		now,
		buildSearchText(creator.Name, nextUID, nextSecUID, nextProfileURL, nextBio),
		id,
	)
	return err
}

func shouldCorrectNearMatchCreatorName(creator Creator, desiredName string) bool {
	currentName := strings.TrimSpace(creator.Name)
	desiredName = preferredCreatorName(desiredName)
	if currentName == "" || desiredName == "" || currentName == desiredName || creator.WorkCount != 0 {
		return false
	}
	return comparableCreatorName(currentName) != "" && comparableCreatorName(currentName) == comparableCreatorName(desiredName)
}

var comparableCreatorNameSeparators = regexp.MustCompile(`[\s\p{P}\p{S}_]+`)

func comparableCreatorName(value string) string {
	value = strings.ToLower(preferredCreatorName(value))
	value = comparableCreatorNameSeparators.ReplaceAllString(value, "")
	value = strings.ReplaceAll(value, "的", "")
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

type CreatorListOptions struct {
	Search       string
	Sort         string
	Order        string
	MinWorkCount *int64
}

func (s *Store) Creators(search ...string) ([]Creator, error) {
	opts := CreatorListOptions{}
	if len(search) > 0 {
		opts.Search = search[0]
	}
	return s.CreatorsWithOptions(opts)
}

func (s *Store) CreatorsWithOptions(opts CreatorListOptions) ([]Creator, error) {
	query := `WITH work_stats AS (
	SELECT creator_id, MAX(published_at) AS last_work_created_at
	FROM works
	GROUP BY creator_id
), view_stats AS (
	SELECT creator_id, COUNT(*) AS view_count
	FROM work_view_history
	WHERE event_type='view'
	GROUP BY creator_id
), subtitle_stats AS (
	SELECT creator_id, COUNT(*) AS subtitle_count
	FROM subtitle_records
	WHERE status='ready' AND text_path<>''
	GROUP BY creator_id
)
SELECT c.id, c.directory_id, c.name, c.work_count, c.bilibili_uid, c.bilibili_sec_uid, c.bilibili_profile_url, c.online_identity_status, c.avatar_url, c.avatar_file, c.bio, c.profile_updated_at,
COALESCE(ws.last_work_created_at, '') AS last_work_created_at,
COALESCE(vs.view_count, 0) AS view_count,
COALESCE(ss.subtitle_count, 0) AS subtitle_count
FROM creators c
LEFT JOIN work_stats ws ON ws.creator_id=c.id
LEFT JOIN view_stats vs ON vs.creator_id=c.id
LEFT JOIN subtitle_stats ss ON ss.creator_id=c.id`
	var args []any
	var where []string
	if like := searchLikePattern(opts.Search); like != "" {
		where = append(where, `c.search_text LIKE ?`)
		args = append(args, like)
	}
	if opts.MinWorkCount != nil {
		where = append(where, `c.work_count >= ?`)
		args = append(args, *opts.MinWorkCount)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	order := "DESC"
	if strings.EqualFold(opts.Order, "asc") {
		order = "ASC"
	}
	switch strings.ToLower(strings.TrimSpace(opts.Sort)) {
	case "name":
		query += ` ORDER BY c.name COLLATE NOCASE ` + order + `, c.id DESC`
	case "views":
		query += ` ORDER BY view_count ` + order + `, c.name COLLATE NOCASE ASC`
	case "updated_at", "recent", "":
		query += ` ORDER BY last_work_created_at ` + order + `, c.profile_updated_at ` + order + `, c.id DESC`
	default:
		query += ` ORDER BY last_work_created_at DESC, c.profile_updated_at DESC, c.id DESC`
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	creators := make([]Creator, 0)
	for rows.Next() {
		var creator Creator
		if err := rows.Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus, &creator.AvatarURL, &creator.AvatarFile, &creator.Bio, &creator.ProfileUpdatedAt, &creator.LastWorkCreatedAt, &creator.ViewCount, &creator.SubtitleCount); err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return creators, s.enrichCreatorList(creators)
}

type FeedOptions struct {
	CreatorID        int64
	DirectoryID      int64
	FavoriteFolderID int64
	Search           string
	Order            string
	Limit            int
}

func (s *Store) Feed(creatorID int64, directoryID int64, search string, limit int) ([]Work, error) {
	return s.FeedWithOptions(FeedOptions{CreatorID: creatorID, DirectoryID: directoryID, Search: search, Limit: limit})
}

func (s *Store) FeedWithOptions(options FeedOptions) ([]Work, error) {
	limit := options.Limit
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	order := strings.ToLower(strings.TrimSpace(options.Order))
	switch order {
	case "asc", "desc", "random":
	default:
		order = "random"
	}
	query := `SELECT w.id, w.directory_id, w.creator_id, c.name, c.avatar_url, c.bio, w.aweme_id, w.work_type, w.title, w.description, w.music_title, w.source_url, w.cover_url, w.cover_file, w.tags_json, w.published_at, w.created_at
FROM works w JOIN creators c ON c.id=w.creator_id`
	var args []any
	var where []string
	if options.FavoriteFolderID > 0 {
		query += ` JOIN work_favorites wf ON wf.work_id=w.id`
		where = append(where, "wf.folder_id=?")
		args = append(args, options.FavoriteFolderID)
	}
	if options.CreatorID > 0 {
		creatorIDs, err := s.CreatorFeedIDs(options.CreatorID)
		if err != nil {
			return nil, err
		}
		if len(creatorIDs) == 0 {
			return []Work{}, nil
		}
		placeholders := make([]string, 0, len(creatorIDs))
		for _, id := range creatorIDs {
			placeholders = append(placeholders, "?")
			args = append(args, id)
		}
		where = append(where, "w.creator_id IN ("+strings.Join(placeholders, ",")+")")
	}
	if options.DirectoryID > 0 {
		where = append(where, "w.directory_id=?")
		args = append(args, options.DirectoryID)
	}
	if like := searchLikePattern(options.Search); like != "" {
		where = append(where, `w.search_text LIKE ?`)
		args = append(args, like)
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	switch order {
	case "asc":
		query += ` ORDER BY w.published_at ASC, w.id ASC LIMIT ?`
	case "desc":
		query += ` ORDER BY w.published_at DESC, w.id DESC LIMIT ?`
	default:
		query += ` ORDER BY RANDOM() LIMIT ?`
	}
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	works := make([]Work, 0)
	for rows.Next() {
		work, err := scanWork(rows)
		if err != nil {
			return nil, err
		}
		works = append(works, work)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range works {
		media, err := s.MediaForWork(works[i].ID)
		if err != nil {
			return nil, err
		}
		works[i].Media = media
		works[i].LinkedCreators, err = s.CreatorLinkedSummaries(works[i].CreatorID)
		if err != nil {
			return nil, err
		}
	}
	return works, nil
}

func (s *Store) FavoriteFolders(userID int64, kind string) ([]FavoriteFolder, error) {
	if kind != "work" && kind != "creator" {
		return nil, errors.New("invalid favorite folder type")
	}
	rows, err := s.db.Query(`SELECT id, user_id, type, name, is_default, created_at, updated_at
FROM favorite_folders WHERE user_id=? AND type=? ORDER BY is_default DESC, created_at, id`, userID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders := make([]FavoriteFolder, 0)
	for rows.Next() {
		folder, err := scanFavoriteFolder(rows)
		if err != nil {
			return nil, err
		}
		folders = append(folders, folder)
	}
	return folders, rows.Err()
}

func (s *Store) CreateFavoriteFolder(userID int64, kind, name string) (FavoriteFolder, error) {
	if kind != "work" && kind != "creator" {
		return FavoriteFolder{}, errors.New("invalid favorite folder type")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return FavoriteFolder{}, errors.New("name is required")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`INSERT INTO favorite_folders(user_id, type, name, is_default, created_at, updated_at) VALUES(?, ?, ?, 0, ?, ?)`, userID, kind, name, now, now)
	if err != nil {
		return FavoriteFolder{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return FavoriteFolder{}, err
	}
	return s.FavoriteFolder(userID, id)
}

func (s *Store) FavoriteFolder(userID, id int64) (FavoriteFolder, error) {
	row := s.db.QueryRow(`SELECT id, user_id, type, name, is_default, created_at, updated_at FROM favorite_folders WHERE user_id=? AND id=?`, userID, id)
	return scanFavoriteFolder(row)
}

func (s *Store) UpdateFavoriteFolder(userID, id int64, name string, isDefault *bool) (FavoriteFolder, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return FavoriteFolder{}, err
	}
	defer tx.Rollback()
	var kind string
	if err := tx.QueryRow(`SELECT type FROM favorite_folders WHERE user_id=? AND id=?`, userID, id).Scan(&kind); err != nil {
		return FavoriteFolder{}, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(name) != "" {
		if _, err := tx.Exec(`UPDATE favorite_folders SET name=?, updated_at=? WHERE id=?`, strings.TrimSpace(name), now, id); err != nil {
			return FavoriteFolder{}, err
		}
	}
	if isDefault != nil && *isDefault {
		if _, err := tx.Exec(`UPDATE favorite_folders SET is_default=0, updated_at=? WHERE user_id=? AND type=?`, now, userID, kind); err != nil {
			return FavoriteFolder{}, err
		}
		if _, err := tx.Exec(`UPDATE favorite_folders SET is_default=1, updated_at=? WHERE user_id=? AND id=?`, now, userID, id); err != nil {
			return FavoriteFolder{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return FavoriteFolder{}, err
	}
	return s.FavoriteFolder(userID, id)
}

func (s *Store) AddWorkFavorite(userID, workID, folderID int64) error {
	id, err := s.resolveFavoriteFolderID(userID, "work", folderID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO work_favorites(folder_id, work_id) VALUES(?, ?) ON CONFLICT(folder_id, work_id) DO NOTHING`, id, workID)
	return err
}

func (s *Store) RemoveWorkFavorite(userID, workID, folderID int64) error {
	id, err := s.resolveFavoriteFolderID(userID, "work", folderID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM work_favorites WHERE folder_id=? AND work_id=?`, id, workID)
	return err
}

func (s *Store) AddCreatorFavorite(userID, creatorID, folderID int64) error {
	id, err := s.resolveFavoriteFolderID(userID, "creator", folderID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO creator_favorites(folder_id, creator_id) VALUES(?, ?) ON CONFLICT(folder_id, creator_id) DO NOTHING`, id, creatorID)
	return err
}

func (s *Store) RemoveCreatorFavorite(userID, creatorID, folderID int64) error {
	id, err := s.resolveFavoriteFolderID(userID, "creator", folderID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`DELETE FROM creator_favorites WHERE folder_id=? AND creator_id=?`, id, creatorID)
	return err
}

func (s *Store) FavoriteCreators(userID, folderID int64) ([]Creator, error) {
	rows, err := s.db.Query(`SELECT c.id, c.directory_id, c.name, c.work_count, c.bilibili_uid, c.bilibili_sec_uid, c.bilibili_profile_url, c.online_identity_status
FROM creator_favorites cf
JOIN favorite_folders ff ON ff.id=cf.folder_id
JOIN creators c ON c.id=cf.creator_id
WHERE ff.user_id=? AND cf.folder_id=? ORDER BY cf.created_at DESC, c.name COLLATE NOCASE`, userID, folderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	creators := make([]Creator, 0)
	for rows.Next() {
		var creator Creator
		if err := rows.Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus); err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, rows.Err()
}

func (s *Store) FavoriteStatus(userID, workID, creatorID int64) (FavoriteStatus, error) {
	var status FavoriteStatus
	if workID > 0 {
		ids, err := s.favoriteFolderIDs(`SELECT wf.folder_id
FROM work_favorites wf JOIN favorite_folders ff ON ff.id=wf.folder_id
WHERE ff.user_id=? AND wf.work_id=? ORDER BY wf.folder_id`, userID, workID)
		if err != nil {
			return status, err
		}
		status.WorkFolderIDs = ids
		status.WorkFavorited = len(ids) > 0
	}
	if creatorID > 0 {
		ids, err := s.favoriteFolderIDs(`SELECT cf.folder_id
FROM creator_favorites cf JOIN favorite_folders ff ON ff.id=cf.folder_id
WHERE ff.user_id=? AND cf.creator_id=? ORDER BY cf.folder_id`, userID, creatorID)
		if err != nil {
			return status, err
		}
		status.CreatorFolderIDs = ids
		status.CreatorFavorited = len(ids) > 0
	}
	return status, nil
}

func (s *Store) favoriteFolderIDs(query string, args ...any) ([]int64, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0)
	for rows.Next() {
		var folderID int64
		if err := rows.Scan(&folderID); err != nil {
			return nil, err
		}
		ids = append(ids, folderID)
	}
	return ids, rows.Err()
}

func (s *Store) resolveFavoriteFolderID(userID int64, kind string, folderID int64) (int64, error) {
	if folderID > 0 {
		var actualKind string
		if err := s.db.QueryRow(`SELECT type FROM favorite_folders WHERE user_id=? AND id=?`, userID, folderID).Scan(&actualKind); err != nil {
			return 0, err
		}
		if actualKind != kind {
			return 0, errors.New("favorite folder type mismatch")
		}
		return folderID, nil
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM favorite_folders WHERE user_id=? AND type=? AND is_default=1`, userID, kind).Scan(&id)
	return id, err
}

func (s *Store) Work(id int64) (Work, error) {
	row := s.db.QueryRow(`SELECT w.id, w.directory_id, w.creator_id, c.name, c.avatar_url, c.bio, w.aweme_id, w.work_type, w.title, w.description, w.music_title, w.source_url, w.cover_url, w.cover_file, w.tags_json, w.published_at, w.created_at
FROM works w JOIN creators c ON c.id=w.creator_id WHERE w.id=?`, id)
	work, err := scanWork(row)
	if err != nil {
		return Work{}, err
	}
	work.Media, err = s.MediaForWork(work.ID)
	if err != nil {
		return Work{}, err
	}
	work.LinkedCreators, err = s.CreatorLinkedSummaries(work.CreatorID)
	return work, err
}

func (s *Store) MediaForWork(workID int64) ([]Media, error) {
	rows, err := s.db.Query(`SELECT id, work_id, type, file_name, ordinal FROM media WHERE work_id=? ORDER BY ordinal, id`, workID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	media := make([]Media, 0)
	for rows.Next() {
		var item Media
		if err := rows.Scan(&item.ID, &item.WorkID, &item.Type, &item.FileName, &item.Ordinal); err != nil {
			return nil, err
		}
		item.URL = "/api/media/" + strconv.FormatInt(item.ID, 10)
		media = append(media, item)
	}
	return media, rows.Err()
}

func (s *Store) MediaPath(id int64) (path string, name string, mediaType string, err error) {
	err = s.db.QueryRow(`SELECT file_path, file_name, type FROM media WHERE id=?`, id).Scan(&path, &name, &mediaType)
	return path, name, mediaType, err
}

func (s *Store) Creator(id int64) (Creator, error) {
	row := s.db.QueryRow(`SELECT id, directory_id, name, work_count, bilibili_uid, bilibili_sec_uid, bilibili_profile_url, online_identity_status, avatar_url, avatar_file, bio, profile_updated_at FROM creators WHERE id=?`, id)
	var creator Creator
	err := row.Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus, &creator.AvatarURL, &creator.AvatarFile, &creator.Bio, &creator.ProfileUpdatedAt)
	if err != nil {
		return creator, err
	}
	return creator, s.enrichCreator(&creator)
}

func (s *Store) enrichCreator(creator *Creator) error {
	if creator == nil || creator.ID <= 0 {
		return nil
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM work_view_history WHERE creator_id=? AND event_type='view'`, creator.ID).Scan(&creator.ViewCount); err != nil {
		return err
	}
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM subtitle_records WHERE creator_id=? AND status='ready' AND text_path<>''`, creator.ID).Scan(&creator.SubtitleCount); err != nil {
		return err
	}
	var lastPublished sql.NullString
	if err := s.db.QueryRow(`SELECT MAX(published_at) FROM works WHERE creator_id=?`, creator.ID).Scan(&lastPublished); err != nil {
		return err
	}
	if lastPublished.Valid {
		creator.LastWorkCreatedAt = lastPublished.String
	}
	linked, err := s.CreatorLinkedSummaries(creator.ID)
	if err != nil {
		return err
	}
	creator.LinkedCreators = linked
	if creator.AvatarURL == "" && creator.AvatarFile != "" {
		creator.AvatarURL = "/api/creators/" + strconv.FormatInt(creator.ID, 10) + "/avatar"
	}
	return nil
}

func (s *Store) enrichCreatorList(creators []Creator) error {
	if len(creators) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(creators))
	byID := make(map[int64]*Creator, len(creators))
	for i := range creators {
		if creators[i].AvatarURL == "" && creators[i].AvatarFile != "" {
			creators[i].AvatarURL = "/api/creators/" + strconv.FormatInt(creators[i].ID, 10) + "/avatar"
		}
		ids = append(ids, creators[i].ID)
		byID[creators[i].ID] = &creators[i]
	}
	linked, err := s.creatorLinkedSummariesFor(ids)
	if err != nil {
		return err
	}
	for id, summaries := range linked {
		if creator := byID[id]; creator != nil {
			creator.LinkedCreators = summaries
		}
	}
	return nil
}

func (s *Store) CreatorFeedIDs(id int64) ([]int64, error) {
	groupID, ok, err := s.creatorLinkGroupID(id)
	if err != nil {
		return nil, err
	}
	if !ok {
		if _, err := s.Creator(id); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return []int64{}, nil
			}
			return nil, err
		}
		return []int64{id}, nil
	}
	rows, err := s.db.Query(`SELECT creator_id FROM creator_link_members WHERE group_id=? ORDER BY creator_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var memberID int64
		if err := rows.Scan(&memberID); err != nil {
			return nil, err
		}
		ids = append(ids, memberID)
	}
	if len(ids) == 0 {
		return []int64{id}, nil
	}
	return ids, rows.Err()
}

func (s *Store) CreatorLinkedSummaries(id int64) ([]CreatorSummary, error) {
	groupID, ok, err := s.creatorLinkGroupID(id)
	if err != nil || !ok {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT c.id, c.name, c.avatar_url, c.avatar_file, c.bilibili_profile_url, c.bilibili_sec_uid
FROM creator_link_members clm JOIN creators c ON c.id=clm.creator_id
WHERE clm.group_id=? AND c.id<>?
ORDER BY c.name COLLATE NOCASE`, groupID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	linked := []CreatorSummary{}
	for rows.Next() {
		summary, err := scanCreatorSummary(rows)
		if err != nil {
			return nil, err
		}
		if summary.AvatarURL == "" && summary.AvatarFile != "" {
			summary.AvatarURL = "/api/creators/" + strconv.FormatInt(summary.ID, 10) + "/avatar"
		}
		linked = append(linked, summary)
	}
	return linked, rows.Err()
}

func (s *Store) creatorLinkedSummariesFor(ids []int64) (map[int64][]CreatorSummary, error) {
	result := make(map[int64][]CreatorSummary)
	if len(ids) == 0 {
		return result, nil
	}
	placeholders := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	rows, err := s.db.Query(`SELECT owner.creator_id, linked.id, linked.name, linked.avatar_url, linked.avatar_file, linked.bilibili_profile_url, linked.bilibili_sec_uid
FROM creator_link_members owner
JOIN creator_link_members member ON member.group_id=owner.group_id AND member.creator_id<>owner.creator_id
JOIN creators linked ON linked.id=member.creator_id
WHERE owner.creator_id IN (`+strings.Join(placeholders, ",")+`)
ORDER BY owner.creator_id, linked.name COLLATE NOCASE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var ownerID int64
		var summary CreatorSummary
		if err := rows.Scan(&ownerID, &summary.ID, &summary.Name, &summary.AvatarURL, &summary.AvatarFile, &summary.ProfileURL, &summary.BilibiliSecUID); err != nil {
			return nil, err
		}
		if summary.AvatarURL == "" && summary.AvatarFile != "" {
			summary.AvatarURL = "/api/creators/" + strconv.FormatInt(summary.ID, 10) + "/avatar"
		}
		result[ownerID] = append(result[ownerID], summary)
	}
	return result, rows.Err()
}

func (s *Store) CreatorLinks(id int64) (CreatorLinksResult, error) {
	if _, err := s.Creator(id); err != nil {
		return CreatorLinksResult{}, err
	}
	linkedIDs := map[int64]bool{}
	linkedSummaries, err := s.CreatorLinkedSummaries(id)
	if err != nil {
		return CreatorLinksResult{}, err
	}
	for _, item := range linkedSummaries {
		linkedIDs[item.ID] = true
	}
	all, err := s.Creators()
	if err != nil {
		return CreatorLinksResult{}, err
	}
	result := CreatorLinksResult{Linked: []Creator{}, Candidates: []Creator{}}
	for _, creator := range all {
		if creator.ID == id {
			continue
		}
		if linkedIDs[creator.ID] {
			result.Linked = append(result.Linked, creator)
		}
		result.Candidates = append(result.Candidates, creator)
	}
	return result, nil
}

func (s *Store) SetCreatorLinks(id int64, linkedIDs []int64) error {
	if _, err := s.Creator(id); err != nil {
		return err
	}
	unique := map[int64]bool{id: true}
	for _, linkedID := range linkedIDs {
		if linkedID > 0 && linkedID != id {
			unique[linkedID] = true
		}
	}
	if len(unique) == 1 {
		return s.unlinkCreator(id)
	}
	memberIDs := make([]int64, 0, len(unique))
	for memberID := range unique {
		memberIDs = append(memberIDs, memberID)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, memberID := range memberIDs {
		var exists int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM creators WHERE id=?`, memberID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return sql.ErrNoRows
		}
	}
	placeholders := make([]string, 0, len(memberIDs))
	args := make([]any, 0, len(memberIDs))
	for _, memberID := range memberIDs {
		placeholders = append(placeholders, "?")
		args = append(args, memberID)
	}
	rows, err := tx.Query(`SELECT DISTINCT group_id FROM creator_link_members WHERE creator_id IN (`+strings.Join(placeholders, ",")+`) ORDER BY group_id`, args...)
	if err != nil {
		return err
	}
	groupIDs := []int64{}
	for rows.Next() {
		var groupID int64
		if err := rows.Scan(&groupID); err != nil {
			rows.Close()
			return err
		}
		groupIDs = append(groupIDs, groupID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(groupIDs) > 0 {
		groupArgs := make([]any, 0, len(groupIDs))
		groupPlaceholders := make([]string, 0, len(groupIDs))
		for _, oldGroupID := range groupIDs {
			groupArgs = append(groupArgs, oldGroupID)
			groupPlaceholders = append(groupPlaceholders, "?")
		}
		memberRows, err := tx.Query(`SELECT creator_id FROM creator_link_members WHERE group_id IN (`+strings.Join(groupPlaceholders, ",")+`)`, groupArgs...)
		if err != nil {
			return err
		}
		for memberRows.Next() {
			var memberID int64
			if err := memberRows.Scan(&memberID); err != nil {
				memberRows.Close()
				return err
			}
			unique[memberID] = true
		}
		if err := memberRows.Close(); err != nil {
			return err
		}
		memberIDs = memberIDs[:0]
		for memberID := range unique {
			memberIDs = append(memberIDs, memberID)
		}
	}
	var groupID int64
	if len(groupIDs) > 0 {
		groupID = groupIDs[0]
	} else {
		result, err := tx.Exec(`INSERT INTO creator_link_groups(created_at) VALUES(?)`, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return err
		}
		groupID, err = result.LastInsertId()
		if err != nil {
			return err
		}
	}
	if len(groupIDs) > 0 {
		groupArgs := make([]any, 0, len(groupIDs))
		groupPlaceholders := make([]string, 0, len(groupIDs))
		for _, oldGroupID := range groupIDs {
			groupArgs = append(groupArgs, oldGroupID)
			groupPlaceholders = append(groupPlaceholders, "?")
		}
		if _, err := tx.Exec(`DELETE FROM creator_link_members WHERE group_id IN (`+strings.Join(groupPlaceholders, ",")+`)`, groupArgs...); err != nil {
			return err
		}
		for _, oldGroupID := range groupIDs[1:] {
			if _, err := tx.Exec(`DELETE FROM creator_link_groups WHERE id=?`, oldGroupID); err != nil {
				return err
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for _, memberID := range memberIDs {
		if _, err := tx.Exec(`INSERT INTO creator_link_members(group_id, creator_id, created_at) VALUES(?, ?, ?)
ON CONFLICT(creator_id) DO UPDATE SET group_id=excluded.group_id`, groupID, memberID, now); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) creatorLinkGroupID(id int64) (int64, bool, error) {
	var groupID int64
	err := s.db.QueryRow(`SELECT group_id FROM creator_link_members WHERE creator_id=?`, id).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return groupID, true, nil
}

func (s *Store) unlinkCreator(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var groupID int64
	err = tx.QueryRow(`SELECT group_id FROM creator_link_members WHERE creator_id=?`, id).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM creator_link_members WHERE creator_id=?`, id); err != nil {
		return err
	}
	var remaining int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM creator_link_members WHERE group_id=?`, groupID).Scan(&remaining); err != nil {
		return err
	}
	if remaining < 2 {
		if _, err := tx.Exec(`DELETE FROM creator_link_members WHERE group_id=?`, groupID); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM creator_link_groups WHERE id=?`, groupID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) CreatorAvatarFile(id int64) (string, error) {
	var path string
	if err := s.db.QueryRow(`SELECT avatar_file FROM creators WHERE id=?`, id).Scan(&path); err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", sql.ErrNoRows
	}
	return path, nil
}

func (s *Store) WorkCoverFile(id int64) (string, error) {
	var path string
	if err := s.db.QueryRow(`SELECT cover_file FROM works WHERE id=?`, id).Scan(&path); err != nil {
		return "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", sql.ErrNoRows
	}
	return path, nil
}

func (s *Store) RecordWorkView(userID, workID int64, eventType string, positionSec, durationSec float64, mediaType, detail string) error {
	if workID <= 0 {
		return errors.New("work_id is required")
	}
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		eventType = "view"
	}
	var creatorID int64
	if err := s.db.QueryRow(`SELECT creator_id FROM works WHERE id=?`, workID).Scan(&creatorID); err != nil {
		return err
	}
	_, err := s.db.Exec(`INSERT INTO work_view_history(user_id, work_id, creator_id, event_type, position_sec, duration_sec, media_type, detail_text, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID,
		workID,
		creatorID,
		eventType,
		positionSec,
		durationSec,
		strings.TrimSpace(mediaType),
		strings.TrimSpace(detail),
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) PlaybackDiagnostics(limit int) (PlaybackDiagnostics, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var result PlaybackDiagnostics
	var lastViewed sql.NullString
	var topViewed sql.NullString
	if err := s.db.QueryRow(`SELECT COUNT(*), 
COALESCE(SUM(CASE WHEN event_type='view' THEN 1 ELSE 0 END), 0),
COALESCE(SUM(CASE WHEN event_type LIKE '%error%' THEN 1 ELSE 0 END), 0),
MAX(created_at)
FROM work_view_history`).Scan(&result.TotalEvents, &result.TotalViews, &result.TotalErrors, &lastViewed); err != nil {
		return result, err
	}
	if lastViewed.Valid {
		result.LastViewedAt = lastViewed.String
	}
	_ = s.db.QueryRow(`SELECT c.name
FROM work_view_history wvh JOIN creators c ON c.id=wvh.creator_id
WHERE wvh.event_type='view'
GROUP BY c.id, c.name
ORDER BY COUNT(*) DESC, c.name COLLATE NOCASE
LIMIT 1`).Scan(&topViewed)
	if topViewed.Valid {
		result.TopViewedCreator = topViewed.String
	}
	creatorRows, err := s.db.Query(`SELECT c.id, c.name,
COALESCE(SUM(CASE WHEN wvh.event_type='view' THEN 1 ELSE 0 END), 0) AS views,
COALESCE(SUM(CASE WHEN wvh.event_type LIKE '%error%' THEN 1 ELSE 0 END), 0) AS errors,
MAX(wvh.created_at) AS last_viewed_at
FROM work_view_history wvh
JOIN creators c ON c.id=wvh.creator_id
GROUP BY c.id, c.name
ORDER BY views DESC, errors DESC, c.name COLLATE NOCASE
LIMIT 5`)
	if err != nil {
		return result, err
	}
	for creatorRows.Next() {
		var item PlaybackCreatorStat
		var lastViewed sql.NullString
		if err := creatorRows.Scan(&item.CreatorID, &item.CreatorName, &item.Views, &item.Errors, &lastViewed); err != nil {
			creatorRows.Close()
			return result, err
		}
		if lastViewed.Valid {
			item.LastViewedAt = lastViewed.String
		}
		result.TopCreators = append(result.TopCreators, item)
	}
	if err := creatorRows.Close(); err != nil {
		return result, err
	}
	workRows, err := s.db.Query(`SELECT w.id, c.id, c.name, w.title,
COALESCE(SUM(CASE WHEN wvh.event_type='view' THEN 1 ELSE 0 END), 0) AS views,
COALESCE(SUM(CASE WHEN wvh.event_type LIKE '%error%' THEN 1 ELSE 0 END), 0) AS errors,
MAX(wvh.created_at) AS last_event_at
FROM work_view_history wvh
JOIN works w ON w.id=wvh.work_id
JOIN creators c ON c.id=wvh.creator_id
GROUP BY w.id, c.id, c.name, w.title
HAVING errors > 0 OR views > 0
ORDER BY errors DESC, views DESC, last_event_at DESC
LIMIT 5`)
	if err != nil {
		return result, err
	}
	for workRows.Next() {
		var item PlaybackWorkStat
		var lastEvent sql.NullString
		if err := workRows.Scan(&item.WorkID, &item.CreatorID, &item.CreatorName, &item.WorkTitle, &item.Views, &item.Errors, &lastEvent); err != nil {
			workRows.Close()
			return result, err
		}
		if lastEvent.Valid {
			item.LastEventAt = lastEvent.String
		}
		result.TopProblemWorks = append(result.TopProblemWorks, item)
	}
	if err := workRows.Close(); err != nil {
		return result, err
	}
	rows, err := s.db.Query(`SELECT wvh.id, wvh.user_id, u.username, wvh.work_id, wvh.creator_id, c.name, w.title, wvh.event_type, wvh.position_sec, wvh.duration_sec, wvh.media_type, wvh.detail_text, wvh.created_at
FROM work_view_history wvh
LEFT JOIN users u ON u.id=wvh.user_id
JOIN creators c ON c.id=wvh.creator_id
JOIN works w ON w.id=wvh.work_id
ORDER BY wvh.id DESC LIMIT ?`, limit)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var event PlaybackEvent
		var created string
		if err := rows.Scan(&event.ID, &event.UserID, &event.Username, &event.WorkID, &event.CreatorID, &event.CreatorName, &event.WorkTitle, &event.EventType, &event.PositionSec, &event.DurationSec, &event.MediaType, &event.Detail, &created); err != nil {
			return result, err
		}
		event.CreatedAt = parseDBTime(created)
		result.RecentEvents = append(result.RecentEvents, event)
	}
	if err := rows.Err(); err != nil {
		return result, err
	}
	warnings, err := s.RecentDiagnosticWarnings("video_seekability_warning", 5)
	if err != nil {
		return result, err
	}
	result.RecentRepairWarnings = warnings
	return result, nil
}

func (s *Store) RecentDiagnosticWarnings(eventType string, limit int) ([]DiagnosticWarning, error) {
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	query := `SELECT id, event_type, severity, message, detail_json, created_at
FROM system_events`
	var args []any
	if strings.TrimSpace(eventType) != "" {
		query += ` WHERE event_type=?`
		args = append(args, strings.TrimSpace(eventType))
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]DiagnosticWarning, 0)
	for rows.Next() {
		var item DiagnosticWarning
		var created string
		if err := rows.Scan(&item.ID, &item.EventType, &item.Severity, &item.Message, &item.Detail, &created); err != nil {
			return nil, err
		}
		item.CreatedAt = parseDBTime(created)
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) CreateUpdateQueue(name, cron string) (UpdateQueue, error) {
	name = strings.TrimSpace(name)
	cron = strings.TrimSpace(cron)
	if name == "" {
		return UpdateQueue{}, errors.New("name is required")
	}
	if !ValidCron5(cron) {
		return UpdateQueue{}, errors.New("invalid cron")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`INSERT INTO update_queues(name, cron, is_default, enabled, next_run_at, created_at, updated_at) VALUES(?, ?, 0, 1, ?, ?, ?)`, name, cron, NextCronRun(cron, time.Now()).Format(time.RFC3339), now, now)
	if err != nil {
		return UpdateQueue{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UpdateQueue{}, err
	}
	return s.UpdateQueue(id)
}

func (s *Store) UpdateQueues() ([]UpdateQueue, error) {
	rows, err := s.db.Query(`SELECT q.id, q.name, q.cron, q.is_default, q.enabled, q.next_run_at, q.last_run_summary, q.created_at, q.updated_at, COUNT(uqc.creator_id)
FROM update_queues q LEFT JOIN update_queue_creators uqc ON uqc.queue_id=q.id
GROUP BY q.id ORDER BY q.is_default DESC, q.created_at, q.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	queues := make([]UpdateQueue, 0)
	for rows.Next() {
		queue, err := scanUpdateQueue(rows)
		if err != nil {
			return nil, err
		}
		queues = append(queues, queue)
	}
	return queues, rows.Err()
}

func (s *Store) UpdateQueue(id int64) (UpdateQueue, error) {
	row := s.db.QueryRow(`SELECT q.id, q.name, q.cron, q.is_default, q.enabled, q.next_run_at, q.last_run_summary, q.created_at, q.updated_at, COUNT(uqc.creator_id)
FROM update_queues q LEFT JOIN update_queue_creators uqc ON uqc.queue_id=q.id WHERE q.id=? GROUP BY q.id`, id)
	return scanUpdateQueue(row)
}

func (s *Store) UpdateUpdateQueue(id int64, name, cron string, isDefault, enabled *bool) (UpdateQueue, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return UpdateQueue{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(name) != "" {
		if _, err := tx.Exec(`UPDATE update_queues SET name=?, updated_at=? WHERE id=?`, strings.TrimSpace(name), now, id); err != nil {
			return UpdateQueue{}, err
		}
	}
	if strings.TrimSpace(cron) != "" {
		if !ValidCron5(cron) {
			return UpdateQueue{}, errors.New("invalid cron")
		}
		if _, err := tx.Exec(`UPDATE update_queues SET cron=?, next_run_at=?, updated_at=? WHERE id=?`, strings.TrimSpace(cron), NextCronRun(cron, time.Now()).Format(time.RFC3339), now, id); err != nil {
			return UpdateQueue{}, err
		}
	}
	if enabled != nil {
		value := 0
		if *enabled {
			value = 1
		}
		if _, err := tx.Exec(`UPDATE update_queues SET enabled=?, updated_at=? WHERE id=?`, value, now, id); err != nil {
			return UpdateQueue{}, err
		}
	}
	if isDefault != nil && *isDefault {
		if _, err := tx.Exec(`UPDATE update_queues SET is_default=0, updated_at=?`, now); err != nil {
			return UpdateQueue{}, err
		}
		if _, err := tx.Exec(`UPDATE update_queues SET is_default=1, updated_at=? WHERE id=?`, now, id); err != nil {
			return UpdateQueue{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return UpdateQueue{}, err
	}
	return s.UpdateQueue(id)
}

func (s *Store) DeleteUpdateQueue(id int64) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM update_queues`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("cannot delete the last update queue")
	}
	_, err := s.db.Exec(`DELETE FROM update_queues WHERE id=?`, id)
	return err
}

func (s *Store) AddCreatorsToUpdateQueue(queueID int64, creatorIDs []int64) error {
	for _, id := range creatorIDs {
		if id <= 0 {
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO update_queue_creators(queue_id, creator_id) VALUES(?, ?) ON CONFLICT(queue_id, creator_id) DO NOTHING`, queueID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveCreatorFromUpdateQueue(queueID, creatorID int64) error {
	_, err := s.db.Exec(`DELETE FROM update_queue_creators WHERE queue_id=? AND creator_id=?`, queueID, creatorID)
	return err
}

func (s *Store) CreatorsForUpdateQueue(queueID int64) ([]Creator, error) {
	rows, err := s.db.Query(`SELECT c.id, c.directory_id, c.name, c.work_count, c.bilibili_uid, c.bilibili_sec_uid, c.bilibili_profile_url, c.online_identity_status
FROM update_queue_creators uqc JOIN creators c ON c.id=uqc.creator_id WHERE uqc.queue_id=? ORDER BY c.name COLLATE NOCASE`, queueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	creators := make([]Creator, 0)
	for rows.Next() {
		var creator Creator
		if err := rows.Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus); err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, rows.Err()
}

func (s *Store) CreateUpdateRun(queue UpdateQueue, total int, triggerSource string) (UpdateRun, error) {
	source := normalizeUpdateTriggerSource(triggerSource)
	result, err := s.db.Exec(`INSERT INTO update_runs(queue_id, queue_name, total_creators, trigger_source, status, summary) VALUES(?, ?, ?, ?, 'queued', ?)`, queue.ID, queue.Name, total, source, "已加入立即执行队列")
	if err != nil {
		return UpdateRun{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UpdateRun{}, err
	}
	return s.UpdateRun(id)
}

func (s *Store) UpdateRun(id int64) (UpdateRun, error) {
	row := s.db.QueryRow(`SELECT id, queue_id, queue_name, enqueued_at, started_at, finished_at, trigger_source, total_creators, succeeded_creators, failed_creators, added_works, status, summary FROM update_runs WHERE id=?`, id)
	return scanUpdateRun(row)
}

func (s *Store) AddUpdateJobs(creatorIDs []int64, queueID, runID int64, triggerSource string) ([]UpdateJob, error) {
	jobs := make([]UpdateJob, 0, len(creatorIDs))
	source := normalizeUpdateTriggerSource(triggerSource)
	for _, creatorID := range creatorIDs {
		if creatorID <= 0 {
			continue
		}
		var existing int64
		err := s.db.QueryRow(`SELECT id FROM update_jobs WHERE creator_id=? AND status IN ('pending','running') LIMIT 1`, creatorID).Scan(&existing)
		if err == nil {
			job, err := s.UpdateJob(existing)
			if err != nil {
				return nil, err
			}
			jobs = append(jobs, job)
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		result, err := s.db.Exec(`INSERT INTO update_jobs(creator_id, queue_id, run_id, trigger_source, status) VALUES(?, ?, ?, ?, 'pending')`, creatorID, queueID, runID, source)
		if err != nil {
			return nil, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		job, err := s.UpdateJob(id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *Store) AddFailedUpdateJob(creatorID, queueID, runID, retryOfJobID int64, attempt int, message string, triggerSources ...string) (UpdateJob, error) {
	if creatorID <= 0 {
		return UpdateJob{}, errors.New("creator_id is required")
	}
	if attempt <= 0 {
		attempt = 1
	}
	source := normalizeUpdateTriggerSource("")
	if len(triggerSources) > 0 {
		source = normalizeUpdateTriggerSource(triggerSources[0])
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`INSERT INTO update_jobs(creator_id, queue_id, run_id, retry_of_job_id, attempt, trigger_source, status, error, started_at, finished_at) VALUES(?, ?, ?, ?, ?, ?, 'failed', ?, ?, ?)`,
		creatorID,
		queueID,
		runID,
		retryOfJobID,
		attempt,
		source,
		strings.TrimSpace(message),
		finishedAt,
		finishedAt,
	)
	if err != nil {
		return UpdateJob{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return UpdateJob{}, err
	}
	if runID > 0 {
		_, _ = s.db.Exec(`UPDATE update_runs SET failed_creators=failed_creators+1, status=CASE WHEN succeeded_creators + failed_creators + 1 >= total_creators THEN 'completed' ELSE status END, started_at=CASE WHEN started_at='' THEN ? ELSE started_at END, finished_at=CASE WHEN succeeded_creators + failed_creators + 1 >= total_creators THEN ? ELSE finished_at END, summary=? WHERE id=?`,
			finishedAt,
			finishedAt,
			"成功 0 个UP主，新增 0 个作品",
			runID,
		)
	}
	return s.UpdateJob(id)
}

func (s *Store) NextPendingUpdateJob() (UpdateJob, error) {
	row := s.db.QueryRow(updateJobSelectSQL + `
FROM update_jobs j JOIN creators c ON c.id=j.creator_id WHERE j.status='pending' ORDER BY j.id LIMIT 1`)
	return scanUpdateJob(row)
}

func (s *Store) UpdateJob(id int64) (UpdateJob, error) {
	row := s.db.QueryRow(updateJobSelectSQL+`
FROM update_jobs j JOIN creators c ON c.id=j.creator_id WHERE j.id=?`, id)
	return scanUpdateJob(row)
}

func (s *Store) MarkUpdateJobRunning(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE update_jobs SET status='running', started_at=?, finished_at='', download_speed_bps=0, progress_message='' WHERE id=?`, now, id); err != nil {
		return err
	}
	var runID int64
	if err := tx.QueryRow(`SELECT run_id FROM update_jobs WHERE id=?`, id).Scan(&runID); err != nil {
		return err
	}
	if runID > 0 {
		if _, err := tx.Exec(`UPDATE update_runs SET status='running', started_at=CASE WHEN started_at='' THEN ? ELSE started_at END WHERE id=?`, now, runID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) UpdateUpdateJobProgress(id int64, totalWorks, processedWorks, downloadedMedia, skippedWorks int, speedBps int64, message, directory, notice string) error {
	_, err := s.db.Exec(`UPDATE update_jobs SET total_works=?, processed_works=?, downloaded_media=?, skipped_works=?, download_speed_bps=?, progress_message=?, download_directory=?, download_notice=? WHERE id=?`,
		totalWorks,
		processedWorks,
		downloadedMedia,
		skippedWorks,
		speedBps,
		strings.TrimSpace(message),
		strings.TrimSpace(directory),
		strings.TrimSpace(notice),
		id,
	)
	return err
}

func (s *Store) FinishUpdateJob(id int64, status string, addedWorks int, message string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var runID, queueID int64
	if err := tx.QueryRow(`SELECT run_id, queue_id FROM update_jobs WHERE id=?`, id).Scan(&runID, &queueID); err != nil {
		return err
	}
	finishedAt := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE update_jobs SET status=?, added_works=?, error=?, finished_at=?, download_speed_bps=0 WHERE id=?`, status, addedWorks, message, finishedAt, id); err != nil {
		return err
	}
	if runID > 0 {
		successDelta := 0
		failedDelta := 0
		if status == "succeeded" {
			successDelta = 1
		}
		if status == "failed" {
			failedDelta = 1
		}
		summary := "成功 " + strconv.Itoa(successDelta) + " 个UP主，新增 " + strconv.Itoa(addedWorks) + " 个作品"
		if _, err := tx.Exec(`UPDATE update_runs SET
  succeeded_creators=succeeded_creators+?,
  failed_creators=failed_creators+?,
  added_works=added_works+?,
  status=CASE WHEN succeeded_creators + failed_creators + ? + ? >= total_creators THEN 'completed' ELSE status END,
  finished_at=CASE WHEN succeeded_creators + failed_creators + ? + ? >= total_creators THEN ? ELSE finished_at END,
  summary=?
WHERE id=?`, successDelta, failedDelta, addedWorks, successDelta, failedDelta, successDelta, failedDelta, finishedAt, summary, runID); err != nil {
			return err
		}
		if queueID > 0 {
			_, _ = tx.Exec(`UPDATE update_queues SET last_run_summary=(SELECT summary FROM update_runs WHERE id=?), updated_at=? WHERE id=?`, runID, time.Now().UTC().Format(time.RFC3339), queueID)
		}
	}
	return tx.Commit()
}

func (s *Store) FailInterruptedUpdateJobs(message string) (int64, error) {
	message = strings.TrimSpace(message)
	if message == "" {
		message = "服务重启后更新任务已中断，请重新触发更新"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE update_jobs SET status='failed', error=?, finished_at=?, download_speed_bps=0 WHERE status IN ('pending','running')`, message, now)
	if err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`UPDATE update_runs SET status='failed', finished_at=CASE WHEN finished_at='' THEN ? ELSE finished_at END, summary=? WHERE status IN ('queued','running')`, now, message); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return affected, nil
}

func (s *Store) UpdateJobs(limit int) ([]UpdateJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(updateJobSelectSQL+`
FROM update_jobs j JOIN creators c ON c.id=j.creator_id ORDER BY j.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]UpdateJob, 0)
	for rows.Next() {
		job, err := scanUpdateJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) UpdateJobsPage(page, pageSize int, statusFilter ...string) (UpdateJobsPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	filter := strings.TrimSpace(firstNonEmpty(statusFilter...))
	where := ""
	args := []any{}
	if filter != "" {
		where = " WHERE j.status=?"
		args = append(args, filter)
	}
	summary, err := s.UpdateJobsSummary()
	if err != nil {
		return UpdateJobsPage{}, err
	}
	var total int
	countArgs := append([]any{}, args...)
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM update_jobs j`+where, countArgs...).Scan(&total); err != nil {
		return UpdateJobsPage{}, err
	}
	offset := (page - 1) * pageSize
	queryArgs := append([]any{}, args...)
	queryArgs = append(queryArgs, pageSize, offset)
	rows, err := s.db.Query(updateJobSelectSQL+`
FROM update_jobs j JOIN creators c ON c.id=j.creator_id`+where+` ORDER BY j.id DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return UpdateJobsPage{}, err
	}
	defer rows.Close()
	jobs := make([]UpdateJob, 0, pageSize)
	for rows.Next() {
		job, err := scanUpdateJob(rows)
		if err != nil {
			return UpdateJobsPage{}, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return UpdateJobsPage{}, err
	}
	totalPages := total / pageSize
	if total%pageSize != 0 {
		totalPages++
	}
	return UpdateJobsPage{Jobs: jobs, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages, Summary: summary}, nil
}

func (s *Store) UpdateJobsSummary() (UpdateJobsSummary, error) {
	var summary UpdateJobsSummary
	rows, err := s.db.Query(`SELECT status, COUNT(*), COALESCE(SUM(added_works), 0) FROM update_jobs GROUP BY status`)
	if err != nil {
		return summary, err
	}
	defer rows.Close()
	for rows.Next() {
		var status string
		var count int
		var added int
		if err := rows.Scan(&status, &count, &added); err != nil {
			return summary, err
		}
		switch status {
		case "pending":
			summary.Pending = count
		case "running":
			summary.Running = count
		case "succeeded":
			summary.Succeeded = count
		case "failed":
			summary.Failed = count
		}
		summary.AddedWorks += added
	}
	return summary, rows.Err()
}

func (s *Store) RunningUpdateJobs() ([]UpdateJob, error) {
	rows, err := s.db.Query(updateJobSelectSQL + `
FROM update_jobs j JOIN creators c ON c.id=j.creator_id WHERE j.status='running' ORDER BY j.started_at DESC, j.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	jobs := make([]UpdateJob, 0)
	for rows.Next() {
		job, err := scanUpdateJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) DeleteUpdateJob(id int64) error {
	if id <= 0 {
		return errors.New("job id is required")
	}
	var status string
	if err := s.db.QueryRow(`SELECT status FROM update_jobs WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "running" {
		return errors.New("running update job cannot be deleted")
	}
	_, err := s.db.Exec(`DELETE FROM update_jobs WHERE id=?`, id)
	return err
}

func (s *Store) ClearUpdateJobs() error {
	_, err := s.db.Exec(`DELETE FROM update_jobs`)
	return err
}

func (s *Store) RetryUpdateJob(id int64) (UpdateJob, error) {
	original, err := s.UpdateJob(id)
	if err != nil {
		return UpdateJob{}, err
	}
	if original.Status != "failed" {
		return UpdateJob{}, errors.New("only failed update jobs can be retried")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return UpdateJob{}, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE update_jobs SET
  status='pending',
  attempt=attempt+1,
  added_works=0,
  error='',
  finished_at='',
  started_at='',
  download_speed_bps=0,
  progress_message='等待重试',
  download_notice=''
WHERE id=?`, id); err != nil {
		return UpdateJob{}, err
	}
	if original.RunID > 0 {
		if _, err := tx.Exec(`UPDATE update_runs SET
  failed_creators=CASE WHEN failed_creators > 0 THEN failed_creators - 1 ELSE 0 END,
  status='running',
  summary=''
WHERE id=?`, original.RunID); err != nil {
			return UpdateJob{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return UpdateJob{}, err
	}
	return s.UpdateJob(id)
}

type SubtitleVideo struct {
	WorkID      int64
	MediaID     int64
	CreatorID   int64
	CreatorName string
	Title       string
	FilePath    string
	FileName    string
}

func (s *Store) CreatorIDsWithVideoWorks() ([]int64, error) {
	rows, err := s.db.Query(`SELECT DISTINCT w.creator_id
FROM works w
JOIN media m ON m.work_id=w.id
WHERE m.type='video'
ORDER BY w.creator_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) VideoWorksForCreator(creatorID int64, workID int64) ([]SubtitleVideo, error) {
	query := `SELECT w.id, m.id, w.creator_id, c.name, w.title, m.file_path, m.file_name
FROM works w
JOIN creators c ON c.id=w.creator_id
JOIN media m ON m.work_id=w.id
WHERE w.creator_id=? AND m.type='video'`
	args := []any{creatorID}
	if workID > 0 {
		query += ` AND w.id=?`
		args = append(args, workID)
	}
	query += ` ORDER BY w.published_at DESC, w.id DESC, m.ordinal, m.id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]SubtitleVideo, 0)
	for rows.Next() {
		var item SubtitleVideo
		if err := rows.Scan(&item.WorkID, &item.MediaID, &item.CreatorID, &item.CreatorName, &item.Title, &item.FilePath, &item.FileName); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Store) UpsertSubtitleRecord(workID, mediaID, creatorID int64, textPath, srtPath, jsonPath, source, status, message string) error {
	if workID <= 0 || creatorID <= 0 {
		return errors.New("work_id and creator_id are required")
	}
	token, err := s.subtitleTokenForWork(workID)
	if err != nil {
		return err
	}
	if token == "" {
		token, err = randomToken()
		if err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`INSERT INTO subtitle_records(work_id, media_id, creator_id, text_path, srt_path, json_path, token, status, source, error, created_at, updated_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(work_id) DO UPDATE SET
  media_id=excluded.media_id,
  creator_id=excluded.creator_id,
  text_path=excluded.text_path,
  srt_path=excluded.srt_path,
  json_path=excluded.json_path,
  status=excluded.status,
  source=excluded.source,
  error=excluded.error,
  updated_at=excluded.updated_at`,
		workID, mediaID, creatorID, strings.TrimSpace(textPath), strings.TrimSpace(srtPath), strings.TrimSpace(jsonPath), token, strings.TrimSpace(status), strings.TrimSpace(source), strings.TrimSpace(message), now, now)
	return err
}

func (s *Store) subtitleTokenForWork(workID int64) (string, error) {
	var token string
	err := s.db.QueryRow(`SELECT token FROM subtitle_records WHERE work_id=?`, workID).Scan(&token)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return token, err
}

func (s *Store) SubtitleTokenForWork(workID int64) (string, error) {
	token, err := s.subtitleTokenForWork(workID)
	if err != nil || token != "" {
		return token, err
	}
	work, err := s.Work(workID)
	if err != nil {
		return "", err
	}
	token, err = randomToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = s.db.Exec(`INSERT INTO subtitle_records(work_id, creator_id, token, status, source, created_at, updated_at) VALUES(?, ?, ?, 'pending', 'link', ?, ?) ON CONFLICT(work_id) DO NOTHING`, workID, work.CreatorID, token, now, now)
	if err != nil {
		return "", err
	}
	return s.subtitleTokenForWork(workID)
}

func (s *Store) SubtitleTokenForCreator(creatorID int64) (string, error) {
	var token string
	err := s.db.QueryRow(`SELECT token FROM subtitle_records WHERE creator_id=? ORDER BY id LIMIT 1`, creatorID).Scan(&token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	videos, err := s.VideoWorksForCreator(creatorID, 0)
	if err != nil {
		return "", err
	}
	if len(videos) == 0 {
		return "", ErrNotFound
	}
	return s.SubtitleTokenForWork(videos[0].WorkID)
}

func (s *Store) SubtitleTextForWork(workID int64, token string) (string, string, bool, error) {
	var path, title, actualToken, status string
	err := s.db.QueryRow(`SELECT sr.text_path, w.title, sr.token, sr.status
FROM subtitle_records sr JOIN works w ON w.id=sr.work_id WHERE sr.work_id=?`, workID).Scan(&path, &title, &actualToken, &status)
	if err != nil {
		return "", "", false, err
	}
	if token == "" || token != actualToken {
		return "", "", false, errors.New("invalid subtitle token")
	}
	if status != "ready" || strings.TrimSpace(path) == "" {
		return title, "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return title, "", false, err
	}
	return title, string(data), true, nil
}

func (s *Store) SubtitleTextForCreator(creatorID int64, token string) (string, bool, error) {
	rows, err := s.db.Query(`SELECT w.title, w.published_at, sr.text_path, sr.token, sr.status
FROM works w
JOIN subtitle_records sr ON sr.work_id=w.id
WHERE w.creator_id=?
ORDER BY w.published_at DESC, w.id DESC`, creatorID)
	if err != nil {
		return "", false, err
	}
	defer rows.Close()
	var builder strings.Builder
	ready := false
	authorized := false
	for rows.Next() {
		var title, publishedAt, path, actualToken, status string
		if err := rows.Scan(&title, &publishedAt, &path, &actualToken, &status); err != nil {
			return "", false, err
		}
		if token != "" && token == actualToken {
			authorized = true
		}
		if status != "ready" || strings.TrimSpace(path) == "" {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", false, err
		}
		if builder.Len() > 0 {
			builder.WriteString("\n\n")
		}
		builder.WriteString("## ")
		if date := subtitleChapterDate(publishedAt); date != "" {
			builder.WriteString(date)
			builder.WriteString(" ")
		}
		builder.WriteString(strings.TrimSpace(title))
		builder.WriteString("\n\n")
		builder.Write(data)
		ready = true
	}
	if err := rows.Err(); err != nil {
		return "", false, err
	}
	if !authorized {
		firstToken, err := s.SubtitleTokenForCreator(creatorID)
		if err != nil {
			return "", false, err
		}
		if token != firstToken {
			return "", false, errors.New("invalid subtitle token")
		}
	}
	return builder.String(), ready, nil
}

func subtitleChapterDate(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.UTC().Format("2006-01-02")
	}
	if parsed, err := time.Parse("2006-01-02", value); err == nil {
		return parsed.Format("2006-01-02")
	}
	return ""
}

func (s *Store) CreateSubtitleQueue(name, cron string) (SubtitleQueue, error) {
	name = strings.TrimSpace(name)
	cron = strings.TrimSpace(cron)
	if name == "" {
		return SubtitleQueue{}, errors.New("name is required")
	}
	if !ValidCron5(cron) {
		return SubtitleQueue{}, errors.New("invalid cron")
	}
	now := time.Now().UTC().Format(time.RFC3339)
	result, err := s.db.Exec(`INSERT INTO subtitle_queues(name, cron, is_default, enabled, next_run_at, created_at, updated_at) VALUES(?, ?, 0, 1, ?, ?, ?)`, name, cron, NextCronRun(cron, time.Now()).Format(time.RFC3339), now, now)
	if err != nil {
		return SubtitleQueue{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SubtitleQueue{}, err
	}
	return s.SubtitleQueue(id)
}

func (s *Store) SubtitleQueues() ([]SubtitleQueue, error) {
	rows, err := s.db.Query(`SELECT q.id, q.name, q.cron, q.is_default, q.enabled, q.next_run_at, q.last_run_summary, q.created_at, q.updated_at, COUNT(sqc.creator_id)
FROM subtitle_queues q LEFT JOIN subtitle_queue_creators sqc ON sqc.queue_id=q.id
GROUP BY q.id ORDER BY q.is_default DESC, q.created_at, q.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var queues []SubtitleQueue
	for rows.Next() {
		queue, err := scanSubtitleQueue(rows)
		if err != nil {
			return nil, err
		}
		queues = append(queues, queue)
	}
	return queues, rows.Err()
}

func (s *Store) SubtitleQueue(id int64) (SubtitleQueue, error) {
	row := s.db.QueryRow(`SELECT q.id, q.name, q.cron, q.is_default, q.enabled, q.next_run_at, q.last_run_summary, q.created_at, q.updated_at, COUNT(sqc.creator_id)
FROM subtitle_queues q LEFT JOIN subtitle_queue_creators sqc ON sqc.queue_id=q.id WHERE q.id=? GROUP BY q.id`, id)
	return scanSubtitleQueue(row)
}

func (s *Store) UpdateSubtitleQueue(id int64, name, cron string, isDefault, enabled *bool) (SubtitleQueue, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return SubtitleQueue{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339)
	if strings.TrimSpace(name) != "" {
		if _, err := tx.Exec(`UPDATE subtitle_queues SET name=?, updated_at=? WHERE id=?`, strings.TrimSpace(name), now, id); err != nil {
			return SubtitleQueue{}, err
		}
	}
	if strings.TrimSpace(cron) != "" {
		if !ValidCron5(cron) {
			return SubtitleQueue{}, errors.New("invalid cron")
		}
		if _, err := tx.Exec(`UPDATE subtitle_queues SET cron=?, next_run_at=?, updated_at=? WHERE id=?`, strings.TrimSpace(cron), NextCronRun(cron, time.Now()).Format(time.RFC3339), now, id); err != nil {
			return SubtitleQueue{}, err
		}
	}
	if enabled != nil {
		value := 0
		if *enabled {
			value = 1
		}
		if _, err := tx.Exec(`UPDATE subtitle_queues SET enabled=?, updated_at=? WHERE id=?`, value, now, id); err != nil {
			return SubtitleQueue{}, err
		}
	}
	if isDefault != nil && *isDefault {
		if _, err := tx.Exec(`UPDATE subtitle_queues SET is_default=0, updated_at=?`, now); err != nil {
			return SubtitleQueue{}, err
		}
		if _, err := tx.Exec(`UPDATE subtitle_queues SET is_default=1, updated_at=? WHERE id=?`, now, id); err != nil {
			return SubtitleQueue{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return SubtitleQueue{}, err
	}
	return s.SubtitleQueue(id)
}

func (s *Store) DeleteSubtitleQueue(id int64) error {
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM subtitle_queues`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return errors.New("cannot delete the last subtitle queue")
	}
	_, err := s.db.Exec(`DELETE FROM subtitle_queues WHERE id=?`, id)
	return err
}

func (s *Store) AddCreatorsToSubtitleQueue(queueID int64, creatorIDs []int64) error {
	for _, id := range creatorIDs {
		if id <= 0 {
			continue
		}
		if _, err := s.db.Exec(`INSERT INTO subtitle_queue_creators(queue_id, creator_id) VALUES(?, ?) ON CONFLICT(queue_id, creator_id) DO NOTHING`, queueID, id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RemoveCreatorFromSubtitleQueue(queueID, creatorID int64) error {
	_, err := s.db.Exec(`DELETE FROM subtitle_queue_creators WHERE queue_id=? AND creator_id=?`, queueID, creatorID)
	return err
}

func (s *Store) CreatorsForSubtitleQueue(queueID int64) ([]Creator, error) {
	rows, err := s.db.Query(`SELECT c.id, c.directory_id, c.name, c.work_count, c.bilibili_uid, c.bilibili_sec_uid, c.bilibili_profile_url, c.online_identity_status, c.avatar_url, c.avatar_file, c.bio, c.profile_updated_at
FROM subtitle_queue_creators sqc JOIN creators c ON c.id=sqc.creator_id WHERE sqc.queue_id=? ORDER BY c.name COLLATE NOCASE`, queueID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var creators []Creator
	for rows.Next() {
		var creator Creator
		if err := rows.Scan(&creator.ID, &creator.DirectoryID, &creator.Name, &creator.WorkCount, &creator.BilibiliUID, &creator.BilibiliSecUID, &creator.BilibiliProfileURL, &creator.OnlineIdentityStatus, &creator.AvatarURL, &creator.AvatarFile, &creator.Bio, &creator.ProfileUpdatedAt); err != nil {
			return nil, err
		}
		creators = append(creators, creator)
	}
	return creators, s.enrichCreatorList(creators)
}

func (s *Store) AddSubtitleJobs(creatorIDs []int64, queueID, runID int64, source string) ([]SubtitleJob, error) {
	jobs := make([]SubtitleJob, 0, len(creatorIDs))
	for _, creatorID := range creatorIDs {
		if creatorID <= 0 {
			continue
		}
		var existing int64
		err := s.db.QueryRow(`SELECT id FROM subtitle_jobs WHERE creator_id=? AND work_id=0 AND status IN ('pending','running') LIMIT 1`, creatorID).Scan(&existing)
		if err == nil {
			job, err := s.SubtitleJob(existing)
			if err != nil {
				return nil, err
			}
			jobs = append(jobs, job)
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		result, err := s.db.Exec(`INSERT INTO subtitle_jobs(creator_id, queue_id, run_id, source, status) VALUES(?, ?, ?, ?, 'pending')`, creatorID, queueID, runID, strings.TrimSpace(source))
		if err != nil {
			return nil, err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return nil, err
		}
		job, err := s.SubtitleJob(id)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func (s *Store) AddSubtitleWorkJob(creatorID, workID int64, source string) (SubtitleJob, error) {
	var existing int64
	err := s.db.QueryRow(`SELECT id FROM subtitle_jobs WHERE work_id=? AND status IN ('pending','running') LIMIT 1`, workID).Scan(&existing)
	if err == nil {
		return s.SubtitleJob(existing)
	}
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return SubtitleJob{}, err
	}
	result, err := s.db.Exec(`INSERT INTO subtitle_jobs(creator_id, work_id, source, status) VALUES(?, ?, ?, 'pending')`, creatorID, workID, strings.TrimSpace(source))
	if err != nil {
		return SubtitleJob{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return SubtitleJob{}, err
	}
	return s.SubtitleJob(id)
}

func (s *Store) NextPendingSubtitleJob() (SubtitleJob, error) {
	row := s.db.QueryRow(subtitleJobSelectSQL + `
FROM subtitle_jobs j JOIN creators c ON c.id=j.creator_id LEFT JOIN works w ON w.id=j.work_id WHERE j.status='pending' ORDER BY j.id LIMIT 1`)
	return scanSubtitleJob(row)
}

func (s *Store) SubtitleJob(id int64) (SubtitleJob, error) {
	row := s.db.QueryRow(subtitleJobSelectSQL+`
FROM subtitle_jobs j JOIN creators c ON c.id=j.creator_id LEFT JOIN works w ON w.id=j.work_id WHERE j.id=?`, id)
	return scanSubtitleJob(row)
}

func (s *Store) MarkSubtitleJobRunning(id int64) error {
	_, err := s.db.Exec(`UPDATE subtitle_jobs SET status='running', started_at=?, finished_at='', progress_message='' WHERE id=?`, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Store) UpdateSubtitleJobProgress(id int64, totalWorks, processedWorks, subtitleCount, skippedWorks int, message, directory string) error {
	_, err := s.db.Exec(`UPDATE subtitle_jobs SET total_works=?, processed_works=?, subtitle_count=?, skipped_works=?, progress_message=?, output_directory=? WHERE id=?`,
		totalWorks, processedWorks, subtitleCount, skippedWorks, strings.TrimSpace(message), strings.TrimSpace(directory), id)
	return err
}

func (s *Store) FinishSubtitleJob(id int64, status string, subtitleWorks int, message string) error {
	_, err := s.db.Exec(`UPDATE subtitle_jobs SET status=?, subtitle_count=?, error=?, finished_at=? WHERE id=?`, status, subtitleWorks, strings.TrimSpace(message), time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func (s *Store) SubtitleJobs(limit int) ([]SubtitleJob, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(subtitleJobSelectSQL+`
FROM subtitle_jobs j JOIN creators c ON c.id=j.creator_id LEFT JOIN works w ON w.id=j.work_id ORDER BY j.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []SubtitleJob
	for rows.Next() {
		job, err := scanSubtitleJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) SubtitleJobsPage(page, pageSize int) (SubtitleJobsPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM subtitle_jobs`).Scan(&total); err != nil {
		return SubtitleJobsPage{}, err
	}
	offset := (page - 1) * pageSize
	rows, err := s.db.Query(subtitleJobSelectSQL+`
FROM subtitle_jobs j JOIN creators c ON c.id=j.creator_id LEFT JOIN works w ON w.id=j.work_id ORDER BY j.id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	if err != nil {
		return SubtitleJobsPage{}, err
	}
	defer rows.Close()
	jobs := make([]SubtitleJob, 0, pageSize)
	for rows.Next() {
		job, err := scanSubtitleJob(rows)
		if err != nil {
			return SubtitleJobsPage{}, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return SubtitleJobsPage{}, err
	}
	totalPages := total / pageSize
	if total%pageSize != 0 {
		totalPages++
	}
	return SubtitleJobsPage{Jobs: jobs, Page: page, PageSize: pageSize, Total: total, TotalPages: totalPages}, nil
}

func (s *Store) RunningSubtitleJobs() ([]SubtitleJob, error) {
	rows, err := s.db.Query(subtitleJobSelectSQL + `
FROM subtitle_jobs j JOIN creators c ON c.id=j.creator_id LEFT JOIN works w ON w.id=j.work_id WHERE j.status='running' ORDER BY j.started_at DESC, j.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var jobs []SubtitleJob
	for rows.Next() {
		job, err := scanSubtitleJob(rows)
		if err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (s *Store) DeleteSubtitleJob(id int64) error {
	var status string
	if err := s.db.QueryRow(`SELECT status FROM subtitle_jobs WHERE id=?`, id).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if status == "running" {
		return errors.New("running subtitle job cannot be deleted")
	}
	_, err := s.db.Exec(`DELETE FROM subtitle_jobs WHERE id=?`, id)
	return err
}

func (s *Store) ClearSubtitleJobs() error {
	_, err := s.db.Exec(`DELETE FROM subtitle_jobs`)
	return err
}

func (s *Store) RetrySubtitleJob(id int64) (SubtitleJob, error) {
	original, err := s.SubtitleJob(id)
	if err != nil {
		return SubtitleJob{}, err
	}
	if original.Status != "failed" {
		return SubtitleJob{}, errors.New("only failed subtitle jobs can be retried")
	}
	result, err := s.db.Exec(`INSERT INTO subtitle_jobs(creator_id, work_id, queue_id, run_id, retry_of_job_id, attempt, source, status) VALUES(?, ?, ?, ?, ?, ?, ?, 'pending')`,
		original.CreatorID, original.WorkID, original.QueueID, original.RunID, original.ID, original.Attempt+1, original.Source)
	if err != nil {
		return SubtitleJob{}, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return SubtitleJob{}, err
	}
	return s.SubtitleJob(newID)
}

func (s *Store) SaveUpdateCookie(value string) error {
	_, err := s.db.Exec(`UPDATE update_cookie_settings SET cookie_value=?, updated_at=?, validation_status='unknown', validation_message='' WHERE id=1`, value, time.Now().UTC().Format(time.RFC3339))
	return err
}

func (s *Store) UpdateCookie() (UpdateCookie, error) {
	var value string
	var cookie UpdateCookie
	err := s.db.QueryRow(`SELECT cookie_value, updated_at, validation_status, validation_message FROM update_cookie_settings WHERE id=1`).Scan(&value, &cookie.UpdatedAt, &cookie.ValidationStatus, &cookie.ValidationMessage)
	if err != nil {
		return cookie, err
	}
	cookie.Configured = strings.TrimSpace(value) != ""
	cookie.Value = value
	return cookie, nil
}

func (s *Store) updateCookieValue() (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT cookie_value FROM update_cookie_settings WHERE id=1`).Scan(&value)
	return value, err
}

func (s *Store) MarkUpdateCookieValidation(status, message string) error {
	_, err := s.db.Exec(`UPDATE update_cookie_settings SET validation_status=?, validation_message=?, updated_at=? WHERE id=1`, status, message, time.Now().UTC().Format(time.RFC3339))
	return err
}

type AuditEventInput struct {
	ActorUserID int64
	ActorName   string
	EventType   string
	TargetType  string
	TargetID    int64
	Message     string
	DetailJSON  string
}

type SystemEventInput struct {
	EventType  string
	Severity   string
	Message    string
	DetailJSON string
}

func (s *Store) RecordAuditEvent(input AuditEventInput) error {
	_, err := s.db.Exec(`INSERT INTO audit_events(actor_user_id, actor_name, event_type, target_type, target_id, message, detail_json, created_at)
VALUES(?, ?, ?, ?, ?, ?, ?, ?)`,
		input.ActorUserID,
		input.ActorName,
		input.EventType,
		input.TargetType,
		input.TargetID,
		input.Message,
		input.DetailJSON,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) RecordSystemEvent(input SystemEventInput) error {
	severity := strings.TrimSpace(input.Severity)
	if severity == "" {
		severity = "info"
	}
	_, err := s.db.Exec(`INSERT INTO system_events(event_type, severity, message, detail_json, created_at)
VALUES(?, ?, ?, ?, ?)`,
		input.EventType,
		severity,
		input.Message,
		input.DetailJSON,
		time.Now().UTC().Format(time.RFC3339),
	)
	return err
}

func (s *Store) AuditEvents(page, pageSize int) (LogEventsPage, error) {
	return s.logEventsPage("audit_events", page, pageSize, true)
}

func (s *Store) SystemEvents(page, pageSize int) (LogEventsPage, error) {
	return s.logEventsPage("system_events", page, pageSize, false)
}

func (s *Store) logEventsPage(table string, page, pageSize int, includeActor bool) (LogEventsPage, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	var total int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&total); err != nil {
		return LogEventsPage{}, err
	}
	offset := (page - 1) * pageSize
	var (
		rows *sql.Rows
		err  error
	)
	if includeActor {
		rows, err = s.db.Query(`SELECT id, actor_user_id, actor_name, event_type, target_type, target_id, '' AS severity, message, detail_json, created_at
FROM `+table+` ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	} else {
		rows, err = s.db.Query(`SELECT id, 0 AS actor_user_id, '' AS actor_name, event_type, '' AS target_type, 0 AS target_id, severity, message, detail_json, created_at
FROM `+table+` ORDER BY id DESC LIMIT ? OFFSET ?`, pageSize, offset)
	}
	if err != nil {
		return LogEventsPage{}, err
	}
	defer rows.Close()
	events := make([]LogEvent, 0, pageSize)
	for rows.Next() {
		var event LogEvent
		var created string
		if err := rows.Scan(&event.ID, &event.ActorUserID, &event.ActorName, &event.EventType, &event.TargetType, &event.TargetID, &event.Severity, &event.Message, &event.DetailJSON, &created); err != nil {
			return LogEventsPage{}, err
		}
		event.CreatedAt = parseDBTime(created)
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return LogEventsPage{}, err
	}
	totalPages := total / pageSize
	if total%pageSize != 0 {
		totalPages++
	}
	return LogEventsPage{
		Events:     events,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

func scanDirectory(scanner interface{ Scan(...any) error }) (Directory, error) {
	var dir Directory
	var last sql.NullString
	var created string
	if err := scanner.Scan(&dir.ID, &dir.Path, &dir.Name, &dir.Status, &last, &created); err != nil {
		return Directory{}, err
	}
	if last.Valid && last.String != "" {
		t, err := time.Parse(time.RFC3339, last.String)
		if err == nil {
			dir.LastScannedAt = &t
		}
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", created)
	}
	if err == nil {
		dir.CreatedAt = t
	}
	return dir, nil
}

func scanWork(scanner interface{ Scan(...any) error }) (Work, error) {
	var work Work
	var tagsJSON, created string
	if err := scanner.Scan(&work.ID, &work.DirectoryID, &work.CreatorID, &work.CreatorName, &work.CreatorAvatarURL, &work.CreatorBio, &work.AwemeID, &work.WorkType, &work.Title, &work.Description, &work.MusicTitle, &work.SourceURL, &work.CoverURL, &work.CoverFile, &tagsJSON, &work.PublishedAt, &created); err != nil {
		return Work{}, err
	}
	work.BVID = work.AwemeID
	if work.CreatorAvatarURL == "" && work.CreatorID > 0 {
		work.CreatorAvatarURL = "/api/creators/" + strconv.FormatInt(work.CreatorID, 10) + "/avatar"
	}
	if work.CoverURL == "" && work.CoverFile != "" {
		work.CoverURL = "/api/works/" + strconv.FormatInt(work.ID, 10) + "/cover"
	}
	_ = json.Unmarshal([]byte(tagsJSON), &work.Tags)
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		t, err = time.Parse("2006-01-02 15:04:05", created)
	}
	if err == nil {
		work.CreatedAt = t
	}
	return work, nil
}

func scanCreatorSummary(scanner interface{ Scan(...any) error }) (CreatorSummary, error) {
	var summary CreatorSummary
	if err := scanner.Scan(&summary.ID, &summary.Name, &summary.AvatarURL, &summary.AvatarFile, &summary.ProfileURL, &summary.BilibiliSecUID); err != nil {
		return CreatorSummary{}, err
	}
	return summary, nil
}

func scanFavoriteFolder(scanner interface{ Scan(...any) error }) (FavoriteFolder, error) {
	var folder FavoriteFolder
	var isDefault int
	var created, updated string
	if err := scanner.Scan(&folder.ID, &folder.UserID, &folder.Type, &folder.Name, &isDefault, &created, &updated); err != nil {
		return FavoriteFolder{}, err
	}
	folder.IsDefault = isDefault == 1
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		folder.CreatedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04:05", created); err == nil {
		folder.CreatedAt = t
	}
	if t, err := time.Parse(time.RFC3339, updated); err == nil {
		folder.UpdatedAt = t
	} else if t, err := time.Parse("2006-01-02 15:04:05", updated); err == nil {
		folder.UpdatedAt = t
	}
	return folder, nil
}

func scanUpdateQueue(scanner interface{ Scan(...any) error }) (UpdateQueue, error) {
	var queue UpdateQueue
	var isDefault, enabled int
	var created, updated string
	if err := scanner.Scan(&queue.ID, &queue.Name, &queue.Cron, &isDefault, &enabled, &queue.NextRunAt, &queue.LastRunSummary, &created, &updated, &queue.SubscribedCount); err != nil {
		return UpdateQueue{}, err
	}
	queue.IsDefault = isDefault == 1
	queue.Enabled = enabled == 1
	queue.CreatedAt = parseDBTime(created)
	queue.UpdatedAt = parseDBTime(updated)
	return queue, nil
}

func scanSubtitleQueue(scanner interface{ Scan(...any) error }) (SubtitleQueue, error) {
	var queue SubtitleQueue
	var isDefault, enabled int
	var created, updated string
	if err := scanner.Scan(&queue.ID, &queue.Name, &queue.Cron, &isDefault, &enabled, &queue.NextRunAt, &queue.LastRunSummary, &created, &updated, &queue.SubscribedCount); err != nil {
		return SubtitleQueue{}, err
	}
	queue.IsDefault = isDefault == 1
	queue.Enabled = enabled == 1
	queue.CreatedAt = parseDBTime(created)
	queue.UpdatedAt = parseDBTime(updated)
	return queue, nil
}

func scanUpdateRun(scanner interface{ Scan(...any) error }) (UpdateRun, error) {
	var run UpdateRun
	var enqueued string
	if err := scanner.Scan(&run.ID, &run.QueueID, &run.QueueName, &enqueued, &run.StartedAt, &run.FinishedAt, &run.TriggerSource, &run.TotalCreators, &run.SucceededCreators, &run.FailedCreators, &run.AddedWorks, &run.Status, &run.Summary); err != nil {
		return UpdateRun{}, err
	}
	run.EnqueuedAt = parseDBTime(enqueued)
	run.TriggerSource = normalizeUpdateTriggerSource(run.TriggerSource)
	return run, nil
}

const updateJobSelectSQL = `SELECT j.id, j.creator_id, c.name, j.queue_id, j.run_id, j.retry_of_job_id, j.attempt, j.status, j.trigger_source, j.added_works, j.total_works, j.processed_works, j.downloaded_media, j.skipped_works, j.download_speed_bps, j.progress_message, j.download_directory, j.download_notice, j.error, j.created_at, j.started_at, j.finished_at`

func scanUpdateJob(scanner interface{ Scan(...any) error }) (UpdateJob, error) {
	var job UpdateJob
	var created string
	if err := scanner.Scan(&job.ID, &job.CreatorID, &job.CreatorName, &job.QueueID, &job.RunID, &job.RetryOfJobID, &job.Attempt, &job.Status, &job.TriggerSource, &job.AddedWorks, &job.TotalWorks, &job.ProcessedWorks, &job.DownloadedMedia, &job.SkippedWorks, &job.DownloadSpeedBps, &job.ProgressMessage, &job.DownloadDirectory, &job.DownloadNotice, &job.Error, &created, &job.StartedAt, &job.FinishedAt); err != nil {
		return UpdateJob{}, err
	}
	job.CreatedAt = parseDBTime(created)
	job.TriggerSource = normalizeUpdateTriggerSource(job.TriggerSource)
	return job, nil
}

func normalizeUpdateTriggerSource(value string) string {
	switch strings.TrimSpace(value) {
	case "scheduled":
		return "scheduled"
	default:
		return "manual"
	}
}

const subtitleJobSelectSQL = `SELECT j.id, j.creator_id, c.name, j.work_id, COALESCE(w.title, ''), j.queue_id, j.run_id, j.retry_of_job_id, j.attempt, j.source, j.status, j.total_works, j.processed_works, j.subtitle_count, j.skipped_works, j.progress_message, j.output_directory, j.error, j.created_at, j.started_at, j.finished_at`

func scanSubtitleJob(scanner interface{ Scan(...any) error }) (SubtitleJob, error) {
	var job SubtitleJob
	var created string
	if err := scanner.Scan(&job.ID, &job.CreatorID, &job.CreatorName, &job.WorkID, &job.WorkTitle, &job.QueueID, &job.RunID, &job.RetryOfJobID, &job.Attempt, &job.Source, &job.Status, &job.TotalWorks, &job.ProcessedWorks, &job.SubtitleCount, &job.SkippedWorks, &job.ProgressMessage, &job.OutputDirectory, &job.Error, &created, &job.StartedAt, &job.FinishedAt); err != nil {
		return SubtitleJob{}, err
	}
	job.CreatedAt = parseDBTime(created)
	return job, nil
}

func parseDBTime(value string) time.Time {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return t
	}
	return time.Time{}
}

var ErrNotFound = errors.New("not found")
