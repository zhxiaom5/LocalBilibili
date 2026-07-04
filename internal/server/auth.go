package server

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	defaultAdminUsername  = "ted"
	defaultAdminPassword  = "ted"
	passwordIterations    = 120000
	sessionCookieName     = "localbilibili_session"
	sessionLifetime       = 30 * 24 * time.Hour
	passwordSaltByteCount = 16
	sessionTokenByteCount = 32

	userRoleUser       = "user"
	userRoleAdmin      = "admin"
	userRoleSuperAdmin = "super_admin"
)

func (s *Store) ensureBootstrapAdmin() error {
	count, err := s.userCount()
	if err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	salt, hash, err := passwordRecord(defaultAdminPassword)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO users(username, password_salt, password_hash, password_iterations, is_admin, role, created_at, updated_at) VALUES(?, ?, ?, ?, 1, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`,
		defaultAdminUsername, salt, hash, passwordIterations, userRoleSuperAdmin)
	if err != nil {
		return err
	}
	user, err := s.UserByUsername(defaultAdminUsername)
	if err != nil {
		return err
	}
	return s.ensureDefaultFavoriteFoldersForUser(user.ID)
}

func (s *Store) Users() ([]User, error) {
	rows, err := s.db.Query(`SELECT id, username, is_admin, role, created_at, updated_at FROM users ORDER BY is_admin DESC, username COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	users := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}
	return users, rows.Err()
}

func (s *Store) User(id int64) (User, error) {
	row := s.db.QueryRow(`SELECT id, username, is_admin, role, created_at, updated_at FROM users WHERE id=?`, id)
	return scanUser(row)
}

func (s *Store) UserByUsername(username string) (User, error) {
	row := s.db.QueryRow(`SELECT id, username, is_admin, role, created_at, updated_at FROM users WHERE username=?`, strings.TrimSpace(username))
	return scanUser(row)
}

func (s *Store) Authenticate(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if err := s.ensureReservedSuperAdmin(username); err != nil {
		return User{}, err
	}
	var id int64
	var storedUsername string
	var isAdmin int
	var role string
	var salt, hash string
	var iterations int
	var created, updated string
	err := s.db.QueryRow(`SELECT id, username, is_admin, role, password_salt, password_hash, password_iterations, created_at, updated_at FROM users WHERE username=?`, username).
		Scan(&id, &storedUsername, &isAdmin, &role, &salt, &hash, &iterations, &created, &updated)
	if err != nil {
		return User{}, err
	}
	if !verifyPassword(password, salt, hash, iterations) {
		return User{}, errors.New("invalid username or password")
	}
	return userFromRecord(id, storedUsername, isAdmin, role, created, updated), nil
}

func (s *Store) ensureReservedSuperAdmin(username string) error {
	if !strings.EqualFold(strings.TrimSpace(username), defaultAdminUsername) {
		return nil
	}
	_, err := s.db.Exec(`UPDATE users
SET is_admin=1, role=?, updated_at=CURRENT_TIMESTAMP
WHERE lower(username)=lower(?) AND (is_admin<>1 OR role<>?)`,
		userRoleSuperAdmin, defaultAdminUsername, userRoleSuperAdmin)
	if err != nil {
		return err
	}
	return nil
}

func (s *Store) CreateUser(username, password string) (User, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return User{}, errors.New("username is required")
	}
	if password == "" {
		return User{}, errors.New("password is required")
	}
	salt, hash, err := passwordRecord(password)
	if err != nil {
		return User{}, err
	}
	if _, err := s.db.Exec(`INSERT INTO users(username, password_salt, password_hash, password_iterations, is_admin, role, created_at, updated_at) VALUES(?, ?, ?, ?, 0, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)`, username, salt, hash, passwordIterations, userRoleUser); err != nil {
		return User{}, err
	}
	user, err := s.UserByUsername(username)
	if err != nil {
		return User{}, err
	}
	if err := s.ensureDefaultFavoriteFoldersForUser(user.ID); err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *Store) ResetUserPassword(id int64, password string) (User, error) {
	if password == "" {
		return User{}, errors.New("password is required")
	}
	salt, hash, err := passwordRecord(password)
	if err != nil {
		return User{}, err
	}
	if _, err := s.db.Exec(`UPDATE users SET password_salt=?, password_hash=?, password_iterations=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`, salt, hash, passwordIterations, id); err != nil {
		return User{}, err
	}
	return s.User(id)
}

func (s *Store) ChangePassword(id int64, oldPassword, newPassword string) (User, error) {
	var salt, hash string
	var iterations int
	if err := s.db.QueryRow(`SELECT password_salt, password_hash, password_iterations FROM users WHERE id=?`, id).Scan(&salt, &hash, &iterations); err != nil {
		return User{}, err
	}
	if !verifyPassword(oldPassword, salt, hash, iterations) {
		return User{}, errors.New("old password is incorrect")
	}
	return s.ResetUserPassword(id, newPassword)
}

func (s *Store) DeleteUser(id int64) error {
	var role string
	if err := s.db.QueryRow(`SELECT role FROM users WHERE id=?`, id).Scan(&role); err != nil {
		return err
	}
	if normalizeUserRole(role, true) == userRoleSuperAdmin {
		return errors.New("cannot delete super admin user")
	}
	_, err := s.db.Exec(`DELETE FROM users WHERE id=?`, id)
	return err
}

func (s *Store) CreateSession(userID int64) (string, error) {
	token, err := randomToken()
	if err != nil {
		return "", err
	}
	now := time.Now().UTC()
	expiresAt := now.Add(sessionLifetime).Format(time.RFC3339)
	if _, err := s.db.Exec(`INSERT INTO sessions(user_id, token_hash, created_at, expires_at, last_used_at) VALUES(?, ?, ?, ?, ?)`,
		userID, hashToken(token), now.Format(time.RFC3339), expiresAt, now.Format(time.RFC3339)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hashToken(token))
	return err
}

func (s *Store) UserBySession(token string) (User, error) {
	var user User
	var created, updated, expires, role string
	row := s.db.QueryRow(`SELECT u.id, u.username, u.is_admin, u.role, u.created_at, u.updated_at, s.expires_at
FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.token_hash=?`, hashToken(token))
	if err := row.Scan(&user.ID, &user.Username, &user.IsAdmin, &role, &created, &updated, &expires); err != nil {
		return User{}, err
	}
	if expiresAt, err := time.Parse(time.RFC3339, expires); err == nil && time.Now().UTC().After(expiresAt) {
		_, _ = s.db.Exec(`DELETE FROM sessions WHERE token_hash=?`, hashToken(token))
		return User{}, sql.ErrNoRows
	}
	return userFromRecord(user.ID, user.Username, boolToInt(user.IsAdmin), role, created, updated), nil
}

func (s *Store) userCount() (int64, error) {
	var count int64
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func passwordRecord(password string) (salt string, hash string, err error) {
	rawSalt := make([]byte, passwordSaltByteCount)
	if _, err := rand.Read(rawSalt); err != nil {
		return "", "", err
	}
	key := pbkdf2SHA256([]byte(password), rawSalt, passwordIterations, 32)
	return encodeValue(rawSalt), encodeValue(key), nil
}

func verifyPassword(password, saltValue, hashValue string, iterations int) bool {
	salt, err := decodeValue(saltValue)
	if err != nil {
		return false
	}
	expected, err := decodeValue(hashValue)
	if err != nil {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, iterations, len(expected))
	return hmac.Equal(actual, expected)
}

func randomToken() (string, error) {
	raw := make([]byte, sessionTokenByteCount)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return encodeValue(raw), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func encodeValue(raw []byte) string {
	return base64.RawStdEncoding.EncodeToString(raw)
}

func decodeValue(value string) ([]byte, error) {
	return base64.RawStdEncoding.DecodeString(value)
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLen int) []byte {
	hashLen := sha256.Size
	blocks := (keyLen + hashLen - 1) / hashLen
	result := make([]byte, 0, blocks*hashLen)
	for block := 1; block <= blocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{
			byte(block >> 24),
			byte(block >> 16),
			byte(block >> 8),
			byte(block),
		})
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iterations; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		result = append(result, t...)
	}
	return result[:keyLen]
}

func scanUser(scanner interface{ Scan(...any) error }) (User, error) {
	var user User
	var isAdmin int
	var role, created, updated string
	if err := scanner.Scan(&user.ID, &user.Username, &isAdmin, &role, &created, &updated); err != nil {
		return User{}, err
	}
	return userFromRecord(user.ID, user.Username, isAdmin, role, created, updated), nil
}

func userFromRecord(id int64, username string, isAdmin int, role, created, updated string) User {
	normalizedRole := normalizeUserRole(role, isAdmin == 1)
	return User{
		ID:           id,
		Username:     username,
		Role:         normalizedRole,
		IsAdmin:      normalizedRole == userRoleAdmin || normalizedRole == userRoleSuperAdmin,
		IsSuperAdmin: normalizedRole == userRoleSuperAdmin,
		CanUpdate:    normalizedRole == userRoleAdmin || normalizedRole == userRoleSuperAdmin,
		CreatedAt:    parseTime(created),
		UpdatedAt:    parseTime(updated),
	}
}

func normalizeUserRole(role string, isAdmin bool) string {
	switch strings.TrimSpace(role) {
	case userRoleSuperAdmin:
		return userRoleSuperAdmin
	case userRoleAdmin:
		return userRoleAdmin
	case userRoleUser:
		return userRoleUser
	}
	if isAdmin {
		return userRoleSuperAdmin
	}
	return userRoleUser
}

func parseTime(value string) time.Time {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t
	}
	if t, err := time.Parse("2006-01-02 15:04:05", value); err == nil {
		return t
	}
	return time.Time{}
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func currentSessionToken(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || strings.TrimSpace(cookie.Value) == "" {
		return "", false
	}
	return cookie.Value, true
}

func (s *Store) SessionFromRequest(r *http.Request) (User, error) {
	token, ok := currentSessionToken(r)
	if !ok {
		return User{}, sql.ErrNoRows
	}
	return s.UserBySession(token)
}

func (s *Store) MustDeleteSession(r *http.Request) error {
	token, ok := currentSessionToken(r)
	if !ok {
		return nil
	}
	return s.DeleteSession(token)
}

func (s *Store) SessionCookie(token string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   false,
		MaxAge:   int(sessionLifetime.Seconds()),
	}
}

func (s *Store) ExpiredSessionCookie() *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
}
