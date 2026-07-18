package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"safeops-agent/internal/agent"
	"safeops-agent/internal/approval"
	"safeops-agent/internal/executor"
	"safeops-agent/internal/id"
	"safeops-agent/internal/llm"
	"safeops-agent/internal/registry"
	"safeops-agent/internal/session"
	"safeops-agent/internal/storage"
	"safeops-agent/internal/task"
	"safeops-agent/internal/trace"
)

type Server struct {
	store             storage.Store
	registry          *registry.Registry
	agent             *agent.Orchestrator
	trace             *trace.Writer
	approvals         *approval.Store
	llmProvider       *llm.RuntimeProvider
	llmSettings       *llm.SettingsStore
	llmConfigMu       sync.Mutex
	sessionMu         sync.Mutex
	runningSessions   map[string]struct{}
	resourceMu        sync.Mutex
	limits            Limits
	taskSlots         chan struct{}
	executorAllowlist *executor.ConfigManager
	runtimeLog        *runtimeLog
	approvalResumer   interface {
		Resume(context.Context, approval.Record) (task.Task, error)
	}
	webRoot string
	hub     *eventHub
	mux     *http.ServeMux
}

type Option func(*Server)

type Limits struct {
	MaxConcurrentTasks int
	MaxSessions        int
	MaxTasks           int
}

var defaultLimits = Limits{MaxConcurrentTasks: 8, MaxSessions: 1000, MaxTasks: 10000}

func WithLimits(limits Limits) Option {
	return func(server *Server) {
		if limits.MaxConcurrentTasks > 0 {
			server.limits.MaxConcurrentTasks = limits.MaxConcurrentTasks
		}
		if limits.MaxSessions > 0 {
			server.limits.MaxSessions = limits.MaxSessions
		}
		if limits.MaxTasks > 0 {
			server.limits.MaxTasks = limits.MaxTasks
		}
	}
}

func WithApprovals(store *approval.Store, resumer interface {
	Resume(context.Context, approval.Record) (task.Task, error)
}) Option {
	return func(server *Server) {
		server.approvals = store
		server.approvalResumer = resumer
	}
}

// WithWebRoot serves a prebuilt frontend from root. The directory is trusted
// deployment content; API routes remain isolated and take precedence.
func WithWebRoot(root string) Option {
	return func(server *Server) {
		server.webRoot = filepath.Clean(root)
	}
}

func WithLLM(provider *llm.RuntimeProvider, settings *llm.SettingsStore) Option {
	return func(server *Server) {
		server.llmProvider = provider
		server.llmSettings = settings
	}
}

func WithRuntimeLog(writer io.Writer) Option {
	return func(server *Server) {
		if writer != nil {
			server.runtimeLog = &runtimeLog{writer: writer}
		}
	}
}

func WithExecutorAllowlist(manager *executor.ConfigManager) Option {
	return func(server *Server) {
		server.executorAllowlist = manager
	}
}

func New(store storage.Store, registry *registry.Registry, orchestrator *agent.Orchestrator, traceWriter *trace.Writer, options ...Option) *Server {
	s := &Server{store: store, registry: registry, agent: orchestrator, trace: traceWriter, runningSessions: map[string]struct{}{}, limits: defaultLimits, hub: newEventHub(), mux: http.NewServeMux()}
	for _, option := range options {
		option(s)
	}
	s.taskSlots = make(chan struct{}, s.limits.MaxConcurrentTasks)
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.runtimeLog.handler(securityHeaders(s.mux)) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", s.health)
	s.mux.HandleFunc("GET /api/v1/overview", s.overview)
	s.mux.HandleFunc("GET /api/v1/mcp/servers", s.mcpServers)
	s.mux.HandleFunc("GET /api/v1/llm/config", s.getLLMConfig)
	s.mux.HandleFunc("PUT /api/v1/llm/config", s.putLLMConfig)
	s.mux.HandleFunc("DELETE /api/v1/llm/config", s.deleteLLMConfig)
	s.mux.HandleFunc("GET /api/v1/executor/allowlist", s.getExecutorAllowlist)
	s.mux.HandleFunc("PUT /api/v1/executor/allowlist", s.putExecutorAllowlist)
	s.mux.HandleFunc("GET /api/v1/executor/path-browser", s.browseExecutorPath)
	s.mux.HandleFunc("POST /api/v1/executor/path-browser/directories", s.createExecutorDirectory)
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverID}/enable", s.enableMCPServer)
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverID}/disable", s.disableMCPServer)
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverID}/rediscover", s.rediscoverMCPServer)
	s.mux.HandleFunc("POST /api/v1/mcp/servers/{serverID}/health", s.healthMCPServer)
	s.mux.HandleFunc("GET /api/v1/approvals", s.listApprovals)
	s.mux.HandleFunc("GET /api/v1/approvals/{approvalID}", s.getApproval)
	s.mux.HandleFunc("POST /api/v1/approvals/{approvalID}/resolve", s.resolveApproval)
	s.mux.HandleFunc("POST /api/v1/sessions", s.createSession)
	s.mux.HandleFunc("GET /api/v1/sessions", s.listSessions)
	s.mux.HandleFunc("GET /api/v1/sessions/{sessionID}", s.getSession)
	s.mux.HandleFunc("PATCH /api/v1/sessions/{sessionID}", s.updateSession)
	s.mux.HandleFunc("POST /api/v1/sessions/{sessionID}/messages", s.createMessage)
	s.mux.HandleFunc("GET /api/v1/tasks", s.listTasks)
	s.mux.HandleFunc("GET /api/v1/tasks/{taskID}", s.getTask)
	s.mux.HandleFunc("GET /api/v1/tasks/{taskID}/trace", s.getTrace)
	s.mux.HandleFunc("GET /api/v1/tasks/{taskID}/events", s.events)
	if s.webRoot != "" && s.webRoot != "." {
		s.mux.HandleFunc("GET /", s.web)
	}
}

func (s *Server) getLLMConfig(w http.ResponseWriter, _ *http.Request) {
	if s.llmProvider == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("LLM runtime provider is not configured"))
		return
	}
	writeJSON(w, http.StatusOK, s.llmProvider.Status())
}

func (s *Server) putLLMConfig(w http.ResponseWriter, r *http.Request) {
	if s.llmProvider == nil || s.llmSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("LLM runtime configuration is not enabled"))
		return
	}
	s.llmConfigMu.Lock()
	defer s.llmConfigMu.Unlock()
	var input struct {
		BaseURL string `json:"base_url"`
		APIKey  string `json:"api_key"`
		Model   string `json:"model"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	config := llm.Config{BaseURL: strings.TrimSpace(input.BaseURL), APIKey: strings.TrimSpace(input.APIKey), Model: strings.TrimSpace(input.Model)}
	if config.APIKey == "" {
		current := s.llmProvider.Config()
		if strings.TrimSpace(current.APIKey) != "" {
			config.APIKey = current.APIKey
		}
	}
	if config.BaseURL == "" || config.APIKey == "" || config.Model == "" {
		writeError(w, http.StatusBadRequest, errors.New("base_url, api_key, and model are required; api_key may be omitted only to keep an existing key"))
		return
	}
	if _, err := llm.NewOpenAICompatible(config); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := time.Now().UTC()
	stored, err := s.llmSettings.Save(config, now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.llmProvider.Configure(stored.Config(), "web", stored.UpdatedAt); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, s.llmProvider.Status())
}

func (s *Server) deleteLLMConfig(w http.ResponseWriter, _ *http.Request) {
	if s.llmProvider == nil || s.llmSettings == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("LLM runtime configuration is not enabled"))
		return
	}
	s.llmConfigMu.Lock()
	defer s.llmConfigMu.Unlock()
	if err := s.llmSettings.Delete(); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.llmProvider.Clear()
	writeJSON(w, http.StatusOK, s.llmProvider.Status())
}

func (s *Server) getExecutorAllowlist(w http.ResponseWriter, _ *http.Request) {
	if s.executorAllowlist == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("executor allowlist configuration is not enabled"))
		return
	}
	status := s.executorAllowlist.Status()
	status.WriteActionsEnabled = s.agent != nil && s.agent.Actions != nil && s.agent.FileTargets != nil
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) putExecutorAllowlist(w http.ResponseWriter, r *http.Request) {
	if s.executorAllowlist == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("executor allowlist configuration is not enabled"))
		return
	}
	var input struct {
		ManagedRoots []string `json:"managed_roots"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	status, err := s.executorAllowlist.UpdateManagedRoots(input.ManagedRoots)
	if err != nil {
		if errors.Is(err, executor.ErrInvalidManagedRoots) {
			writeError(w, http.StatusBadRequest, err)
		} else {
			writeError(w, http.StatusInternalServerError, err)
		}
		return
	}
	status.WriteActionsEnabled = s.agent != nil && s.agent.Actions != nil && s.agent.FileTargets != nil
	writeJSON(w, http.StatusOK, status)
}

func (s *Server) browseExecutorPath(w http.ResponseWriter, r *http.Request) {
	if s.executorAllowlist == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("executor allowlist configuration is not enabled"))
		return
	}
	limit := 200
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("limit must be a number"))
			return
		}
		limit = parsed
	}
	browser, err := s.executorAllowlist.BrowsePath(r.URL.Query().Get("path"), r.URL.Query().Get("mode"), limit)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, browser)
}

func (s *Server) createExecutorDirectory(w http.ResponseWriter, r *http.Request) {
	if s.executorAllowlist == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("executor allowlist configuration is not enabled"))
		return
	}
	var input struct {
		Parent string `json:"parent"`
		Name   string `json:"name"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	browser, err := s.executorAllowlist.CreateDirectory(input.Parent, input.Name)
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusCreated, browser)
}

func (s *Server) web(w http.ResponseWriter, r *http.Request) {
	cleanPath := path.Clean("/" + r.URL.Path)
	if strings.HasPrefix(cleanPath, "/api/") {
		writeError(w, http.StatusNotFound, errors.New("API route not found"))
		return
	}
	relative := strings.TrimPrefix(cleanPath, "/")
	if relative != "" {
		candidate := filepath.Join(s.webRoot, filepath.FromSlash(relative))
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			serveWebFile(w, r, candidate, info)
			return
		}
		if path.Ext(cleanPath) != "" {
			http.NotFound(w, r)
			return
		}
	}
	index := filepath.Join(s.webRoot, "index.html")
	info, err := os.Stat(index)
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	serveWebFile(w, r, index, info)
}

func serveWebFile(w http.ResponseWriter, r *http.Request, filename string, info os.FileInfo) {
	file, err := os.Open(filename)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if path.Ext(filename) == ".html" {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("approval store is not configured"))
		return
	}
	values, err := s.approvals.List(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": values})
}

func (s *Server) getApproval(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("approval store is not configured"))
		return
	}
	value, err := s.approvals.Get(r.Context(), r.PathValue("approvalID"))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) resolveApproval(w http.ResponseWriter, r *http.Request) {
	if s.approvals == nil || s.approvalResumer == nil {
		writeError(w, http.StatusServiceUnavailable, errors.New("approval workflow is not configured"))
		return
	}
	var input struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	approve := false
	switch strings.ToUpper(strings.TrimSpace(input.Decision)) {
	case "APPROVE":
		approve = true
	case "REJECT":
	default:
		writeError(w, http.StatusBadRequest, errors.New("decision must be APPROVE or REJECT"))
		return
	}
	if !s.acquireTaskSlot(w) {
		return
	}
	defer s.releaseTaskSlot()
	record, err := s.approvals.Resolve(r.Context(), r.PathValue("approvalID"), approve, input.Reason)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	resumed, err := s.approvalResumer.Resume(r.Context(), record)
	if err != nil {
		s.hub.publish(agent.RuntimeEvent{Type: "task.progress", TaskID: resumed.ID, State: resumed.State, Message: "审批后任务恢复失败：" + err.Error(), Timestamp: resumed.UpdatedAt})
		writeJSON(w, http.StatusBadGateway, map[string]any{"approval": record, "task": resumed, "resume_error": err.Error()})
		return
	}
	message := "审批结果已持久化，任务已自动恢复"
	switch resumed.State {
	case task.WaitingApproval:
		message = "前一动作已验证，下一受控动作正在等待独立审批"
	case task.Completed:
		message = "审批后任务已执行、验证并完成"
	case task.Cancelled:
		message = "审批被拒绝，任务已安全结束"
	}
	s.hub.publish(agent.RuntimeEvent{Type: "task.progress", TaskID: resumed.ID, State: resumed.State, Message: message, Timestamp: resumed.UpdatedAt})
	writeJSON(w, http.StatusOK, map[string]any{"approval": record, "task": resumed})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	states := s.registry.States()
	healthy := true
	for _, state := range states {
		if state.Manifest.Enabled && state.Status != registry.StatusHealthy {
			healthy = false
		}
	}
	status := http.StatusOK
	if !healthy {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, map[string]any{"status": map[bool]string{true: "ok", false: "degraded"}[healthy], "mcp_servers": states})
}

func (s *Server) overview(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	tasks, err := s.store.ListTasks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	approvalCounts := map[approval.Status]int{}
	if s.approvals != nil {
		values, listErr := s.approvals.List(r.Context())
		if listErr != nil {
			writeError(w, http.StatusInternalServerError, listErr)
			return
		}
		for _, value := range values {
			approvalCounts[value.Status]++
		}
	}
	sessionCounts := map[string]int{"active": 0, "archived": 0}
	for _, value := range sessions {
		if value.Archived {
			sessionCounts["archived"]++
		} else {
			sessionCounts["active"]++
		}
	}
	taskCounts := map[task.State]int{}
	for _, value := range tasks {
		taskCounts[value.State]++
	}
	serverCounts := map[string]int{"total": 0, "healthy": 0, "tools": 0}
	for _, value := range s.registry.States() {
		serverCounts["total"]++
		serverCounts["tools"] += len(value.Tools)
		if value.Status == registry.StatusHealthy {
			serverCounts["healthy"]++
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"mcp": serverCounts, "sessions": sessionCounts, "tasks": taskCounts, "approvals": approvalCounts, "generated_at": time.Now().UTC()})
}
func (s *Server) mcpServers(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"servers": s.registry.States()})
}

func (s *Server) enableMCPServer(w http.ResponseWriter, r *http.Request) {
	s.changeMCPServer(w, r, func(ctx context.Context, id string) error { return s.registry.SetEnabled(ctx, id, true) })
}
func (s *Server) disableMCPServer(w http.ResponseWriter, r *http.Request) {
	s.changeMCPServer(w, r, func(ctx context.Context, id string) error { return s.registry.SetEnabled(ctx, id, false) })
}
func (s *Server) rediscoverMCPServer(w http.ResponseWriter, r *http.Request) {
	s.changeMCPServer(w, r, s.registry.Discover)
}
func (s *Server) healthMCPServer(w http.ResponseWriter, r *http.Request) {
	s.changeMCPServer(w, r, s.registry.Health)
}
func (s *Server) changeMCPServer(w http.ResponseWriter, r *http.Request, operation func(context.Context, string) error) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	id := r.PathValue("serverID")
	if err := operation(ctx, id); err != nil {
		status := http.StatusConflict
		if strings.Contains(err.Error(), "unknown MCP server") {
			status = http.StatusNotFound
		}
		writeError(w, status, err)
		return
	}
	for _, state := range s.registry.States() {
		if state.Manifest.ID == id {
			writeJSON(w, http.StatusOK, state)
			return
		}
	}
	writeError(w, http.StatusNotFound, errors.New("MCP server state not found"))
}

func (s *Server) createSession(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	now := time.Now().UTC()
	if strings.TrimSpace(in.Name) == "" {
		in.Name = "新会话"
	}
	if len([]rune(strings.TrimSpace(in.Name))) > 128 {
		writeError(w, http.StatusBadRequest, errors.New("session name must not exceed 128 characters"))
		return
	}
	s.resourceMu.Lock()
	defer s.resourceMu.Unlock()
	values, err := s.store.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if len(values) >= s.limits.MaxSessions {
		writeRetentionError(w, "session retention limit reached")
		return
	}
	value := session.Session{ID: id.New("ses"), Name: strings.TrimSpace(in.Name), PinnedContext: map[string]string{}, CreatedAt: now, UpdatedAt: now}
	if err := s.store.SaveSession(r.Context(), value); err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusCreated, value)
}
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request) {
	values, err := s.store.ListSessions(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	query := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	if len([]rune(query)) > 128 {
		writeError(w, http.StatusBadRequest, errors.New("session search must not exceed 128 characters"))
		return
	}
	archiveFilter := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("archived")))
	if archiveFilter == "" {
		archiveFilter = "false"
	}
	if archiveFilter != "false" && archiveFilter != "true" && archiveFilter != "all" {
		writeError(w, http.StatusBadRequest, errors.New("archived must be false, true, or all"))
		return
	}
	filtered := make([]session.Session, 0, len(values))
	for _, value := range values {
		if archiveFilter == "false" && value.Archived || archiveFilter == "true" && !value.Archived {
			continue
		}
		if query != "" && !sessionMatches(value, query) {
			continue
		}
		filtered = append(filtered, value)
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": filtered, "query": query, "archived": archiveFilter})
}

func (s *Server) updateSession(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Name     *string `json:"name"`
		Archived *bool   `json:"archived"`
	}
	if err := decodeJSON(r, &input); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if input.Name == nil && input.Archived == nil {
		writeError(w, http.StatusBadRequest, errors.New("name or archived is required"))
		return
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" || len([]rune(name)) > 128 {
			writeError(w, http.StatusBadRequest, errors.New("session name must contain 1-128 characters"))
			return
		}
		input.Name = &name
	}
	s.sessionMu.Lock()
	defer s.sessionMu.Unlock()
	if input.Archived != nil && *input.Archived {
		if _, running := s.runningSessions[r.PathValue("sessionID")]; running {
			writeError(w, http.StatusConflict, errors.New("session has a running task"))
			return
		}
		values, err := s.store.ListTasks(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, err)
			return
		}
		for _, value := range values {
			if value.SessionID == r.PathValue("sessionID") && !terminal(value.State) {
				writeError(w, http.StatusConflict, fmt.Errorf("session has active task %s in state %s", value.ID, value.State))
				return
			}
		}
	}
	value, err := s.store.UpdateSession(r.Context(), r.PathValue("sessionID"), func(value *session.Session) error {
		if input.Name != nil {
			value.Name = *input.Name
		}
		if input.Archived != nil {
			value.Archived = *input.Archived
		}
		value.UpdatedAt = time.Now().UTC()
		return nil
	})
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func sessionMatches(value session.Session, query string) bool {
	if strings.Contains(strings.ToLower(value.Name+"\n"+value.Summary), query) {
		return true
	}
	for _, message := range value.Messages {
		if strings.Contains(strings.ToLower(message.Content), query) {
			return true
		}
	}
	return false
}
func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	value, err := s.store.GetSession(r.Context(), r.PathValue("sessionID"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *Server) createMessage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.PathValue("sessionID")
	var in struct {
		Content string `json:"content"`
	}
	if err := decodeJSON(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" {
		writeError(w, http.StatusBadRequest, errors.New("content is required"))
		return
	}
	s.sessionMu.Lock()
	currentSession, err := s.store.GetSession(r.Context(), sessionID)
	if errors.Is(err, storage.ErrNotFound) {
		s.sessionMu.Unlock()
		writeError(w, http.StatusNotFound, err)
		return
	} else if err != nil {
		s.sessionMu.Unlock()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	if currentSession.Archived {
		s.sessionMu.Unlock()
		writeError(w, http.StatusConflict, errors.New("archived session must be restored before adding messages"))
		return
	}
	if s.agent == nil {
		s.sessionMu.Unlock()
		writeError(w, http.StatusServiceUnavailable, errors.New("agent runtime is not configured"))
		return
	}
	if _, running := s.runningSessions[sessionID]; running {
		s.sessionMu.Unlock()
		writeError(w, http.StatusConflict, errors.New("session already has a running task"))
		return
	}
	s.resourceMu.Lock()
	tasks, err := s.store.ListTasks(r.Context())
	if err != nil {
		s.resourceMu.Unlock()
		s.sessionMu.Unlock()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	for _, value := range tasks {
		if value.SessionID == sessionID && !terminal(value.State) {
			s.resourceMu.Unlock()
			s.sessionMu.Unlock()
			writeError(w, http.StatusConflict, fmt.Errorf("session already has active task %s in state %s", value.ID, value.State))
			return
		}
	}
	if len(tasks) >= s.limits.MaxTasks {
		s.resourceMu.Unlock()
		s.sessionMu.Unlock()
		writeRetentionError(w, "task retention limit reached")
		return
	}
	if !s.reserveTaskSlot() {
		s.resourceMu.Unlock()
		s.sessionMu.Unlock()
		writeLimitError(w, "concurrent task limit reached")
		return
	}
	s.runningSessions[sessionID] = struct{}{}
	taskID := id.New("task")
	if _, err := s.agent.Prepare(r.Context(), taskID, sessionID, in.Content); err != nil {
		delete(s.runningSessions, sessionID)
		s.releaseTaskSlot()
		s.resourceMu.Unlock()
		s.sessionMu.Unlock()
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	s.resourceMu.Unlock()
	s.sessionMu.Unlock()
	go func() {
		defer func() {
			s.sessionMu.Lock()
			delete(s.runningSessions, sessionID)
			s.sessionMu.Unlock()
			s.releaseTaskSlot()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		_, _ = s.agent.Run(ctx, taskID, sessionID, in.Content, s.hub.publish)
	}()
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": taskID, "session_id": sessionID, "state": task.New, "events_url": fmt.Sprintf("/api/v1/tasks/%s/events", taskID)})
}

func (s *Server) reserveTaskSlot() bool {
	select {
	case s.taskSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Server) acquireTaskSlot(w http.ResponseWriter) bool {
	if s.reserveTaskSlot() {
		return true
	}
	writeLimitError(w, "concurrent task limit reached")
	return false
}

func (s *Server) releaseTaskSlot() { <-s.taskSlots }

func writeLimitError(w http.ResponseWriter, message string) {
	w.Header().Set("Retry-After", "1")
	writeError(w, http.StatusTooManyRequests, errors.New(message))
}

func writeRetentionError(w http.ResponseWriter, message string) {
	writeError(w, http.StatusInsufficientStorage, errors.New(message))
}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	value, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	values, err := s.store.ListTasks(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	sessionID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	state := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("state")))
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		parsed, parseErr := strconv.Atoi(raw)
		if parseErr != nil || parsed < 1 || parsed > 500 {
			writeError(w, http.StatusBadRequest, errors.New("limit must be within 1-500"))
			return
		}
		limit = parsed
	}
	filtered := make([]task.Task, 0, min(limit, len(values)))
	for _, value := range values {
		if sessionID != "" && value.SessionID != sessionID || state != "" && string(value.State) != state {
			continue
		}
		filtered = append(filtered, value)
		if len(filtered) == limit {
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": filtered, "limit": limit})
}
func (s *Server) getTrace(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("taskID")
	if err := s.trace.VerifyIntegrity(taskID); err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	events, err := s.trace.Read(taskID)
	if err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"integrity": "VALID", "events": events})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	taskSnapshot, err := s.store.GetTask(r.Context(), r.PathValue("taskID"))
	if errors.Is(err, storage.ErrNotFound) {
		writeError(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, errors.New("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	lastID := uint64(0)
	if value := strings.TrimSpace(r.Header.Get("Last-Event-ID")); value != "" {
		lastID, err = strconv.ParseUint(value, 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, errors.New("Last-Event-ID must be an unsigned integer"))
			return
		}
	}
	window, ch, unsubscribe := s.hub.subscribe(r.PathValue("taskID"), lastID)
	defer unsubscribe()
	send := func(eventName string, event agent.RuntimeEvent) bool {
		b, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if event.Sequence > 0 {
			if _, err := fmt.Fprintf(w, "id: %d\n", event.Sequence); err != nil {
				return false
			}
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventName, b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if window.Gap {
		gap := agent.RuntimeEvent{Type: "task.gap", TaskID: taskSnapshot.ID, State: taskSnapshot.State, Message: fmt.Sprintf("SSE 重放窗口存在缺口；last=%d oldest=%d latest=%d，请回读持久 Task/Trace", lastID, window.Oldest, window.Latest), Timestamp: time.Now().UTC()}
		if !send("task.gap", gap) {
			return
		}
	}
	for _, event := range window.Events {
		if !send("task.progress", event) {
			return
		}
		if terminal(event.State) {
			return
		}
	}
	if len(window.Events) == 0 && (terminal(taskSnapshot.State) || window.Gap) {
		message := "任务已从持久化状态恢复"
		if taskSnapshot.State == task.Failed {
			message = "任务失败：" + taskSnapshot.FailureReason
		}
		_ = send("task.snapshot", agent.RuntimeEvent{Type: "task.snapshot", TaskID: taskSnapshot.ID, State: taskSnapshot.State, Message: message, Timestamp: taskSnapshot.UpdatedAt})
		if terminal(taskSnapshot.State) {
			return
		}
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok || !send("task.progress", event) {
				return
			}
			if terminal(event.State) {
				return
			}
		case <-heartbeat.C:
			if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func terminal(state task.State) bool {
	return state == task.Completed || state == task.Failed || state == task.Cancelled
}
func decodeJSON(r *http.Request, out any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 32<<10)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain one JSON object")
	}
	return nil
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{"error": err.Error()})
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; connect-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; img-src 'self' data:; object-src 'none'; script-src 'self'; style-src 'self'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		next.ServeHTTP(w, r)
	})
}
