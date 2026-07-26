package server

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

func (s *Store) migrateLearning() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS learning_queues (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT NOT NULL UNIQUE, cron TEXT NOT NULL, enabled INTEGER NOT NULL DEFAULT 1, next_run_at TEXT NOT NULL DEFAULT '', last_run_summary TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP);
CREATE TABLE IF NOT EXISTS learning_queue_creators (queue_id INTEGER NOT NULL, creator_id INTEGER NOT NULL, created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, UNIQUE(queue_id,creator_id), FOREIGN KEY(queue_id) REFERENCES learning_queues(id) ON DELETE CASCADE, FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE);
CREATE TABLE IF NOT EXISTS learning_runs (id INTEGER PRIMARY KEY AUTOINCREMENT, queue_id INTEGER NOT NULL DEFAULT 0, queue_name TEXT NOT NULL DEFAULT '', enqueued_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, started_at TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '', total_jobs INTEGER NOT NULL DEFAULT 0, succeeded INTEGER NOT NULL DEFAULT 0, failed INTEGER NOT NULL DEFAULT 0, status TEXT NOT NULL DEFAULT 'queued', summary TEXT NOT NULL DEFAULT '');
CREATE TABLE IF NOT EXISTS learning_jobs (id INTEGER PRIMARY KEY AUTOINCREMENT, creator_id INTEGER NOT NULL, work_id INTEGER, queue_id INTEGER NOT NULL DEFAULT 0, run_id INTEGER NOT NULL DEFAULT 0, retry_of_job_id INTEGER NOT NULL DEFAULT 0, attempt INTEGER NOT NULL DEFAULT 1, source TEXT NOT NULL DEFAULT 'manual', status TEXT NOT NULL DEFAULT 'pending', total_works INTEGER NOT NULL DEFAULT 0, processed_works INTEGER NOT NULL DEFAULT 0, progress_message TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, started_at TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '', FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE, FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE);
CREATE INDEX IF NOT EXISTS learning_jobs_status_idx ON learning_jobs(status,id);
CREATE TABLE IF NOT EXISTS mind_map_records (id INTEGER PRIMARY KEY AUTOINCREMENT, creator_id INTEGER NOT NULL, work_id INTEGER, map_id TEXT NOT NULL DEFAULT '', map_url TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', summary_json TEXT NOT NULL DEFAULT '', content_fingerprint TEXT NOT NULL DEFAULT '', prompt_version TEXT NOT NULL DEFAULT 'v1', sync_status TEXT NOT NULL DEFAULT 'pending', updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE, FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE);
CREATE UNIQUE INDEX IF NOT EXISTS mind_map_records_scope_idx ON mind_map_records(creator_id, COALESCE(work_id,0));`)
	if err != nil {
		return err
	}
	if err := s.migrateLearningCreatorScope(); err != nil {
		return err
	}
	for _, column := range []struct{ name, definition string }{{"started_at", "TEXT NOT NULL DEFAULT ''"}, {"finished_at", "TEXT NOT NULL DEFAULT ''"}, {"total_jobs", "INTEGER NOT NULL DEFAULT 0"}, {"succeeded", "INTEGER NOT NULL DEFAULT 0"}, {"failed", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := s.addColumnIfMissing("learning_runs", column.name, column.definition); err != nil {
			return err
		}
	}
	_, err = s.db.Exec(`INSERT INTO learning_queues(name,cron,enabled,next_run_at,created_at,updated_at) SELECT '默认学习队列','0 4 * * *',1,'',CURRENT_TIMESTAMP,CURRENT_TIMESTAMP WHERE NOT EXISTS (SELECT 1 FROM learning_queues);`)
	return err
}

// migrateLearningCreatorScope replaces the original 0-sentinel work foreign key.
// A NULL work_id represents a creator aggregate; public API values retain 0.
func (s *Store) migrateLearningCreatorScope() error {
	var jobsSQL, mapsSQL string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='learning_jobs'`).Scan(&jobsSQL); err != nil {
		return err
	}
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='mind_map_records'`).Scan(&mapsSQL); err != nil {
		return err
	}
	if !strings.Contains(strings.ToLower(jobsSQL), "work_id integer not null") && !strings.Contains(strings.ToLower(mapsSQL), "work_id integer not null") {
		return nil
	}
	if _, err := s.db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer s.db.Exec(`PRAGMA foreign_keys = ON`)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`
CREATE TABLE learning_jobs_new (id INTEGER PRIMARY KEY AUTOINCREMENT, creator_id INTEGER NOT NULL, work_id INTEGER, queue_id INTEGER NOT NULL DEFAULT 0, run_id INTEGER NOT NULL DEFAULT 0, retry_of_job_id INTEGER NOT NULL DEFAULT 0, attempt INTEGER NOT NULL DEFAULT 1, source TEXT NOT NULL DEFAULT 'manual', status TEXT NOT NULL DEFAULT 'pending', total_works INTEGER NOT NULL DEFAULT 0, processed_works INTEGER NOT NULL DEFAULT 0, progress_message TEXT NOT NULL DEFAULT '', error TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, started_at TEXT NOT NULL DEFAULT '', finished_at TEXT NOT NULL DEFAULT '', FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE, FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE);
INSERT INTO learning_jobs_new SELECT id,creator_id,NULLIF(work_id,0),queue_id,run_id,retry_of_job_id,attempt,source,status,total_works,processed_works,progress_message,error,created_at,started_at,finished_at FROM learning_jobs;
DROP TABLE learning_jobs;
ALTER TABLE learning_jobs_new RENAME TO learning_jobs;
CREATE INDEX learning_jobs_status_idx ON learning_jobs(status,id);
CREATE TABLE mind_map_records_new (id INTEGER PRIMARY KEY AUTOINCREMENT, creator_id INTEGER NOT NULL, work_id INTEGER, map_id TEXT NOT NULL DEFAULT '', map_url TEXT NOT NULL DEFAULT '', title TEXT NOT NULL DEFAULT '', summary_json TEXT NOT NULL DEFAULT '', content_fingerprint TEXT NOT NULL DEFAULT '', prompt_version TEXT NOT NULL DEFAULT 'v1', sync_status TEXT NOT NULL DEFAULT 'pending', updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP, FOREIGN KEY(creator_id) REFERENCES creators(id) ON DELETE CASCADE, FOREIGN KEY(work_id) REFERENCES works(id) ON DELETE CASCADE);
INSERT INTO mind_map_records_new SELECT id,creator_id,NULLIF(work_id,0),map_id,map_url,title,summary_json,content_fingerprint,prompt_version,sync_status,updated_at FROM mind_map_records;
DROP TABLE mind_map_records;
ALTER TABLE mind_map_records_new RENAME TO mind_map_records;
CREATE UNIQUE INDEX mind_map_records_scope_idx ON mind_map_records(creator_id, COALESCE(work_id,0));`); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) LearningSubtitle(workID int64) (string, error) {
	var path, status string
	err := s.db.QueryRow(`SELECT text_path,status FROM subtitle_records WHERE work_id=?`, workID).Scan(&path, &status)
	if err != nil {
		return "", err
	}
	if status != "ready" || strings.TrimSpace(path) == "" {
		return "", errors.New("缺少字幕")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

const learningJobSelect = `SELECT j.id,j.creator_id,c.name,COALESCE(j.work_id,0),COALESCE(w.title,''),j.queue_id,j.run_id,j.retry_of_job_id,j.attempt,j.source,j.status,j.processed_works,j.total_works,j.progress_message,j.error,j.created_at,j.started_at,j.finished_at FROM learning_jobs j JOIN creators c ON c.id=j.creator_id LEFT JOIN works w ON w.id=j.work_id `

func scanLearningJob(row interface{ Scan(...any) error }) (LearningJob, error) {
	var v LearningJob
	var created string
	err := row.Scan(&v.ID, &v.CreatorID, &v.CreatorName, &v.WorkID, &v.WorkTitle, &v.QueueID, &v.RunID, &v.RetryOfJobID, &v.Attempt, &v.Source, &v.Status, &v.ProcessedWorks, &v.TotalWorks, &v.ProgressMessage, &v.Error, &created, &v.StartedAt, &v.FinishedAt)
	v.CreatedAt = parseDBTime(created)
	return v, err
}
func (s *Store) LearningJob(id int64) (LearningJob, error) {
	return scanLearningJob(s.db.QueryRow(learningJobSelect+`WHERE j.id=?`, id))
}
func (s *Store) NextPendingLearningJob() (LearningJob, error) {
	return scanLearningJob(s.db.QueryRow(learningJobSelect + `WHERE j.status='pending' ORDER BY j.id LIMIT 1`))
}

// RecoverInterruptedLearningJobs makes an interrupted single-process worker
// observable and retryable after an app restart. The old process cannot still
// be running after the service process is replaced, so keeping it as running
// would leave the queue permanently stuck.
func (s *Store) RecoverInterruptedLearningJobs() (int64, error) {
	result, err := s.db.Exec(`UPDATE learning_jobs SET status='pending',processed_works=0,total_works=0,progress_message='应用重启后等待恢复',error='',started_at='',finished_at='' WHERE status='running'`)
	if err != nil {
		return 0, err
	}
	if _, err := s.db.Exec(`UPDATE learning_runs SET status='queued',started_at='',finished_at='',summary='应用重启后等待恢复' WHERE status='running' AND EXISTS (SELECT 1 FROM learning_jobs WHERE run_id=learning_runs.id AND status='pending')`); err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
func (s *Store) LearningJobs(limit int) ([]LearningJob, error) {
	if limit < 1 {
		limit = 30
	}
	rows, err := s.db.Query(learningJobSelect+`ORDER BY j.id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LearningJob
	for rows.Next() {
		v, e := scanLearningJob(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) AddLearningJob(creatorID, workID, queueID int64, source string) (LearningJob, error) {
	return s.AddLearningJobWithRun(creatorID, workID, queueID, 0, source)
}
func (s *Store) AddLearningJobWithRun(creatorID, workID, queueID, runID int64, source string) (LearningJob, error) {
	var id int64
	q := `SELECT id FROM learning_jobs WHERE creator_id=? AND ((?=0 AND work_id IS NULL) OR work_id=?) AND status IN ('pending','running') LIMIT 1`
	err := s.db.QueryRow(q, creatorID, workID, workID).Scan(&id)
	if err == nil {
		return s.LearningJob(id)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return LearningJob{}, err
	}
	var nullableWorkID any = workID
	if workID == 0 {
		nullableWorkID = nil
	}
	r, err := s.db.Exec(`INSERT INTO learning_jobs(creator_id,work_id,queue_id,run_id,source,status) VALUES(?,?,?,?,?, 'pending')`, creatorID, nullableWorkID, queueID, runID, strings.TrimSpace(source))
	if err != nil {
		return LearningJob{}, err
	}
	id, err = r.LastInsertId()
	if err != nil {
		return LearningJob{}, err
	}
	return s.LearningJob(id)
}
func (s *Store) CreateLearningRun(queue LearningQueue, total int) (LearningRun, error) {
	r, err := s.db.Exec(`INSERT INTO learning_runs(queue_id,queue_name,total_jobs,status,summary) VALUES(?,?,?,'queued','已加入学习队列')`, queue.ID, queue.Name, total)
	if err != nil {
		return LearningRun{}, err
	}
	id, err := r.LastInsertId()
	if err != nil {
		return LearningRun{}, err
	}
	return s.LearningRun(id)
}
func (s *Store) LearningRun(id int64) (LearningRun, error) {
	return scanLearningRun(s.db.QueryRow(`SELECT id,queue_id,queue_name,enqueued_at,started_at,finished_at,total_jobs,succeeded,failed,status,summary FROM learning_runs WHERE id=?`, id))
}
func (s *Store) LearningRuns(limit int) ([]LearningRun, error) {
	if limit < 1 {
		limit = 30
	}
	rows, err := s.db.Query(`SELECT id,queue_id,queue_name,enqueued_at,started_at,finished_at,total_jobs,succeeded,failed,status,summary FROM learning_runs ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LearningRun{}
	for rows.Next() {
		v, e := scanLearningRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func scanLearningRun(row interface{ Scan(...any) error }) (LearningRun, error) {
	var v LearningRun
	var enqueued string
	if err := row.Scan(&v.ID, &v.QueueID, &v.QueueName, &enqueued, &v.StartedAt, &v.FinishedAt, &v.TotalJobs, &v.Succeeded, &v.Failed, &v.Status, &v.Summary); err != nil {
		return v, err
	}
	v.EnqueuedAt = parseDBTime(enqueued)
	return v, nil
}

// LearningCreatorSubtitles returns every ready subtitle for a creator as one
// input document. The visible work boundaries let Codex recognize context while
// keeping the requested full-creator summary as a single model input.
func (s *Store) LearningCreatorSubtitles(creatorID int64) (string, int, error) {
	rows, err := s.db.Query(`SELECT w.id,w.title,w.published_at,sr.text_path
FROM works w JOIN subtitle_records sr ON sr.work_id=w.id
WHERE w.creator_id=? AND sr.status='ready' AND TRIM(sr.text_path)<>''
ORDER BY w.published_at,w.id`, creatorID)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	var input strings.Builder
	count := 0
	for rows.Next() {
		var workID int64
		var title, publishedAt, path string
		if err := rows.Scan(&workID, &title, &publishedAt, &path); err != nil {
			return "", 0, err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return "", 0, err
		}
		text := strings.TrimSpace(string(body))
		if text == "" {
			continue
		}
		if input.Len() > 0 {
			input.WriteString("\n\n")
		}
		chapterTitle := strings.TrimSpace(title)
		if date := subtitleChapterDate(publishedAt); date != "" {
			chapterTitle = strings.TrimSpace(date + "  " + chapterTitle)
		}
		fmt.Fprintf(&input, "===== 作品 %d：%s =====\n%s", workID, chapterTitle, text)
		count++
	}
	if err := rows.Err(); err != nil {
		return "", 0, err
	}
	if count == 0 {
		return "", 0, errors.New("缺少字幕")
	}
	return input.String(), count, nil
}
func (s *Store) MarkLearningRunning(id int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, e := s.db.Exec(`UPDATE learning_jobs SET status='running',started_at=?,finished_at='',error='' WHERE id=?`, now, id)
	if e == nil {
		_, _ = s.db.Exec(`UPDATE learning_runs SET status='running',started_at=CASE WHEN started_at='' THEN ? ELSE started_at END WHERE id=(SELECT run_id FROM learning_jobs WHERE id=?) AND (SELECT run_id FROM learning_jobs WHERE id=?)>0`, now, id, id)
	}
	return e
}
func (s *Store) FinishLearningJob(id int64, status, message string, done, total int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, e := s.db.Exec(`UPDATE learning_jobs SET status=?,error=?,processed_works=?,total_works=?,progress_message=?,finished_at=? WHERE id=?`, status, strings.TrimSpace(message), done, total, strings.TrimSpace(message), now, id)
	if e == nil {
		_, _ = s.db.Exec(`UPDATE learning_runs SET succeeded=(SELECT COUNT(*) FROM learning_jobs WHERE run_id=learning_runs.id AND status='succeeded'),failed=(SELECT COUNT(*) FROM learning_jobs WHERE run_id=learning_runs.id AND status='failed'),status=CASE WHEN (SELECT COUNT(*) FROM learning_jobs WHERE run_id=learning_runs.id AND status IN ('pending','running'))=0 THEN 'completed' ELSE 'running' END,finished_at=CASE WHEN (SELECT COUNT(*) FROM learning_jobs WHERE run_id=learning_runs.id AND status IN ('pending','running'))=0 THEN ? ELSE '' END,summary=(SELECT printf('成功 %d，失败 %d',SUM(CASE WHEN status='succeeded' THEN 1 ELSE 0 END),SUM(CASE WHEN status='failed' THEN 1 ELSE 0 END)) FROM learning_jobs WHERE run_id=learning_runs.id) WHERE id=(SELECT run_id FROM learning_jobs WHERE id=?) AND (SELECT run_id FROM learning_jobs WHERE id=?)>0`, now, id, id)
	}
	return e
}
func (s *Store) DeleteLearningJob(id int64) error {
	r, e := s.db.Exec(`DELETE FROM learning_jobs WHERE id=? AND status!='running'`, id)
	if e != nil {
		return e
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) RetryLearningJob(id int64) (LearningJob, error) {
	v, e := s.LearningJob(id)
	if e != nil {
		return v, e
	}
	if v.Status != "failed" {
		return v, errors.New("only failed learning jobs can be retried")
	}
	var nullableWorkID any = v.WorkID
	if v.WorkID == 0 {
		nullableWorkID = nil
	}
	r, e := s.db.Exec(`INSERT INTO learning_jobs(creator_id,work_id,queue_id,retry_of_job_id,attempt,source,status) VALUES(?,?,?,?,?,?, 'pending')`, v.CreatorID, nullableWorkID, v.QueueID, v.ID, v.Attempt+1, v.Source)
	if e != nil {
		return v, e
	}
	n, e := r.LastInsertId()
	if e != nil {
		return v, e
	}
	return s.LearningJob(n)
}
func (s *Store) ClearLearningJobs() error { _, e := s.db.Exec(`DELETE FROM learning_jobs`); return e }
func (s *Store) UpsertMindMap(v MindMapRecord, summary string) error {
	var nullableWorkID any = v.WorkID
	if v.WorkID == 0 {
		nullableWorkID = nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	r, e := s.db.Exec(`UPDATE mind_map_records SET map_id=?,map_url=?,title=?,summary_json=?,content_fingerprint=?,sync_status=?,updated_at=? WHERE creator_id=? AND ((?=0 AND work_id IS NULL) OR work_id=?)`, v.MapID, v.MapURL, v.Title, summary, v.ContentFingerprint, v.SyncStatus, now, v.CreatorID, v.WorkID, v.WorkID)
	if e != nil {
		return e
	}
	if n, _ := r.RowsAffected(); n > 0 {
		return nil
	}
	_, e = s.db.Exec(`INSERT INTO mind_map_records(creator_id,work_id,map_id,map_url,title,summary_json,content_fingerprint,sync_status,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, v.CreatorID, nullableWorkID, v.MapID, v.MapURL, v.Title, summary, v.ContentFingerprint, v.SyncStatus, now)
	return e
}
func (s *Store) MindMap(creatorID, workID int64) (MindMapRecord, error) {
	var v MindMapRecord
	e := s.db.QueryRow(`SELECT id,creator_id,COALESCE(work_id,0),map_id,map_url,title,summary_json,content_fingerprint,sync_status,updated_at FROM mind_map_records WHERE creator_id=? AND ((?=0 AND work_id IS NULL) OR work_id=?)`, creatorID, workID, workID).Scan(&v.ID, &v.CreatorID, &v.WorkID, &v.MapID, &v.MapURL, &v.Title, &v.SummaryJSON, &v.ContentFingerprint, &v.SyncStatus, &v.UpdatedAt)
	return v, e
}

// WorkMindMaps returns the persisted single-work outlines in creator order.
func (s *Store) WorkMindMaps(creatorID int64) ([]MindMapRecord, error) {
	rows, err := s.db.Query(`SELECT m.id,m.creator_id,COALESCE(m.work_id,0),m.map_id,m.map_url,m.title,m.summary_json,m.content_fingerprint,m.sync_status,m.updated_at
FROM mind_map_records m JOIN works w ON w.id=m.work_id
WHERE m.creator_id=? AND m.work_id IS NOT NULL AND TRIM(m.summary_json)<>''
ORDER BY w.published_at,w.id`, creatorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MindMapRecord
	for rows.Next() {
		var v MindMapRecord
		if err := rows.Scan(&v.ID, &v.CreatorID, &v.WorkID, &v.MapID, &v.MapURL, &v.Title, &v.SummaryJSON, &v.ContentFingerprint, &v.SyncStatus, &v.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateMindMapURL(creatorID, workID int64, mapURL string) error {
	_, err := s.db.Exec(`UPDATE mind_map_records SET map_url=?,updated_at=? WHERE creator_id=? AND ((?=0 AND work_id IS NULL) OR work_id=?)`, mapURL, time.Now().UTC().Format(time.RFC3339), creatorID, workID, workID)
	return err
}
func (s *Store) LearningQueues() ([]LearningQueue, error) {
	rows, e := s.db.Query(`SELECT q.id,q.name,q.cron,q.enabled,q.next_run_at,q.last_run_summary,COUNT(c.creator_id) FROM learning_queues q LEFT JOIN learning_queue_creators c ON c.queue_id=q.id GROUP BY q.id ORDER BY q.id`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []LearningQueue
	for rows.Next() {
		var v LearningQueue
		if e = rows.Scan(&v.ID, &v.Name, &v.Cron, &v.Enabled, &v.NextRunAt, &v.LastRunSummary, &v.SubscribedCount); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s *Store) LearningQueue(id int64) (LearningQueue, error) {
	queues, err := s.LearningQueues()
	if err != nil {
		return LearningQueue{}, err
	}
	for _, queue := range queues {
		if queue.ID == id {
			return queue, nil
		}
	}
	return LearningQueue{}, ErrNotFound
}
func (s *Store) CreateLearningQueue(name, cron string) (LearningQueue, error) {
	if strings.TrimSpace(name) == "" || !ValidCron5(cron) {
		return LearningQueue{}, errors.New("队列名称或 cron 无效")
	}
	r, e := s.db.Exec(`INSERT INTO learning_queues(name,cron,next_run_at) VALUES(?,?,?)`, strings.TrimSpace(name), strings.TrimSpace(cron), NextCronRun(cron, time.Now()).Format(time.RFC3339))
	if e != nil {
		return LearningQueue{}, e
	}
	id, e := r.LastInsertId()
	if e != nil {
		return LearningQueue{}, e
	}
	qs, e := s.LearningQueues()
	for _, q := range qs {
		if q.ID == id {
			return q, nil
		}
	}
	return LearningQueue{}, e
}
func (s *Store) AddCreatorsToLearningQueue(queueID int64, ids []int64) error {
	for _, id := range ids {
		if id > 0 {
			if _, e := s.db.Exec(`INSERT INTO learning_queue_creators(queue_id,creator_id) VALUES(?,?) ON CONFLICT DO NOTHING`, queueID, id); e != nil {
				return e
			}
		}
	}
	return nil
}
func (s *Store) CreatorsForLearningQueue(queueID int64) ([]Creator, error) {
	rows, e := s.db.Query(`SELECT c.id,c.directory_id,c.name,c.work_count,c.bilibili_uid,c.bilibili_sec_uid,c.bilibili_profile_url,c.online_identity_status,c.avatar_url,c.avatar_file,c.bio,c.profile_updated_at FROM learning_queue_creators q JOIN creators c ON c.id=q.creator_id WHERE q.queue_id=?`, queueID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []Creator
	for rows.Next() {
		var c Creator
		e = rows.Scan(&c.ID, &c.DirectoryID, &c.Name, &c.WorkCount, &c.BilibiliUID, &c.BilibiliSecUID, &c.BilibiliProfileURL, &c.OnlineIdentityStatus, &c.AvatarURL, &c.AvatarFile, &c.Bio, &c.ProfileUpdatedAt)
		if e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, e
}
