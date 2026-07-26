package server

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const learningGeneratorVersion = "wisemapping-tango-layout-v5"

const (
	// WiseMapping validates mindmaps with fewer than 4,000 topics. Keep a
	// buffer for future metadata topics and preserve every work as a visible
	// branch when a creator aggregate falls back to saved work outlines.
	maxCreatorMapNodes        = 3600
	workMindMapInstruction    = `将XX总结成思维导图，要求简单、概要、明了，同时覆盖核心观点、方法和结论。`
	creatorMindMapInstruction = `将XX总结成一张UP主学习思维导图。以下是同一UP主的全部作品字幕，每段都以作品编号、日期和标题开始。
按主题或方法论组织一级章节，不要按作品逐条罗列；跨作品的共同观点要合并，重要的不同观点或演变要保留。章节应覆盖核心主题、方法步骤、关键判断、案例或行动要点（仅在原文存在时）。`
)

type MindNode struct {
	Title    string     `json:"title"`
	Children []MindNode `json:"children,omitempty"`
	// Category and Tags only apply to the root node. They are deliberately kept
	// with the generated outline so an existing map can be re-synced without a
	// second Codex request.
	Category string   `json:"category,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}
type LearningManager struct {
	store  *Store
	hub    *EventHub
	mu     sync.Mutex
	cfg    LearningRuntimeConfig
	status LearningStatus
	stop   chan struct{}
	cancel context.CancelFunc
	login  codexLoginSession
}

type codexLoginSession struct {
	status CodexLoginStatus
	cancel context.CancelFunc
}

func NewLearningManager(store *Store, hub *EventHub, cfg LearningRuntimeConfig) *LearningManager {
	m := &LearningManager{store: store, hub: hub, cfg: normalizeLearningRuntimeConfig(cfg), stop: make(chan struct{}), status: LearningStatus{Errors: []string{}}}
	_, err := store.RecoverInterruptedLearningJobs()
	if err != nil {
		m.status.Errors = append(m.status.Errors, "恢复中断学习任务失败: "+err.Error())
	}
	// Start once even if an older package already converted an interrupted job
	// to pending. run exits immediately when the persisted queue is empty.
	m.status.Running = true
	go m.run()
	go m.schedule()
	return m
}
func (m *LearningManager) Close() {
	close(m.stop)
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	if m.login.cancel != nil {
		m.login.cancel()
	}
	m.mu.Unlock()
}

func (m *LearningManager) CodexLoginStatus(ctx context.Context) CodexLoginStatus {
	m.mu.Lock()
	status := m.login.status
	m.mu.Unlock()
	if status.State == "pending" {
		return status
	}
	loggedIn, message := codexLoginState(ctx, m.Config())
	if loggedIn {
		return CodexLoginStatus{State: "logged_in", Message: message}
	}
	return CodexLoginStatus{State: "logged_out", Message: message}
}

func (m *LearningManager) StartCodexLogin() (CodexLoginStatus, error) {
	m.mu.Lock()
	if m.login.status.State == "pending" {
		status := m.login.status
		m.mu.Unlock()
		return status, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.login = codexLoginSession{status: CodexLoginStatus{State: "pending", Message: "正在请求设备授权"}, cancel: cancel}
	m.mu.Unlock()
	go m.runCodexDeviceLogin(ctx)
	return m.CodexLoginStatus(context.Background()), nil
}

func (m *LearningManager) CancelCodexLogin() CodexLoginStatus {
	m.mu.Lock()
	if m.login.cancel != nil {
		m.login.cancel()
	}
	m.login = codexLoginSession{status: CodexLoginStatus{State: "cancelled", Message: "已取消设备授权"}}
	status := m.login.status
	m.mu.Unlock()
	return status
}

func (m *LearningManager) LogoutCodex(ctx context.Context) error {
	path := resolveCodexPath(m.Config().CodexPath)
	cmd := exec.CommandContext(ctx, path, "logout")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+learningCodexHome())
	if output, err := cmd.CombinedOutput(); err != nil {
		return sanitizedCodexError(err, output)
	}
	m.mu.Lock()
	m.login = codexLoginSession{status: CodexLoginStatus{State: "logged_out", Message: "已退出 Codex"}}
	m.mu.Unlock()
	return nil
}

func (m *LearningManager) runCodexDeviceLogin(ctx context.Context) {
	path := resolveCodexPath(m.Config().CodexPath)
	cmd := exec.CommandContext(ctx, path, "login", "--device-auth")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+learningCodexHome())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return
	}
	if err = cmd.Start(); err != nil {
		m.setCodexLoginFailed(err.Error())
		return
	}
	var output strings.Builder
	var outputMu sync.Mutex
	var readers sync.WaitGroup
	consume := func(scanner *bufio.Scanner) {
		defer readers.Done()
		for scanner.Scan() {
			line := scanner.Text()
			outputMu.Lock()
			output.WriteString(line + "\n")
			outputMu.Unlock()
			url, code := parseCodexDeviceOutput(line)
			if code != "" || strings.Contains(line, "codex/device") {
				m.mu.Lock()
				if m.login.status.State == "pending" {
					m.login.status.VerificationURL = url
					m.login.status.UserCode = code
					m.login.status.Message = "请在浏览器完成设备授权"
					m.login.status.ExpiresAt = time.Now().Add(15 * time.Minute).UTC().Format(time.RFC3339)
				}
				m.mu.Unlock()
			}
		}
	}
	readers.Add(2)
	go consume(bufio.NewScanner(stdout))
	go consume(bufio.NewScanner(stderr))
	readers.Wait()
	err = cmd.Wait()
	message := strings.TrimSpace(output.String())
	m.mu.Lock()
	defer m.mu.Unlock()
	if ctx.Err() != nil {
		return
	}
	if err != nil {
		m.login = codexLoginSession{status: CodexLoginStatus{State: "failed", Message: sanitizedCodexMessage(message)}}
		return
	}
	m.login = codexLoginSession{status: CodexLoginStatus{State: "logged_in", Message: "Codex 已登录"}}
}

func (m *LearningManager) setCodexLoginFailed(message string) {
	m.mu.Lock()
	m.login = codexLoginSession{status: CodexLoginStatus{State: "failed", Message: sanitizedCodexMessage(message)}}
	m.mu.Unlock()
}

func parseCodexDeviceOutput(output string) (string, string) {
	var verificationURL, userCode string
	for _, token := range strings.Fields(output) {
		if strings.HasPrefix(token, "http://") || strings.HasPrefix(token, "https://") {
			verificationURL = strings.TrimRight(token, ".,)")
		}
	}
	for _, token := range strings.Fields(output) {
		cleaned := strings.Trim(token, " .,:;()[]")
		if len(cleaned) >= 6 && strings.Contains(cleaned, "-") {
			userCode = cleaned
		}
	}
	if verificationURL == "" {
		verificationURL = "https://auth.openai.com/codex/device"
	}
	return verificationURL, userCode
}

func codexLoginState(ctx context.Context, cfg LearningRuntimeConfig) (bool, string) {
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(checkCtx, resolveCodexPath(cfg.CodexPath), "login", "status")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+learningCodexHome())
	output, err := cmd.CombinedOutput()
	message := sanitizedCodexMessage(string(output))
	if err != nil {
		return false, message
	}
	lower := strings.ToLower(string(output))
	return strings.Contains(lower, "logged in") || strings.Contains(lower, "chatgpt"), message
}

func sanitizedCodexError(err error, output []byte) error {
	return errors.New(sanitizedCodexMessage(strings.TrimSpace(string(output))) + ": " + err.Error())
}
func sanitizedCodexMessage(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "Codex 命令失败"
	}
	lower := strings.ToLower(value)
	if strings.Contains(lower, "stack") || strings.Contains(lower, "file://") || strings.Contains(lower, "missing optional dependency") {
		return "Codex CLI 无法启动，请检查内置运行时或路径"
	}
	for _, marker := range []string{"access_token", "refresh_token", "Bearer "} {
		if i := strings.Index(strings.ToLower(value), strings.ToLower(marker)); i >= 0 {
			return strings.TrimSpace(value[:i]) + "（敏感输出已隐藏）"
		}
	}
	if line, _, ok := strings.Cut(value, "\n"); ok {
		value = strings.TrimSpace(line)
	}
	if len(value) > 240 {
		return value[:240]
	}
	return value
}
func (m *LearningManager) SetConfig(c LearningRuntimeConfig) {
	m.mu.Lock()
	m.cfg = normalizeLearningRuntimeConfig(c)
	m.mu.Unlock()
}
func (m *LearningManager) Config() LearningRuntimeConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg
}
func resolveCodexPath(path string) string {
	if strings.TrimSpace(path) != "" {
		return strings.TrimSpace(path)
	}
	if env := strings.TrimSpace(os.Getenv("LOCALBILIBILI_CODEX_PATH")); env != "" {
		return env
	}
	if exe, e := os.Executable(); e == nil {
		candidate := filepath.Join(filepath.Dir(exe), "codex")
		if fileExecutable(candidate) {
			return candidate
		}
	}
	return "codex"
}
func learningPasswordConfigured() bool {
	p := learningSecretPath()
	info, e := os.Stat(p)
	return e == nil && !info.IsDir() && info.Size() > 0
}

func learningSecretPath() string {
	if p := strings.TrimSpace(os.Getenv("LOCALBILIBILI_WISEMAPPING_SECRET_FILE")); p != "" {
		return p
	}
	base := strings.TrimSpace(os.Getenv("LOCALBILIBILI_LEARNING_SECRET_DIR"))
	if base == "" {
		base = filepath.Join(os.TempDir(), "localbilibili-learning")
	}
	return filepath.Join(base, "wisemapping.secret")
}

func saveLearningPassword(password string) error {
	path := learningSecretPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".wisemapping-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(strings.TrimSpace(password) + "\n"); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}
func (m *LearningManager) Check(ctx context.Context) LearningCheck {
	cfg := m.Config()
	out := LearningCheck{CodexPath: resolveCodexPath(cfg.CodexPath), PasswordConfigured: learningPasswordConfigured()}
	if !fileExecutable(out.CodexPath) && out.CodexPath != "codex" {
		out.Message = "Codex CLI 不可执行"
		return out
	}
	cctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	b, e := exec.CommandContext(cctx, out.CodexPath, "--version").CombinedOutput()
	out.CodexVersion = strings.TrimSpace(string(b))
	out.CodexLoggedIn, _ = codexLoginState(ctx, cfg)
	out.OK = e == nil && out.CodexLoggedIn && strings.TrimSpace(cfg.WiseMappingURL) != "" && strings.TrimSpace(cfg.WiseMappingUser) != "" && out.PasswordConfigured
	if !out.OK {
		out.Message = "需配置可用 Codex、WiseMapping 地址/账号及私有密码文件"
	}
	return out
}
func (m *LearningManager) Status() LearningStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	jobs, _ := m.store.LearningJobs(30)
	m.status.Jobs = jobs
	pending, done, failed := 0, 0, 0
	for _, j := range jobs {
		switch j.Status {
		case "pending":
			pending++
		case "succeeded":
			done++
		case "failed":
			failed++
		}
	}
	m.status.Pending = pending
	m.status.Succeeded = done
	m.status.Failed = failed
	t := pending + done + failed
	if t > 0 {
		m.status.Progress = (done + failed) * 100 / t
	}
	return m.status
}
func (m *LearningManager) Enqueue(creatorID, workID, queueID int64, source string) (LearningStatus, error) {
	return m.EnqueueRun(creatorID, workID, queueID, 0, source)
}
func (m *LearningManager) EnqueueRun(creatorID, workID, queueID, runID int64, source string) (LearningStatus, error) {
	if creatorID <= 0 {
		return LearningStatus{}, errors.New("creator_id is required")
	}
	if _, e := m.store.AddLearningJobWithRun(creatorID, workID, queueID, runID, source); e != nil {
		return LearningStatus{}, e
	}
	m.mu.Lock()
	if !m.status.Running {
		m.status.Running = true
		go m.run()
	}
	m.mu.Unlock()
	return m.Status(), nil
}
func (m *LearningManager) Reset() error {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
	}
	m.status = LearningStatus{Errors: []string{}}
	m.mu.Unlock()
	return m.store.ClearLearningJobs()
}
func (m *LearningManager) run() {
	for {
		select {
		case <-m.stop:
			return
		default:
		}
		job, e := m.store.NextPendingLearningJob()
		if errors.Is(e, sql.ErrNoRows) {
			m.mu.Lock()
			m.status.Running = false
			m.status.CurrentCreator = ""
			m.cancel = nil
			m.mu.Unlock()
			return
		}
		if e != nil {
			time.Sleep(time.Second)
			continue
		}
		_ = m.store.MarkLearningRunning(job.ID)
		timeout := 8 * time.Minute
		if job.WorkID == 0 {
			// A full creator request intentionally sends every subtitle in one call.
			// Give that single model invocation a larger bounded window instead of
			// silently splitting or truncating its input.
			timeout = 45 * time.Minute
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		m.mu.Lock()
		m.cancel = cancel
		m.status.CurrentCreator = job.CreatorName
		m.mu.Unlock()
		e = m.process(ctx, job)
		cancel()
		state, msg := "succeeded", "完成"
		if e != nil {
			state, msg = "failed", e.Error()
			m.mu.Lock()
			m.status.Errors = append(m.status.Errors, msg)
			m.mu.Unlock()
		}
		_ = m.store.FinishLearningJob(job.ID, state, msg, 1, 1)
		m.publish()
	}
}
func (m *LearningManager) process(ctx context.Context, job LearningJob) error {
	if job.WorkID > 0 {
		return m.createWorkMap(ctx, job.CreatorID, job.WorkID)
	}
	return m.createCreatorMap(ctx, job.CreatorID)
}
func (m *LearningManager) createWorkMap(ctx context.Context, creatorID, workID int64) error {
	text, e := m.store.LearningSubtitle(workID)
	if e != nil {
		return e
	}
	w, e := m.store.Work(workID)
	if e != nil {
		return e
	}
	title := learningWorkMapTitle(w.CreatorName, w.Title)
	prompt := m.workPrompt()
	fingerprint := learningContentFingerprint(text, prompt)
	if old, err := m.store.MindMap(creatorID, workID); err == nil && old.MapID != "" && old.ContentFingerprint == fingerprint && old.SummaryJSON != "" {
		var cached MindNode
		if json.Unmarshal([]byte(old.SummaryJSON), &cached) == nil && validateMindNode(cached) == nil {
			return m.sync(ctx, creatorID, workID, w.CreatorName, title, cached, text)
		}
	}
	node, e := m.codex(ctx, fmt.Sprintf("作品：%s\n\n%s", w.Title, text), prompt)
	if e != nil {
		return e
	}
	return m.sync(ctx, creatorID, workID, w.CreatorName, title, node, text)
}
func (m *LearningManager) createCreatorMap(ctx context.Context, creatorID int64) error {
	c, e := m.store.Creator(creatorID)
	if e != nil {
		return e
	}
	input, _, e := m.store.LearningCreatorSubtitles(creatorID)
	if e != nil {
		return e
	}
	title := fmt.Sprintf("%s - 学习思维导图", c.Name)
	prompt := m.creatorPrompt()
	fingerprint := learningContentFingerprint(input, prompt)
	if old, err := m.store.MindMap(creatorID, 0); err == nil && old.MapID != "" && old.ContentFingerprint == fingerprint && old.SummaryJSON != "" {
		var cached MindNode
		if json.Unmarshal([]byte(old.SummaryJSON), &cached) == nil && validateMindNode(cached) == nil && creatorMapWithinNodeBudget(cached) {
			return m.sync(ctx, creatorID, 0, c.Name, title, cached, input)
		}
	}
	node, e := m.codex(ctx, input, prompt)
	if e != nil {
		// A full creator corpus can exceed the Codex context limit even though
		// every individual work was generated successfully. Compose every saved
		// work outline instead of silently falling back to a tag-filtered list.
		records, recordsErr := m.store.WorkMindMaps(creatorID)
		if recordsErr != nil {
			return e
		}
		node, recordsErr = creatorMapFallbackNode(c.Name, records)
		if recordsErr != nil {
			return e
		}
	}
	return m.sync(ctx, creatorID, 0, c.Name, title, node, input)
}

func creatorMapFallbackNode(creatorName string, records []MindMapRecord) (MindNode, error) {
	root := MindNode{Title: strings.TrimSpace(creatorName) + " - 学习思维导图", Category: "学习总结"}
	// Every saved work must remain discoverable in the aggregate. Reserve one
	// wrapper node per work, then give its saved outline an equal deterministic
	// share of the remaining WiseMapping-safe node budget.
	outlineBudget := 1
	if len(records) > 0 {
		outlineBudget = (maxCreatorMapNodes-1)/len(records) - 1
		if outlineBudget < 1 {
			outlineBudget = 1
		}
	}
	for _, record := range records {
		var node MindNode
		if err := json.Unmarshal([]byte(record.SummaryJSON), &node); err != nil || validateMindNode(node) != nil {
			continue
		}
		workTitle := strings.TrimSpace(record.Title)
		if prefix := strings.TrimSpace(creatorName) + " - "; strings.HasPrefix(workTitle, prefix) {
			workTitle = strings.TrimSpace(strings.TrimPrefix(workTitle, prefix))
		}
		if workTitle == "" {
			workTitle = node.Title
		}
		root.Children = append(root.Children, MindNode{Title: workTitle, Children: []MindNode{trimMindNode(node, outlineBudget)}})
	}
	if len(root.Children) == 0 {
		return MindNode{}, errors.New("缺少可聚合的单作品导图")
	}
	return root, nil
}

func trimMindNode(node MindNode, budget int) MindNode {
	if budget <= 1 {
		return MindNode{Title: node.Title}
	}
	trimmed := MindNode{Title: node.Title}
	remaining := budget - 1
	for i, child := range node.Children {
		if remaining == 0 {
			break
		}
		// Reserve one node for every later sibling so a broad outline does not
		// hide whole top-level topics just because an earlier branch is deep.
		later := len(node.Children) - i - 1
		childBudget := remaining - later
		if childBudget < 1 {
			childBudget = 1
		}
		child = trimMindNode(child, childBudget)
		trimmed.Children = append(trimmed.Children, child)
		remaining -= mindNodeCount(child)
	}
	return trimmed
}

func mindNodeCount(node MindNode) int {
	count := 1
	for _, child := range node.Children {
		count += mindNodeCount(child)
	}
	return count
}

func creatorMapWithinNodeBudget(node MindNode) bool {
	return mindNodeCount(node) <= maxCreatorMapNodes
}

func (m *LearningManager) codex(ctx context.Context, input, instruction string) (MindNode, error) {
	cfg := m.Config()
	path := resolveCodexPath(cfg.CodexPath)
	prompt := strings.ReplaceAll(instruction, "XX", input)
	if prompt == instruction {
		prompt += "\n\n输入内容：\n" + input
	}
	prompt += "\n\n只返回递归 JSON 对象，格式严格如下：{\"title\":\"中心主题\",\"category\":\"内容分类\",\"tags\":[\"主题词\"],\"children\":[{\"title\":\"一级主题\",\"children\":[{\"title\":\"二级主题\",\"children\":[]}]}]}。每一个根节点和子节点都必须是对象且必须有非空 title，不得使用 text、name 或纯字符串代替 title。节点文字简洁、合并重复观点、最多四层、忠于字幕、不补充字幕中不存在的事实。category 是一个简洁内容分类（例如宏观经济、股市投资、商业分析、科技趋势、个人理财）；tags 是 0 到 3 个有助检索的主题词，只能来自字幕，不要包含UP主名、站点名、内容分类或泛泛词。"
	// Codex documents `exec -` as the stdin form of PROMPT. Keeping the full
	// creator corpus out of argv avoids the operating system argument-size limit
	// without splitting or truncating the requested one-shot summary.
	cmd := exec.CommandContext(ctx, path, "exec", "--skip-git-repo-check", "-")
	cmd.Env = append(os.Environ(), "CODEX_HOME="+learningCodexHome())
	cmd.Stdin = strings.NewReader(prompt)
	b, e := cmd.Output()
	if e != nil {
		var exitErr *exec.ExitError
		if errors.As(e, &exitErr) {
			message := sanitizedCodexMessage(string(exitErr.Stderr))
			if message != "Codex 命令失败" {
				return MindNode{}, fmt.Errorf("Codex 执行失败: %s: %w", message, e)
			}
		}
		return MindNode{}, fmt.Errorf("Codex 执行失败: %w", e)
	}
	return parseCodexMindNode(b)
}

// parseCodexMindNode accepts the final JSON object even when the CLI wraps it
// in a Markdown fence or a short explanatory sentence. It deliberately keeps
// the original model output out of errors because it can include subtitles.
func parseCodexMindNode(output []byte) (MindNode, error) {
	trimmed := bytes.TrimSpace(output)
	var node MindNode
	if json.Unmarshal(trimmed, &node) != nil {
		object, ok := firstJSONObject(trimmed)
		if !ok || json.Unmarshal(object, &node) != nil {
			return MindNode{}, errors.New("Codex 未返回合法导图 JSON")
		}
	}
	if err := validateMindNode(node); err != nil {
		return MindNode{}, err
	}
	return node, nil
}

func validateMindNode(root MindNode) error {
	var walk func(MindNode, int) error
	walk = func(node MindNode, depth int) error {
		if strings.TrimSpace(node.Title) == "" {
			if depth == 0 {
				return errors.New("Codex 导图缺少标题")
			}
			return errors.New("Codex 导图包含无标题节点")
		}
		for _, child := range node.Children {
			if err := walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root, 0)
}

func firstJSONObject(value []byte) ([]byte, bool) {
	start := bytes.IndexByte(value, '{')
	if start < 0 {
		return nil, false
	}
	depth := 0
	inString, escaped := false, false
	for i := start; i < len(value); i++ {
		ch := value[i]
		if inString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
			} else if ch == '"' {
				inString = false
			}
			continue
		}
		switch ch {
		case '"':
			inString = true
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				if bytes.IndexByte(value[i+1:], '{') >= 0 {
					return nil, false
				}
				return value[start : i+1], true
			}
		}
	}
	return nil, false
}
func learningCodexHome() string {
	if p := strings.TrimSpace(os.Getenv("LOCALBILIBILI_CODEX_HOME")); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "localbilibili-codex")
}
func (m *LearningManager) sync(ctx context.Context, creatorID, workID int64, creatorName, title string, node MindNode, source string) error {
	xmlDoc, e := wiseMappingXML(node)
	if e != nil {
		return e
	}
	fp := learningContentFingerprint(source, m.promptForMap(workID))
	old, _ := m.store.MindMap(creatorID, workID)
	mapID := old.MapID
	url := old.MapURL
	cfg := m.Config()
	if strings.TrimSpace(cfg.WiseMappingURL) == "" {
		return errors.New("WiseMapping 地址未配置")
	}
	password, err := learningWiseMappingPassword()
	if err != nil {
		return err
	}
	token, err := wiseMappingToken(ctx, cfg, password)
	if err != nil {
		return err
	}
	persistPending := func() error {
		url = wiseMappingEditorURL(cfg.WiseMappingURL, mapID)
		pending := MindMapRecord{CreatorID: creatorID, WorkID: workID, MapID: mapID, MapURL: url, Title: title, ContentFingerprint: fp, SyncStatus: "pending"}
		return m.store.UpsertMindMap(pending, string(mustJSON(node)))
	}
	createMap := func() error {
		mapID, err = wiseMappingCreate(ctx, cfg, token, title)
		if err != nil {
			mapID, err = wiseMappingFindMapID(ctx, cfg, token, wiseMappingTitle(title))
			if err != nil {
				return err
			}
		}
		// A successfully created remote map must survive a later document-save
		// failure, so retries update the same map instead of creating duplicates.
		return persistPending()
	}
	if old.MapID != "" && old.Title != title {
		if err = wiseMappingUpdateTitle(ctx, cfg, token, mapID, title); err != nil {
			if wiseMappingNotFound(err) {
				mapID = ""
			} else {
				// Title compatibility must not make a valid document update fail.
				// Keep the recorded title so a later sync can retry the rename.
				title = old.Title
			}
		}
	}
	if mapID == "" {
		if err = createMap(); err != nil {
			return err
		}
	}
	if err = wiseMappingUpdate(ctx, cfg, token, mapID, xmlDoc); err != nil {
		remoteMissing := wiseMappingNotFound(err)
		if !remoteMissing && wiseMappingForbidden(err) {
			remoteMissing, err = wiseMappingMapMissing(ctx, cfg, token, mapID)
		}
		if remoteMissing {
			mapID = ""
			if err = createMap(); err == nil {
				err = wiseMappingUpdate(ctx, cfg, token, mapID, xmlDoc)
			}
		}
		if err != nil {
			return err
		}
	}
	if err := wiseMappingSyncFolder(ctx, cfg, token, mapID, "LocalDouyin", creatorName); err != nil {
		if !errors.Is(err, errWiseMappingFoldersUnsupported) {
			return err
		}
	}
	if err := wiseMappingSyncLabels(ctx, cfg, token, mapID, learningMapLabels(node)); err != nil {
		return err
	}
	v := MindMapRecord{CreatorID: creatorID, WorkID: workID, MapID: mapID, MapURL: url, Title: title, ContentFingerprint: fp, SyncStatus: "synced"}
	return m.store.UpsertMindMap(v, string(mustJSON(node)))
}

func (m *LearningManager) workPrompt() string {
	return normalizeLearningRuntimeConfig(m.Config()).WorkPrompt
}

func learningWorkMapTitle(creatorName, workTitle string) string {
	creatorName = strings.TrimSpace(creatorName)
	workTitle = strings.TrimSpace(strings.Split(strings.ReplaceAll(workTitle, "\r\n", "\n"), "\n")[0])
	if workTitle == "" {
		return creatorName + " - 作品导图"
	}
	const maxTitleRunes = 72
	runes := []rune(workTitle)
	if len(runes) > maxTitleRunes {
		workTitle = string(runes[:maxTitleRunes-1]) + "…"
	}
	if creatorName == "" {
		return workTitle
	}
	return creatorName + " - " + workTitle
}

func (m *LearningManager) creatorPrompt() string {
	return normalizeLearningRuntimeConfig(m.Config()).CreatorPrompt
}

func (m *LearningManager) promptForMap(workID int64) string {
	if workID > 0 {
		return m.workPrompt()
	}
	return m.creatorPrompt()
}

func learningContentFingerprint(source string, prompt ...string) string {
	activePrompt := ""
	if len(prompt) > 0 {
		activePrompt = prompt[0]
	}
	versioned := learningGeneratorVersion + "\n" + activePrompt + "\n" + source
	return fmt.Sprintf("%x", sha256.Sum256([]byte(versioned)))
}

func wiseMappingNotFound(err error) bool {
	return err != nil && strings.Contains(err.Error(), "404")
}

func wiseMappingForbidden(err error) bool {
	return err != nil && strings.Contains(err.Error(), "403")
}

func learningWiseMappingPassword() (string, error) {
	p := learningSecretPath()
	// Older packages could leave a pre-existing secret with its old mode when
	// the administrator saved unrelated configuration. Correct it on normal
	// use as well as on writes, without ever exposing the secret itself.
	if err := os.Chmod(p, 0o600); err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	password := strings.TrimSpace(string(b))
	if password == "" {
		return "", errors.New("WiseMapping 私有密码文件为空")
	}
	return password, nil
}
func wiseMappingToken(ctx context.Context, cfg LearningRuntimeConfig, password string) (string, error) {
	body, _ := json.Marshal(map[string]string{"email": cfg.WiseMappingUser, "password": password})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/authenticate", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("WiseMapping 登录失败: %s", resp.Status)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(responseBody))
	var out struct {
		Token string `json:"token"`
		JWT   string `json:"jwt"`
	}
	if json.Unmarshal(responseBody, &out) == nil {
		if out.Token != "" {
			return out.Token, nil
		}
		if out.JWT != "" {
			return out.JWT, nil
		}
	}
	var jsonToken string
	if json.Unmarshal(responseBody, &jsonToken) == nil {
		value = strings.TrimSpace(jsonToken)
	}
	if value == "" {
		return "", errors.New("WiseMapping 未返回认证令牌")
	}
	return value, nil
}
func wiseMappingCreate(ctx context.Context, cfg LearningRuntimeConfig, token, title string) (string, error) {
	values := url.Values{"title": {wiseMappingTitle(title)}, "description": {""}, "theme": {"prism"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps?"+values.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 240))
		message := strings.Join(strings.Fields(string(body)), " ")
		if message != "" {
			return "", fmt.Errorf("WiseMapping 创建失败: %s: %s", resp.Status, message)
		}
		return "", fmt.Errorf("WiseMapping 创建失败: %s", resp.Status)
	}
	mapID := strings.TrimSpace(resp.Header.Get("resourceid"))
	if mapID == "" {
		return "", errors.New("WiseMapping 未返回 map id")
	}
	return mapID, nil
}

func wiseMappingTitle(title string) string {
	title = strings.TrimSpace(title)
	const maxRunes = 240
	runes := []rune(title)
	if len(runes) <= maxRunes {
		return title
	}
	return string(runes[:maxRunes-1]) + "…"
}

func wiseMappingUpdateTitle(ctx context.Context, cfg LearningRuntimeConfig, token, mapID, title string) error {
	shortTitle := wiseMappingTitle(title)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/"+url.PathEscape(mapID)+"/title", strings.NewReader(shortTitle))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	// Some deployed WiseMapping builds reject the dedicated title route while
	// accepting their documented generic properties update. Use the latter as a
	// compatibility fallback; it leaves XML and collaboration properties unset.
	payload, _ := json.Marshal(map[string]string{"title": shortTitle})
	fallback, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/"+url.PathEscape(mapID)+"?minor=true", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	fallback.Header.Set("Content-Type", "application/json")
	fallback.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(fallback)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("WiseMapping 更新标题失败: %s（属性回退：%s）", resp.Status, response.Status)
	}
	return nil
}

func wiseMappingEditorURL(baseURL, mapID string) string {
	return strings.TrimRight(baseURL, "/") + "/c/maps/" + url.PathEscape(mapID) + "/edit"
}
func wiseMappingFindMapID(ctx context.Context, cfg LearningRuntimeConfig, token, title string) (string, error) {
	maps, err := wiseMappingMaps(ctx, cfg, token)
	if err != nil {
		return "", err
	}
	for _, item := range maps {
		if item.ID > 0 && item.Title == title {
			return fmt.Sprintf("%d", item.ID), nil
		}
	}
	return "", errors.New("WiseMapping 未找到可复用的同名导图")
}

type wiseMappingMapInfo struct {
	ID     int64                  `json:"id"`
	Title  string                 `json:"title"`
	Labels []wiseMappingLabelInfo `json:"labels"`
}

type wiseMappingLabelInfo struct {
	ID    int64  `json:"id"`
	Title string `json:"title"`
	Color string `json:"color"`
}

type wiseMappingFolderInfo struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	ParentID *int64 `json:"parentId"`
}

var errWiseMappingFoldersUnsupported = errors.New("WiseMapping 目录 API 不可用")

// wiseMappingSyncFolder keeps every generated map in the real WiseMapping
// folder hierarchy, rather than relying on labels as a directory substitute.
// The installed custom WiseMapping API owns this hierarchy per account.
func wiseMappingSyncFolder(ctx context.Context, cfg LearningRuntimeConfig, token, mapID, rootName, creatorName string) error {
	rootName = strings.TrimSpace(rootName)
	creatorName = strings.TrimSpace(creatorName)
	if rootName == "" || creatorName == "" {
		return errors.New("WiseMapping 目录名称不能为空")
	}
	folders, err := wiseMappingFolders(ctx, cfg, token)
	if err != nil {
		return err
	}
	mapNumericID, err := strconv.ParseInt(strings.TrimSpace(mapID), 10, 64)
	if err != nil || mapNumericID <= 0 {
		return fmt.Errorf("WiseMapping 导图 id 无效，无法归档: %q", mapID)
	}
	rootID, err := wiseMappingEnsureFolder(ctx, cfg, token, folders, rootName, nil)
	if err != nil {
		return err
	}
	folders, err = wiseMappingFolders(ctx, cfg, token)
	if err != nil {
		return err
	}
	creatorID, err := wiseMappingEnsureFolder(ctx, cfg, token, folders, creatorName, &rootID)
	if err != nil {
		return err
	}
	return wiseMappingMoveMapsToFolder(ctx, cfg, token, []int64{mapNumericID}, creatorID)
}

func wiseMappingFolders(ctx context.Context, cfg LearningRuntimeConfig, token string) ([]wiseMappingFolderInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/folders", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, errWiseMappingFoldersUnsupported
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("WiseMapping 查询目录失败: %s", resp.Status)
	}
	var folders []wiseMappingFolderInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&folders); err != nil {
		return nil, err
	}
	return folders, nil
}

func wiseMappingEnsureFolder(ctx context.Context, cfg LearningRuntimeConfig, token string, folders []wiseMappingFolderInfo, name string, parentID *int64) (int64, error) {
	if id, ok := wiseMappingFolderID(folders, name, parentID); ok {
		return id, nil
	}
	payload, _ := json.Marshal(map[string]any{"name": name, "parentId": parentID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/folders", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		var created wiseMappingFolderInfo
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&created); err == nil && created.ID > 0 {
			return created.ID, nil
		}
	}
	// A parallel queue worker can create the same account-private folder after
	// our GET. Re-read once before treating the create response as a failure.
	reloaded, reloadErr := wiseMappingFolders(ctx, cfg, token)
	if reloadErr == nil {
		if id, ok := wiseMappingFolderID(reloaded, name, parentID); ok {
			return id, nil
		}
	}
	if resp.StatusCode == http.StatusNotFound {
		return 0, errWiseMappingFoldersUnsupported
	}
	return 0, fmt.Errorf("WiseMapping 创建目录失败: %s", resp.Status)
}

func wiseMappingFolderID(folders []wiseMappingFolderInfo, name string, parentID *int64) (int64, bool) {
	var selected int64
	for _, folder := range folders {
		if folder.Name != name || !sameOptionalInt64(folder.ParentID, parentID) || folder.ID <= 0 {
			continue
		}
		if selected == 0 || folder.ID < selected {
			selected = folder.ID
		}
	}
	return selected, selected > 0
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func wiseMappingMoveMapsToFolder(ctx context.Context, cfg LearningRuntimeConfig, token string, mapIDs []int64, folderID int64) error {
	payload, _ := json.Marshal(map[string]any{"mapIds": mapIDs, "folderId": folderID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/folder", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errWiseMappingFoldersUnsupported
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WiseMapping 移动导图到目录失败: %s", resp.Status)
	}
	return nil
}

func wiseMappingMapMissing(ctx context.Context, cfg LearningRuntimeConfig, token, mapID string) (bool, error) {
	maps, err := wiseMappingMaps(ctx, cfg, token)
	if err != nil {
		return false, err
	}
	for _, item := range maps {
		if strconv.FormatInt(item.ID, 10) == mapID {
			return false, nil
		}
	}
	return true, nil
}

func wiseMappingMaps(ctx context.Context, cfg LearningRuntimeConfig, token string) ([]wiseMappingMapInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("WiseMapping 查询已有导图失败: %s", resp.Status)
	}
	var body struct {
		MindmapsInfo []wiseMappingMapInfo `json:"mindmapsInfo"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, err
	}
	return body.MindmapsInfo, nil
}

// wiseMappingSyncLabels follows the protocol served by the installed
// WiseMapping 6.x frontend: GET /labels/, POST /labels, then POST a JSON label
// id to /maps/{id}/labels. Keeping this here makes labels part of an atomic
// logical map sync: a task is not marked synced until its labels are visible.
const maxLearningMapLabels = 3

func wiseMappingSyncLabels(ctx context.Context, cfg LearningRuntimeConfig, token, mapID string, titles []string) error {
	labels, err := wiseMappingLabels(ctx, cfg, token)
	if err != nil {
		return err
	}
	byTitle := make(map[string]int64, len(labels))
	for _, label := range labels {
		byTitle[label.Title] = label.ID
	}
	for _, title := range titles {
		if _, ok := byTitle[title]; ok {
			continue
		}
		id, err := wiseMappingCreateLabel(ctx, cfg, token, title)
		if err != nil {
			// Another task can create the same shared label between the list and
			// create calls. Re-read once so concurrent queue jobs remain safe.
			labels, reloadErr := wiseMappingLabels(ctx, cfg, token)
			if reloadErr != nil {
				return err
			}
			for _, label := range labels {
				byTitle[label.Title] = label.ID
			}
			if existing, found := byTitle[title]; found {
				id = existing
			} else {
				return err
			}
		}
		byTitle[title] = id
	}

	maps, err := wiseMappingMaps(ctx, cfg, token)
	if err != nil {
		return err
	}
	attached := map[int64]bool{}
	for _, item := range maps {
		if strconv.FormatInt(item.ID, 10) != mapID {
			continue
		}
		for _, label := range item.Labels {
			attached[label.ID] = true
		}
		break
	}
	desired := make(map[int64]bool, len(titles))
	for _, title := range titles {
		desired[byTitle[title]] = true
	}
	for id := range attached {
		if !desired[id] {
			if err := wiseMappingDeleteLabelFromMap(ctx, cfg, token, mapID, id); err != nil {
				return err
			}
		}
	}
	for _, title := range titles {
		id := byTitle[title]
		if attached[id] {
			continue
		}
		if err := wiseMappingAddLabelToMap(ctx, cfg, token, mapID, id); err != nil {
			return err
		}
	}
	return nil
}

func wiseMappingLabels(ctx context.Context, cfg LearningRuntimeConfig, token string) ([]wiseMappingLabelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/labels/", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("WiseMapping 查询标签失败: %s", resp.Status)
	}
	var body struct {
		Labels []wiseMappingLabelInfo `json:"labels"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&body); err != nil {
		return nil, err
	}
	return body.Labels, nil
}

func wiseMappingCreateLabel(ctx context.Context, cfg LearningRuntimeConfig, token, title string) (int64, error) {
	payload, _ := json.Marshal(map[string]string{"title": title, "color": "#4F8EF7", "iconName": "smile"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/labels", bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("WiseMapping 创建标签失败: %s", resp.Status)
	}
	id, err := strconv.ParseInt(strings.TrimSpace(resp.Header.Get("resourceid")), 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("WiseMapping 未返回标签 id")
	}
	return id, nil
}

func wiseMappingAddLabelToMap(ctx context.Context, cfg LearningRuntimeConfig, token, mapID string, labelID int64) error {
	payload, _ := json.Marshal(labelID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/"+url.PathEscape(mapID)+"/labels", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WiseMapping 关联标签失败: %s", resp.Status)
	}
	return nil
}

func wiseMappingDeleteLabelFromMap(ctx context.Context, cfg LearningRuntimeConfig, token, mapID string, labelID int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/"+url.PathEscape(mapID)+"/labels/"+strconv.FormatInt(labelID, 10), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("WiseMapping 移除标签失败: %s", resp.Status)
	}
	return nil
}

func learningMapLabels(node MindNode) []string {
	result := make([]string, 0, maxLearningMapLabels)
	seen := make(map[string]bool, maxLearningMapLabels)
	for _, tag := range node.Tags {
		tag = learningLabelText(tag)
		if tag == "" || seen[tag] || strings.HasPrefix(tag, "UP主名-") || strings.HasPrefix(tag, "站点名称-") || strings.HasPrefix(tag, "内容分类-") {
			continue
		}
		seen[tag] = true
		result = append(result, tag)
		if len(result) == maxLearningMapLabels {
			break
		}
	}
	return result
}

func learningLabelText(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	if len([]rune(value)) > 48 {
		value = string([]rune(value)[:48])
	}
	return value
}
func wiseMappingUpdate(ctx context.Context, cfg LearningRuntimeConfig, token, mapID string, doc []byte) error {
	payload, _ := json.Marshal(map[string]string{"id": mapID, "xml": string(doc), "properties": "{}"})
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, strings.TrimRight(cfg.WiseMappingURL, "/")+"/api/restful/maps/"+url.PathEscape(mapID)+"/document?minor=false", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 240))
		message := strings.Join(strings.Fields(string(body)), " ")
		if message != "" {
			return fmt.Errorf("WiseMapping 更新失败: %s: %s", resp.Status, message)
		}
		return fmt.Errorf("WiseMapping 更新失败: %s", resp.Status)
	}
	return nil
}
func mustJSON(v any) []byte { b, _ := json.Marshal(v); return b }
func wiseMappingXML(root MindNode) ([]byte, error) {
	nextID := 0
	var f func(MindNode, bool, int, int, int, int, int) string
	f = func(n MindNode, central bool, order, depth, side, x, y int) string {
		nextID++
		var b strings.Builder
		b.WriteString(`<topic id="`)
		b.WriteString(fmt.Sprintf("%d", nextID))
		b.WriteString(`" text="`)
		b.WriteString(xmlEscape(n.Title))
		b.WriteString(`"`)
		if central {
			b.WriteString(` central="true"`)
		} else {
			b.WriteString(` position="`)
			b.WriteString(fmt.Sprintf("%d,%d", x, y))
			b.WriteString(`"`)
			b.WriteString(` order="`)
			b.WriteString(fmt.Sprintf("%d", order))
			b.WriteString(`"`)
		}
		if len(n.Children) == 0 {
			b.WriteString(`/>`)
			return b.String()
		}
		b.WriteString(`>`)
		for i, c := range n.Children {
			childSide := side
			if central {
				if i%2 == 0 {
					childSide = 1
				} else {
					childSide = -1
				}
			}
			childX := childSide * (260 + depth*210)
			childY := y + (i-(len(n.Children)-1)/2)*90
			b.WriteString(f(c, false, i, depth+1, childSide, childX, childY))
		}
		b.WriteString(`</topic>`)
		return b.String()
	}
	return []byte(`<map name="LocalBilibili" version="tango" theme="prism" layout="mindmap">` + f(root, true, 0, 0, 1, 0, 0) + `</map>`), nil
}
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
func (m *LearningManager) publish() {
	if m.hub != nil {
		m.hub.PublishAny(map[string]any{"type": "learning", "status": m.Status()})
	}
}
func (m *LearningManager) schedule() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-m.stop:
			return
		case now := <-t.C:
			qs, e := m.store.LearningQueues()
			if e != nil {
				continue
			}
			for _, q := range qs {
				if !q.Enabled {
					continue
				}
				if q.NextRunAt != "" {
					next, e := time.Parse(time.RFC3339, q.NextRunAt)
					if e != nil || now.Before(next) {
						continue
					}
				}
				cs, _ := m.store.CreatorsForLearningQueue(q.ID)
				run, _ := m.store.CreateLearningRun(q, len(cs))
				for _, c := range cs {
					_, _ = m.EnqueueRun(c.ID, 0, q.ID, run.ID, "schedule")
				}
				_, _ = m.store.db.Exec(`UPDATE learning_queues SET next_run_at=?,last_run_summary=?,updated_at=? WHERE id=?`, NextCronRun(q.Cron, now.Add(time.Minute)).Format(time.RFC3339), fmt.Sprintf("已加入 %d 个UP主", len(cs)), time.Now().UTC().Format(time.RFC3339), q.ID)
			}
		}
	}
}
