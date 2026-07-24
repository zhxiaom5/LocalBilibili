package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	accessLogFileName   = "access.log"
	accessLogMaxBytes   = 50 * 1024 * 1024
	accessLogMaxBackups = 5
	accessLogQueueSize  = 4096
)

type AccessLogger struct {
	dir      string
	path     string
	file     *os.File
	size     int64
	mu       sync.Mutex
	records  chan string
	done     chan struct{}
	closeMux sync.Once
}

type accessLogContextKey struct{}

type accessLogRequestDetails struct {
	media *accessLogMedia
}

type accessLogMedia struct {
	ID            int64
	Type          string
	Range         string
	FileSize      int64
	OpenTime      time.Duration
	StatTime      time.Duration
	PlaySessionID string
	WorkID        string
	RangeStart    string
	RangeEnd      string
	ClientEntry   string
}

func NewAccessLogger(logDir string) (*AccessLogger, error) {
	if strings.TrimSpace(logDir) == "" {
		return nil, nil
	}
	cleanDir := filepath.Clean(logDir)
	if err := os.MkdirAll(cleanDir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(cleanDir, accessLogFileName)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	var size int64
	if stat, err := file.Stat(); err == nil {
		size = stat.Size()
	}
	logger := &AccessLogger{
		dir:     cleanDir,
		path:    path,
		file:    file,
		size:    size,
		records: make(chan string, accessLogQueueSize),
		done:    make(chan struct{}),
	}
	go logger.run()
	return logger, nil
}

func accessLogDir(databaseDir string) string {
	if logDir := strings.TrimSpace(os.Getenv("LOCALBILIBILI_LOG_DIR")); logDir != "" {
		return logDir
	}
	return filepath.Join(databaseDir, "logs")
}

func (l *AccessLogger) Middleware(next http.Handler) http.Handler {
	if l == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		details := &accessLogRequestDetails{}
		r = r.WithContext(context.WithValue(r.Context(), accessLogContextKey{}, details))
		recorder := &accessLogResponseWriter{ResponseWriter: w, start: start}
		next.ServeHTTP(recorder, r)
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		line := formatAccessLogLine(r, start, time.Since(start), status, recorder.bytes, recorder.firstWriteDelay(), recorder.writeErrString(), details)
		select {
		case l.records <- line:
		default:
			// Access logging must never block media streaming or API responses.
		}
	})
}

func (l *AccessLogger) Close() error {
	if l == nil {
		return nil
	}
	var err error
	l.closeMux.Do(func() {
		close(l.records)
		<-l.done
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.file != nil {
			err = l.file.Close()
			l.file = nil
		}
	})
	return err
}

func (l *AccessLogger) run() {
	for line := range l.records {
		_ = l.writeLine(line)
	}
	close(l.done)
}

func (l *AccessLogger) writeLine(line string) error {
	data := []byte(line + "\n")
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	if l.size+int64(len(data)) > accessLogMaxBytes {
		if err := l.rotateLocked(); err != nil {
			return err
		}
	}
	n, err := l.file.Write(data)
	l.size += int64(n)
	return err
}

func (l *AccessLogger) rotateLocked() error {
	if l.file != nil {
		if err := l.file.Close(); err != nil {
			return err
		}
		l.file = nil
	}
	for i := accessLogMaxBackups - 1; i >= 1; i-- {
		oldPath := fmt.Sprintf("%s.%d", l.path, i)
		newPath := fmt.Sprintf("%s.%d", l.path, i+1)
		if _, err := os.Stat(oldPath); err == nil {
			_ = os.Rename(oldPath, newPath)
		}
	}
	if _, err := os.Stat(l.path); err == nil {
		_ = os.Rename(l.path, l.path+".1")
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	l.file = file
	l.size = 0
	return nil
}

type accessLogResponseWriter struct {
	http.ResponseWriter
	status     int
	bytes      int64
	start      time.Time
	firstWrite time.Time
	writeErr   error
}

func (w *accessLogResponseWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *accessLogResponseWriter) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.markFirstWrite()
	n, err := w.ResponseWriter.Write(data)
	w.bytes += int64(n)
	w.recordWriteErr(err)
	return n, err
}

func (w *accessLogResponseWriter) ReadFrom(r io.Reader) (int64, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.markFirstWrite()
	if readerFrom, ok := w.ResponseWriter.(io.ReaderFrom); ok {
		n, err := readerFrom.ReadFrom(r)
		w.bytes += n
		w.recordWriteErr(err)
		return n, err
	}
	return io.Copy(accessLogBodyWriter{w: w}, r)
}

type accessLogBodyWriter struct {
	w *accessLogResponseWriter
}

func (w accessLogBodyWriter) Write(data []byte) (int, error) {
	return w.w.Write(data)
}

func (w *accessLogResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		if w.status == 0 {
			w.status = http.StatusOK
		}
		w.markFirstWrite()
		flusher.Flush()
	}
}

func (w *accessLogResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *accessLogResponseWriter) markFirstWrite() {
	if w.firstWrite.IsZero() {
		w.firstWrite = time.Now()
	}
}

func (w *accessLogResponseWriter) firstWriteDelay() time.Duration {
	if w.firstWrite.IsZero() || w.start.IsZero() {
		return 0
	}
	return w.firstWrite.Sub(w.start)
}

func (w *accessLogResponseWriter) recordWriteErr(err error) {
	if err != nil && w.writeErr == nil {
		w.writeErr = err
	}
}

func (w *accessLogResponseWriter) writeErrString() string {
	if w.writeErr == nil {
		return "-"
	}
	return w.writeErr.Error()
}

func accessLogSetMedia(r *http.Request, media accessLogMedia) {
	if r == nil {
		return
	}
	media = enrichAccessLogMedia(r, media)
	details, ok := r.Context().Value(accessLogContextKey{}).(*accessLogRequestDetails)
	if !ok || details == nil {
		return
	}
	details.media = &media
}

func enrichAccessLogMedia(r *http.Request, media accessLogMedia) accessLogMedia {
	if media.PlaySessionID == "" {
		media.PlaySessionID = strings.TrimSpace(r.URL.Query().Get("play_session_id"))
	}
	if media.WorkID == "" {
		media.WorkID = strings.TrimSpace(r.URL.Query().Get("work_id"))
	}
	if media.Range == "" {
		media.Range = r.Header.Get("Range")
	}
	media.RangeStart, media.RangeEnd = accessLogRangeBounds(media.Range)
	media.ClientEntry = accessLogClientEntry(r)
	return media
}

func formatAccessLogLine(r *http.Request, ts time.Time, elapsed time.Duration, status int, bytesSent int64, firstWrite time.Duration, writeErr string, details *accessLogRequestDetails) string {
	remoteAddr := clientIP(r.RemoteAddr)
	requestLine := r.Method + " " + sanitizedRequestURI(r.URL) + " " + r.Proto
	referer := r.Header.Get("Referer")
	if referer == "" {
		referer = "-"
	}
	userAgent := r.Header.Get("User-Agent")
	if userAgent == "" {
		userAgent = "-"
	}
	line := fmt.Sprintf(`%s - - [%s] "%s" %d %d "%s" "%s" rt=%.3f ttfb_ms=%.3f write_err="%s"`,
		accessLogToken(remoteAddr),
		ts.Format("02/Jan/2006:15:04:05 -0700"),
		accessLogQuoteValue(requestLine),
		status,
		bytesSent,
		accessLogQuoteValue(referer),
		accessLogQuoteValue(userAgent),
		elapsed.Seconds(),
		float64(firstWrite.Microseconds())/1000,
		accessLogQuoteValue(writeErr),
	)
	if details == nil || details.media == nil {
		return line
	}
	media := details.media
	rangeHeader := media.Range
	if rangeHeader == "" {
		rangeHeader = "-"
	}
	return fmt.Sprintf(`%s media_id=%d media_type=%s play_session_id=%s work_id=%s client_entry=%s range="%s" range_start=%s range_end=%s file_size=%d open_ms=%.3f stat_ms=%.3f`,
		line,
		media.ID,
		accessLogToken(media.Type),
		accessLogToken(defaultAccessLogToken(media.PlaySessionID)),
		accessLogToken(defaultAccessLogToken(media.WorkID)),
		accessLogToken(defaultAccessLogToken(media.ClientEntry)),
		accessLogQuoteValue(rangeHeader),
		accessLogToken(defaultAccessLogToken(media.RangeStart)),
		accessLogToken(defaultAccessLogToken(media.RangeEnd)),
		media.FileSize,
		float64(media.OpenTime.Microseconds())/1000,
		float64(media.StatTime.Microseconds())/1000,
	)
}

func accessLogRangeBounds(rangeHeader string) (string, string) {
	rangeHeader = strings.TrimSpace(rangeHeader)
	if !strings.HasPrefix(rangeHeader, "bytes=") {
		return "-", "-"
	}
	spec := strings.TrimPrefix(rangeHeader, "bytes=")
	if comma := strings.Index(spec, ","); comma >= 0 {
		spec = spec[:comma]
	}
	start, end, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok {
		return "-", "-"
	}
	start = strings.TrimSpace(start)
	end = strings.TrimSpace(end)
	if start == "" {
		start = "-"
	}
	if end == "" {
		end = "-"
	}
	return start, end
}

func accessLogClientEntry(r *http.Request) string {
	referer := r.Header.Get("Referer")
	if strings.Contains(referer, "office.app.5ddd.com") {
		return "fnos_app_entry"
	}
	remoteAddr := clientIP(r.RemoteAddr)
	if remoteAddr == "127.0.0.1" || remoteAddr == "::1" || remoteAddr == "localhost" {
		return "local_loopback"
	}
	if isPrivateLANIP(remoteAddr) {
		return "direct_lan"
	}
	if referer != "" && r.Host != "" {
		if u, err := url.Parse(referer); err == nil && strings.EqualFold(u.Host, r.Host) {
			return "direct_host"
		}
	}
	return "external_or_unknown"
}

func isPrivateLANIP(value string) bool {
	ip := net.ParseIP(value)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() {
		return true
	}
	return false
}

func defaultAccessLogToken(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func clientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	if strings.TrimSpace(remoteAddr) == "" {
		return "-"
	}
	return remoteAddr
}

func sanitizedRequestURI(u *url.URL) string {
	if u == nil {
		return "/"
	}
	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	query := sanitizeRawQuery(u)
	if query == "" {
		return path
	}
	return path + "?" + query
}

func sanitizeRawQuery(u *url.URL) string {
	values := u.Query()
	if len(values) == 0 {
		return ""
	}
	for key := range values {
		if sensitiveQueryKey(key) {
			values[key] = []string{"REDACTED"}
		}
	}
	return values.Encode()
}

func sensitiveQueryKey(key string) bool {
	lower := strings.ToLower(key)
	sensitive := []string{"token", "password", "cookie", "authorization", "session", "csrf"}
	for _, marker := range sensitive {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func accessLogToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "-"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		default:
			return r
		}
	}, value)
}

func accessLogQuoteValue(value string) string {
	value = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', '\t':
			return ' '
		default:
			return r
		}
	}, value)
	return strings.ReplaceAll(value, `"`, `\"`)
}
