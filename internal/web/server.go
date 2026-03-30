package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/tasks"
	"vsc-taskrunner/internal/uiconfig"
)

// Server holds the HTTP state for the runtask web UI.
type Server struct {
	repo    git.RepositoryStore
	config  *uiconfig.UIConfig
	manager *TaskManager
	auth    *Authenticator
	metrics *MetricsService
	mux     *http.ServeMux

	branchMetaMu    sync.RWMutex
	branchMetaCache map[string]branchAPIItem
	branchMetaReady bool
	branchWarmMu    sync.Mutex
	branchWarmCh    chan struct{}

	ephemeralEmulationIdle time.Duration
	lastRequestMu          sync.Mutex
	lastRequestAt          time.Time
}

type branchTaskResponse struct {
	Label              string                 `json:"label"`
	Type               string                 `json:"type"`
	Group              string                 `json:"group,omitempty"`
	DependsOn          []string               `json:"dependsOn,omitempty"`
	DependsOrder       string                 `json:"dependsOrder,omitempty"`
	Hidden             bool                   `json:"hidden,omitempty"`
	Background         bool                   `json:"background,omitempty"`
	Inputs             []tasks.Input          `json:"inputs,omitempty"`
	Artifact           bool                   `json:"artifact,omitempty"`
	WorktreeDisabled   bool                   `json:"worktreeDisabled,omitempty"`
	PreRunTasks        []preRunTaskResponse   `json:"preRunTasks,omitempty"`
	Artifacts          []artifactRuleResponse `json:"artifacts,omitempty"`
	TaskFilePath       string                 `json:"taskFilePath,omitempty"`
	ResolvedTaskLabels []string               `json:"resolvedTaskLabels,omitempty"`
}

type preRunTaskResponse struct {
	Command string         `json:"command"`
	Args    []string       `json:"args,omitempty"`
	CWD     string         `json:"cwd,omitempty"`
	Shell   *shellResponse `json:"shell,omitempty"`
}

type shellResponse struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args,omitempty"`
}

type artifactRuleResponse struct {
	Path         string `json:"path"`
	Format       string `json:"format,omitempty"`
	NameTemplate string `json:"nameTemplate,omitempty"`
}

type branchAPIItem struct {
	FullRef    string               `json:"fullRef"`
	ShortName  string               `json:"shortName"`
	IsRemote   bool                 `json:"isRemote"`
	CommitHash string               `json:"commitHash"`
	CommitDate string               `json:"commitDate,omitempty"`
	Tasks      []branchTaskResponse `json:"tasks,omitempty"`
	LoadError  string               `json:"loadError,omitempty"`
	FetchedAt  string               `json:"fetchedAt,omitempty"`
}

// NewServer creates a new HTTP server.
func NewServer(repo git.RepositoryStore, config *uiconfig.UIConfig, manager *TaskManager, auth *Authenticator) *Server {
	s := &Server{
		repo:                   repo,
		config:                 config,
		manager:                manager,
		auth:                   auth,
		metrics:                newMetricsService(config, manager),
		mux:                    http.NewServeMux(),
		branchMetaCache:        make(map[string]branchAPIItem),
		ephemeralEmulationIdle: loadEphemeralEmulationIdle(),
	}
	s.routes()
	return s
}

// Handler returns the root HTTP handler.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.maybeResetEphemeralCache(r.Context())
		s.mux.ServeHTTP(w, r)
	})
}

// ListenAndServe starts the HTTP server on the configured address.
func (s *Server) ListenAndServe() error {
	server := &http.Server{
		Addr:    s.config.Addr(),
		Handler: s.Handler(),
	}
	return server.ListenAndServe()
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/health", s.handleHealth)
	s.mux.HandleFunc("/", s.handleStatic)

	if s.auth != nil && s.auth.Enabled() {
		sessionOnly := s.auth.RequireAuth
		apiRead := func(next http.HandlerFunc) http.HandlerFunc {
			return s.auth.RequireAuthWithPolicy(AuthPolicy{
				AllowBearer:         true,
				RequiredTokenScopes: []string{APITokenScopeRunsRead},
			}, next)
		}
		apiAny := func(next http.HandlerFunc) http.HandlerFunc {
			return s.auth.RequireAuthWithPolicy(AuthPolicy{
				AllowBearer: true,
			}, next)
		}
		apiWrite := func(next http.HandlerFunc) http.HandlerFunc {
			return s.auth.RequireAuthWithPolicy(AuthPolicy{
				AllowBearer:         true,
				RequiredTokenScopes: []string{APITokenScopeRunsWrite},
			}, next)
		}
		s.mux.HandleFunc("/auth/login", s.auth.HandleLogin)
		s.mux.HandleFunc("/auth/callback", s.auth.HandleCallback)
		s.mux.HandleFunc("/auth/logout", s.auth.HandleLogout)
		s.mux.HandleFunc("GET /api/me", apiAny(s.handleMe))
		s.mux.HandleFunc("GET /api/settings", apiAny(s.handleSettings))
		s.mux.HandleFunc("GET /api/git/branches", sessionOnly(s.handleBranches))
		s.mux.HandleFunc("POST /api/git/fetch", sessionOnly(s.handleFetch))
		s.mux.HandleFunc("GET /api/git/branches/{branch}/tasks", sessionOnly(s.handleBranchTasks))
		s.mux.HandleFunc("GET /api/runs", apiRead(s.handleRuns))
		s.mux.HandleFunc("POST /api/runs", apiWrite(s.handleRuns))
		s.mux.HandleFunc("GET /api/metrics/stream", sessionOnly(s.handleMetricsStream))
		s.mux.HandleFunc("POST /api/cleanup", sessionOnly(s.handleCleanup))
		s.registerRunRoutes(apiRead)
		if s.auth.TokenService() != nil && s.auth.TokenService().Enabled() {
			s.mux.HandleFunc("GET /api/tokens", sessionOnly(s.handleAPITokens))
			s.mux.HandleFunc("POST /api/tokens", sessionOnly(s.handleAPITokens))
			s.mux.HandleFunc("POST /api/tokens/{tokenId}/revoke", sessionOnly(s.handleRevokeAPIToken))
		}
		return
	}

	s.mux.HandleFunc("GET /api/me", s.handleMe)
	s.mux.HandleFunc("GET /api/settings", s.handleSettings)
	s.mux.HandleFunc("GET /api/git/branches", s.handleBranches)
	s.mux.HandleFunc("POST /api/git/fetch", s.handleFetch)
	s.mux.HandleFunc("GET /api/git/branches/{branch}/tasks", s.handleBranchTasks)
	s.mux.HandleFunc("GET /api/runs", s.handleRuns)
	s.mux.HandleFunc("POST /api/runs", s.handleRuns)
	s.mux.HandleFunc("GET /api/metrics/stream", s.handleMetricsStream)
	s.mux.HandleFunc("POST /api/cleanup", s.handleCleanup)
	s.registerRunRoutes(func(next http.HandlerFunc) http.HandlerFunc { return next })
}

func (s *Server) registerRunRoutes(wrap func(http.HandlerFunc) http.HandlerFunc) {
	s.mux.HandleFunc("GET /api/runs/{runId}", wrap(s.handleRunDetail))
	s.mux.HandleFunc("GET /api/runs/{runId}/log", wrap(s.handleRunLog))
	s.mux.HandleFunc("GET /api/runs/{runId}/artifacts", wrap(s.handleRunArtifacts))
	s.mux.HandleFunc("GET /api/runs/{runId}/artifacts/{path...}", wrap(s.handleRunArtifactFile))
	s.mux.HandleFunc("GET /api/runs/{runId}/worktree", wrap(s.handleRunWorktreeList))
	s.mux.HandleFunc("GET /api/runs/{runId}/worktree/{path...}", wrap(s.handleRunWorktreeFile))
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, value interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func (s *Server) writeError(w http.ResponseWriter, status int, msg string) {
	s.writeJSON(w, status, map[string]interface{}{
		"error": msg,
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil {
		s.writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated":    false,
			"claims":           map[string]string{},
			"canRun":           true,
			"isAdmin":          false,
			"canManageTokens":  false,
			"apiTokensEnabled": false,
		})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated":    true,
		"subject":          SubjectFromClaims(claims),
		"claims":           ClaimsAsJSON(claims),
		"canRun":           s.config.CanRun(claims),
		"isAdmin":          s.config.IsAdminUser(claims),
		"canManageTokens":  AuthMethodFromContext(r.Context()) == AuthMethodSession && s.config.CanManageTokens(claims) && s.auth != nil && s.auth.TokenService() != nil && s.auth.TokenService().Enabled(),
		"apiTokensEnabled": s.auth != nil && s.auth.TokenService() != nil && s.auth.TokenService().Enabled(),
	})
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		s.writeError(w, http.StatusInternalServerError, "repository store not configured")
		return
	}
	branches, err := s.ensureBranchMetadataLoaded(r.Context())
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, branches)
}

func (s *Server) handleFetch(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		s.writeError(w, http.StatusInternalServerError, "repository store not configured")
		return
	}
	if err := s.repo.Refresh(r.Context()); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.resetBranchMetaCache()
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleBranchTasks(w http.ResponseWriter, r *http.Request) {
	if s.repo == nil {
		s.writeError(w, http.StatusInternalServerError, "repository store not configured")
		return
	}
	branch := r.PathValue("branch")
	if !s.config.MatchBranch(branch) {
		s.writeError(w, http.StatusForbidden, "branch not allowed")
		return
	}
	if item, ok := s.cachedBranchMeta(branch); ok {
		if item.LoadError != "" {
			s.writeError(w, http.StatusInternalServerError, item.LoadError)
			return
		}
		s.writeJSON(w, http.StatusOK, item.Tasks)
		return
	}

	item, err := s.resolveBranchMetadata(r.Context(), git.Branch{ShortName: branch})
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if item.LoadError != "" {
		s.writeError(w, http.StatusInternalServerError, item.LoadError)
		return
	}
	s.storeBranchMeta(item)
	s.writeJSON(w, http.StatusOK, item.Tasks)
}

func (s *Server) resolveBranches(ctx context.Context) ([]branchAPIItem, error) {
	branches, err := s.repo.ListBranches(ctx)
	if err != nil {
		return nil, err
	}
	allowed := make([]git.Branch, 0, len(branches))
	for _, branch := range branches {
		if s.config.MatchBranch(branch.ShortName) {
			allowed = append(allowed, branch)
		}
	}
	items := make([]branchAPIItem, 0, len(allowed))
	cache := make(map[string]branchAPIItem, len(allowed))
	for _, branch := range allowed {
		item, err := s.resolveBranchMetadata(ctx, branch)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
		cache[item.ShortName] = item
	}
	s.branchMetaMu.Lock()
	s.branchMetaCache = cache
	s.branchMetaReady = true
	s.branchMetaMu.Unlock()
	return items, nil
}

func (s *Server) WarmBranchMetadata(ctx context.Context) error {
	if s.repo == nil {
		return nil
	}
	_, err := s.ensureBranchMetadataLoaded(ctx)
	return err
}

func (s *Server) ensureBranchMetadataLoaded(ctx context.Context) ([]branchAPIItem, error) {
	if items, ok := s.readyBranchMetaSnapshot(); ok {
		return items, nil
	}

	s.branchWarmMu.Lock()
	if ch := s.branchWarmCh; ch != nil {
		s.branchWarmMu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		items, ok := s.readyBranchMetaSnapshot()
		if !ok {
			return nil, nil
		}
		return items, nil
	}
	ch := make(chan struct{})
	s.branchWarmCh = ch
	s.branchWarmMu.Unlock()

	items, err := s.resolveBranches(ctx)

	s.branchWarmMu.Lock()
	close(ch)
	s.branchWarmCh = nil
	s.branchWarmMu.Unlock()

	if err != nil {
		return nil, err
	}
	return items, nil
}

func (s *Server) resolveBranchMetadata(ctx context.Context, branch git.Branch) (branchAPIItem, error) {
	item := branchAPIItem{
		FullRef:    branch.FullRef,
		ShortName:  branch.ShortName,
		IsRemote:   branch.IsRemote,
		CommitHash: branch.CommitHash,
		FetchedAt:  time.Now().UTC().Format(time.RFC3339),
	}
	taskFile := filepath.ToSlash(filepath.Join(".vscode", "tasks.json"))
	commitHash, commitDate, data, err := s.repo.ReadBranchMetadata(ctx, branch.ShortName, taskFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if commitHash != "" {
				item.CommitHash = commitHash
			}
			if !commitDate.IsZero() {
				item.CommitDate = commitDate.Format(time.RFC3339)
			}
			item.Tasks = []branchTaskResponse{}
			return item, nil
		}
		item.LoadError = fmt.Sprintf("load branch %s metadata: %v", branch.ShortName, err)
		log.Printf("runtask branch preload failed branch=%q error=%v", branch.ShortName, err)
		return item, nil
	}
	item.CommitHash = commitHash
	item.CommitDate = commitDate.Format(time.RFC3339)
	workspaceRoot := filepath.Dir(filepath.Dir(taskFile))
	items, err := loadBranchTasks(data, taskFile, workspaceRoot, s.config)
	if err != nil {
		item.LoadError = err.Error()
		log.Printf("runtask branch preload parse failed branch=%q error=%v", branch.ShortName, err)
		return item, nil
	}
	item.Tasks = items
	return item, nil
}

func loadBranchTasks(data []byte, tasksPath string, workspaceRoot string, cfg *uiconfig.UIConfig) ([]branchTaskResponse, error) {
	loadOptions := tasks.LoadOptions{
		Path:          tasksPath,
		WorkspaceRoot: workspaceRoot,
	}
	file, err := tasks.LoadFileFromBytes(data, loadOptions)
	if err != nil {
		return nil, err
	}
	definitions := tasks.BuildTaskDefinitionCatalog(file, workspaceRoot, tasksPath)
	items := make([]branchTaskResponse, 0, len(file.Tasks))
	for _, task := range file.Tasks {
		taskCfg, ok := cfg.TaskConfig(task.Label)
		if !ok {
			continue
		}
		resolvedTaskLabels, err := tasks.ResolveTaskSelectionLabels(definitions, task.Label, tasks.ResolveOptions{
			WorkspaceRoot:  workspaceRoot,
			TaskFilePath:   tasksPath,
			Inputs:         file.Inputs,
			NonInteractive: true,
		})
		if err != nil {
			return nil, fmt.Errorf("resolve task selection %s: %w", task.Label, err)
		}
		items = append(items, branchTaskResponse{
			Label:              task.Label,
			Type:               task.EffectiveType(),
			Group:              taskGroupName(task.Group),
			DependsOn:          task.Dependencies.Labels(),
			DependsOrder:       task.DependsOrder,
			Hidden:             task.Hide,
			Background:         task.IsBackground,
			Inputs:             referencedInputs(task, file.Inputs),
			Artifact:           len(taskCfg.Artifacts) > 0,
			WorktreeDisabled:   taskCfg.WorktreeDisabled,
			PreRunTasks:        buildPreRunTaskResponses(taskCfg.PreRunTasks),
			Artifacts:          buildArtifactRuleResponses(taskCfg.Artifacts),
			TaskFilePath:       tasksPath,
			ResolvedTaskLabels: resolvedTaskLabels,
		})
	}
	return items, nil
}

func buildPreRunTaskResponses(items []uiconfig.PreRunTaskConfig) []preRunTaskResponse {
	if len(items) == 0 {
		return nil
	}
	resp := make([]preRunTaskResponse, 0, len(items))
	for _, item := range items {
		entry := preRunTaskResponse{
			Command: item.Command,
			Args:    append([]string(nil), item.Args...),
			CWD:     item.CWD,
		}
		if item.Shell != nil {
			entry.Shell = &shellResponse{
				Executable: item.Shell.Executable,
				Args:       append([]string(nil), item.Shell.Args...),
			}
		}
		resp = append(resp, entry)
	}
	return resp
}

func buildArtifactRuleResponses(items []uiconfig.ArtifactRuleConfig) []artifactRuleResponse {
	if len(items) == 0 {
		return nil
	}
	resp := make([]artifactRuleResponse, 0, len(items))
	for _, item := range items {
		resp = append(resp, artifactRuleResponse{
			Path:         item.Path,
			Format:       item.Format,
			NameTemplate: item.NameTemplate,
		})
	}
	return resp
}

func (s *Server) cachedBranchMeta(branch string) (branchAPIItem, bool) {
	s.branchMetaMu.RLock()
	defer s.branchMetaMu.RUnlock()
	item, ok := s.branchMetaCache[branch]
	return item, ok
}

func (s *Server) storeBranchMeta(item branchAPIItem) {
	s.branchMetaMu.Lock()
	defer s.branchMetaMu.Unlock()
	s.branchMetaCache[item.ShortName] = item
}

func (s *Server) readyBranchMetaSnapshot() ([]branchAPIItem, bool) {
	s.branchMetaMu.RLock()
	defer s.branchMetaMu.RUnlock()
	if !s.branchMetaReady {
		return nil, false
	}
	items := make([]branchAPIItem, 0, len(s.branchMetaCache))
	for _, item := range s.branchMetaCache {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].ShortName < items[j].ShortName
	})
	return items, true
}

func (s *Server) resetBranchMetaCache() {
	s.branchMetaMu.Lock()
	defer s.branchMetaMu.Unlock()
	s.branchMetaCache = make(map[string]branchAPIItem)
	s.branchMetaReady = false
}

func loadEphemeralEmulationIdle() time.Duration {
	raw := strings.TrimSpace(os.Getenv("RUNTASK_EPHEMERAL_EMULATION_IDLE"))
	if raw == "" {
		return 0
	}
	idle, err := time.ParseDuration(raw)
	if err != nil || idle <= 0 {
		log.Printf("runtask ephemeral emulation disabled invalid RUNTASK_EPHEMERAL_EMULATION_IDLE=%q", raw)
		return 0
	}
	log.Printf("runtask ephemeral emulation enabled idle=%s", idle)
	return idle
}

func (s *Server) maybeResetEphemeralCache(ctx context.Context) {
	if s.ephemeralEmulationIdle <= 0 || s.repo == nil {
		return
	}
	s.lastRequestMu.Lock()
	defer s.lastRequestMu.Unlock()

	now := time.Now()
	if !s.lastRequestAt.IsZero() && now.Sub(s.lastRequestAt) >= s.ephemeralEmulationIdle {
		if s.manager != nil && s.manager.hasActiveRuns() {
			log.Printf("runtask ephemeral emulation skipped active runs present")
		} else if err := s.resetEphemeralCache(ctx); err != nil {
			log.Printf("runtask ephemeral emulation reset failed: %v", err)
		}
	}
	s.lastRequestAt = now
}

func (s *Server) resetEphemeralCache(ctx context.Context) error {
	basePath := strings.TrimSpace(s.repo.BasePath())
	if basePath == "" {
		return nil
	}
	log.Printf("runtask ephemeral emulation deleting repository cache=%q idle=%s", basePath, s.ephemeralEmulationIdle)
	if err := os.RemoveAll(basePath); err != nil {
		return fmt.Errorf("remove repository cache %s: %w", basePath, err)
	}
	if err := s.repo.Maintenance(ctx); err != nil {
		return fmt.Errorf("recreate repository cache %s: %w", basePath, err)
	}
	s.branchMetaMu.Lock()
	s.branchMetaCache = make(map[string]branchAPIItem)
	s.branchMetaReady = false
	s.branchMetaMu.Unlock()
	log.Printf("runtask ephemeral emulation reset repository cache=%q", basePath)
	return nil
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		metas, err := s.manager.history.List()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if metas == nil {
			metas = []*RunMeta{}
		}
		s.writeJSON(w, http.StatusOK, metas)
	case http.MethodPost:
		var req struct {
			Branch      string            `json:"branch"`
			TaskLabel   string            `json:"taskLabel"`
			User        string            `json:"user,omitempty"`
			InputValues map[string]string `json:"inputValues,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if !s.config.MatchBranch(req.Branch) {
			s.writeError(w, http.StatusForbidden, "branch not allowed")
			return
		}
		if !s.config.MatchTask(req.TaskLabel) {
			s.writeError(w, http.StatusForbidden, "task not allowed")
			return
		}
		if claims := ClaimsFromContext(r.Context()); claims != nil {
			req.User = SubjectFromClaims(claims)
			if !s.config.CanRun(claims) {
				s.writeError(w, http.StatusForbidden, "user is readonly")
				return
			}
		}
		if req.User == "" {
			req.User = "anonymous"
		}
		tokenLabel := ""
		if AuthMethodFromContext(r.Context()) == AuthMethodToken {
			tokenLabel = TokenLabelFromContext(r.Context())
		}

		meta, err := s.manager.StartRunWithInputs(r.Context(), req.Branch, req.TaskLabel, req.User, tokenLabel, req.InputValues)
		if err != nil {
			log.Printf("runtask api run start failed branch=%q task=%q user=%q token_label=%q auth_method=%q remote_addr=%q error=%v", req.Branch, req.TaskLabel, req.User, tokenLabel, AuthMethodFromContext(r.Context()), r.RemoteAddr, err)
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		type runStartResponse struct {
			RunID      string         `json:"runId"`
			RunKey     string         `json:"runKey"`
			Branch     string         `json:"branch"`
			TaskLabel  string         `json:"taskLabel"`
			RunNumber  int            `json:"runNumber"`
			Status     RunStatus      `json:"status"`
			StartTime  time.Time      `json:"startTime"`
			EndTime    *time.Time     `json:"endTime,omitempty"`
			ExitCode   int            `json:"exitCode"`
			User       string         `json:"user,omitempty"`
			TokenLabel string         `json:"tokenLabel,omitempty"`
			Tasks      []*TaskRunMeta `json:"tasks,omitempty"`
		}
		var endTime *time.Time
		if !meta.EndTime.IsZero() {
			value := meta.EndTime
			endTime = &value
		}
		s.writeJSON(w, http.StatusAccepted, runStartResponse{
			RunID:      meta.RunID,
			RunKey:     meta.RunKey,
			Branch:     meta.Branch,
			TaskLabel:  meta.TaskLabel,
			RunNumber:  meta.RunNumber,
			Status:     meta.Status,
			StartTime:  meta.StartTime,
			EndTime:    endTime,
			ExitCode:   meta.ExitCode,
			User:       meta.User,
			TokenLabel: meta.TokenLabel,
			Tasks:      meta.Tasks,
		})
	default:
		s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (s *Server) runIDFromRequest(r *http.Request) (string, error) {
	runID := strings.TrimSpace(r.PathValue("runId"))
	if runID == "" {
		return "", fmt.Errorf("invalid run id")
	}
	return runID, nil
}

func (s *Server) handleRunDetail(w http.ResponseWriter, r *http.Request) {
	runID, err := s.runIDFromRequest(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if active := s.manager.GetActiveRunByID(runID); active != nil {
		meta := *active.Meta
		meta.Tasks = collectTaskRuns(active)
		s.writeJSON(w, http.StatusOK, &meta)
		return
	}
	meta, err := s.manager.history.ReadMeta(runID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, meta)
}

func (s *Server) handleRunLog(w http.ResponseWriter, r *http.Request) {
	runID, err := s.runIDFromRequest(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	ServeLogSSE(w, r, runID, s.manager)
}

func (s *Server) handleMetricsStream(w http.ResponseWriter, r *http.Request) {
	if s.metrics == nil {
		s.writeError(w, http.StatusNotFound, "metrics disabled")
		return
	}
	s.metrics.ServeHTTP(w, r)
}

func (s *Server) handleRunArtifacts(w http.ResponseWriter, r *http.Request) {
	runID, err := s.runIDFromRequest(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	meta, err := s.manager.history.ReadMeta(runID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	type artifactItem struct {
		Source      string `json:"source"`
		Path        string `json:"path"`
		DownloadURL string `json:"downloadUrl"`
		Format      string `json:"format,omitempty"`
		SizeBytes   int64  `json:"sizeBytes"`
		CreatedAt   string `json:"createdAt"`
		HashSHA256  string `json:"hashSha256"`
	}
	items := make([]artifactItem, 0, len(meta.Artifacts))
	for _, artifact := range meta.Artifacts {
		info, err := s.manager.history.StatArtifactFile(runID, artifact.Dest)
		if err != nil {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		data, err := s.manager.history.ReadArtifactFile(runID, artifact.Dest)
		if err != nil {
			s.writeError(w, http.StatusNotFound, err.Error())
			return
		}
		sum := sha256.Sum256(data)
		items = append(items, artifactItem{
			Source:      artifact.Source,
			Path:        artifact.Dest,
			DownloadURL: fmt.Sprintf("/api/runs/%s/artifacts/%s", runID, encodePathSegments(artifact.Dest)),
			Format:      artifact.Format,
			SizeBytes:   info.SizeBytes,
			CreatedAt:   info.CreatedAt.UTC().Format(time.RFC3339),
			HashSHA256:  hex.EncodeToString(sum[:]),
		})
	}
	s.writeJSON(w, http.StatusOK, items)
}

func (s *Server) handleRunArtifactFile(w http.ResponseWriter, r *http.Request) {
	runID, err := s.runIDFromRequest(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filePath := r.PathValue("path")
	data, err := s.manager.history.ReadArtifactFile(runID, filePath)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(filePath))
	_, _ = w.Write(data)
}

func (s *Server) handleRunWorktreeList(w http.ResponseWriter, r *http.Request) {
	runID, err := s.runIDFromRequest(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	files, err := s.manager.history.ListWorktreeFiles(runID)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, files)
}

func (s *Server) handleRunWorktreeFile(w http.ResponseWriter, r *http.Request) {
	runID, err := s.runIDFromRequest(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	filePath := r.PathValue("path")
	data, err := s.manager.history.ReadWorktreeFile(runID, filePath)
	if err != nil {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.Header().Set("Content-Type", contentTypeFor(filePath))
	_, _ = w.Write(data)
}

func (s *Server) handleCleanup(w http.ResponseWriter, r *http.Request) {
	if err := s.manager.history.PruneWorktrees(
		s.config.Storage.Worktree.KeepOnSuccess,
		s.config.Storage.Worktree.KeepOnFailure,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func contentTypeFor(filePath string) string {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".json":
		return "application/json"
	case ".yaml", ".yml":
		return "application/yaml"
	case ".go":
		return "text/x-go"
	case ".ts":
		return "application/typescript"
	case ".js":
		return "application/javascript"
	case ".md":
		return "text/markdown"
	default:
		return "text/plain; charset=utf-8"
	}
}

func newMetricsService(config *uiconfig.UIConfig, manager *TaskManager) *MetricsService {
	if config == nil || manager == nil || manager.history == nil {
		return nil
	}
	return NewMetricsService(config.Metrics, manager.history.historyDir)
}

func encodePathSegments(raw string) string {
	parts := strings.Split(filepath.ToSlash(raw), "/")
	for index, part := range parts {
		parts[index] = url.PathEscape(part)
	}
	return strings.Join(parts, "/")
}

func taskGroupName(raw json.RawMessage) string {
	group, ok := tasks.ParseTaskGroup(raw)
	if ok {
		return group.Kind
	}
	return ""
}

var inputReferencePattern = regexp.MustCompile(`\$\{input:([^}]+)}`)

func referencedInputs(task tasks.Task, inputs []tasks.Input) []tasks.Input {
	if len(inputs) == 0 {
		return nil
	}
	used := make(map[string]bool)
	collectInputRefs(task.Command.Value, used)
	for _, arg := range task.Args {
		collectInputRefs(arg.Value, used)
	}
	if task.Options != nil {
		collectInputRefs(task.Options.CWD, used)
		for _, value := range task.Options.Env {
			collectInputRefs(value, used)
		}
		if task.Options.Shell != nil {
			collectInputRefs(task.Options.Shell.Executable, used)
			for _, arg := range task.Options.Shell.Args {
				collectInputRefs(arg, used)
			}
		}
	}
	result := make([]tasks.Input, 0, len(used))
	for _, input := range inputs {
		if used[input.ID] {
			result = append(result, input)
		}
	}
	return result
}

func collectInputRefs(value string, used map[string]bool) {
	if value == "" {
		return
	}
	for _, match := range inputReferencePattern.FindAllStringSubmatch(value, -1) {
		if len(match) == 2 {
			used[match[1]] = true
		}
	}
}
