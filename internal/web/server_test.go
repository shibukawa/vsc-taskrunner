package web

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/tasks"
	"vsc-taskrunner/internal/uiconfig"
)

type serverTestRepo struct {
	listBranches        func(ctx context.Context) ([]git.Branch, error)
	readBranchMetadata  func(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error)
	readTasksJSON       func(ctx context.Context, branch string) ([]byte, error)
	prepareRunWorkspace func(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error)
	refresh             func(ctx context.Context) error
	maintenance         func(ctx context.Context) error
	basePath            string
}

func (r *serverTestRepo) BasePath() string { return r.basePath }
func (r *serverTestRepo) ListBranches(ctx context.Context) ([]git.Branch, error) {
	if r.listBranches != nil {
		return r.listBranches(ctx)
	}
	return nil, nil
}
func (r *serverTestRepo) Refresh(ctx context.Context) error {
	if r.refresh != nil {
		return r.refresh(ctx)
	}
	return nil
}
func (r *serverTestRepo) FetchBranch(ctx context.Context, branch string) error { return nil }
func (r *serverTestRepo) ReadBranchMetadata(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
	if r.readBranchMetadata != nil {
		return r.readBranchMetadata(ctx, branch, filePath)
	}
	return "", time.Time{}, nil, fmt.Errorf("unexpected ReadBranchMetadata call")
}
func (r *serverTestRepo) ReadTasksJSON(ctx context.Context, branch string) ([]byte, error) {
	if r.readTasksJSON != nil {
		return r.readTasksJSON(ctx, branch)
	}
	return nil, fmt.Errorf("unexpected ReadTasksJSON call")
}
func (r *serverTestRepo) PrepareRunWorkspace(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
	if r.prepareRunWorkspace != nil {
		return r.prepareRunWorkspace(ctx, branch, workspacePath, sparsePaths)
	}
	return workspacePath, nil
}
func (r *serverTestRepo) CleanupWorkspace(path string) error { return nil }
func (r *serverTestRepo) Maintenance(ctx context.Context) error {
	if r.maintenance != nil {
		return r.maintenance(ctx)
	}
	return nil
}

func serverAllowedTaskSpecs(specs ...uiconfig.AllowedTaskSpec) uiconfig.AllowedTaskSpecs {
	return uiconfig.AllowedTaskSpecs(specs)
}

func TestReferencedInputsReturnsOnlyUsedInputs(t *testing.T) {
	t.Parallel()

	task := tasks.Task{
		Label:   "build",
		Command: tasks.TokenValue{Value: "echo ${input:name}", Set: true},
		Args: []tasks.TokenValue{
			{Value: "${input:mode}", Set: true},
			{Value: "static", Set: true},
		},
	}
	inputs := []tasks.Input{
		{ID: "name", Type: "promptString"},
		{ID: "mode", Type: "pickString"},
		{ID: "unused", Type: "promptString"},
	}

	got := referencedInputs(task, inputs)
	if len(got) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(got))
	}
	if got[0].ID != "name" || got[1].ID != "mode" {
		t.Fatalf("unexpected inputs: %+v", got)
	}
}

func TestBranchTasksRouteAcceptsSlashInBranchName(t *testing.T) {
	t.Parallel()

	server := NewServer(nil, &uiconfig.UIConfig{Repository: uiconfig.RepositoryConfig{Source: "/tmp/repo"}}, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/git/branches/feature%2Fdemo/tasks", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("expected branch path with slash to reach handler logic instead of 404 route parsing")
	}
}

func TestHandleBranchesPreloadsTasksAndCommitDate(t *testing.T) {
	t.Parallel()

	repo := &serverTestRepo{
		basePath: t.TempDir(),
		listBranches: func(ctx context.Context) ([]git.Branch, error) {
			return []git.Branch{
				{FullRef: "refs/heads/main", ShortName: "main", CommitHash: "oldhash"},
				{FullRef: "refs/heads/dev", ShortName: "dev", CommitHash: "devhash"},
			}, nil
		},
		readBranchMetadata: func(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
			return "newhash-" + branch, time.Unix(100, 0).UTC(), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), nil
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Branches = []string{"main"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{
			Artifacts:        []uiconfig.ArtifactRuleConfig{{Path: "dist/*"}},
			PreRunTasks:      []uiconfig.PreRunTaskConfig{{Command: "echo", Args: []string{"setup"}, CWD: "/tmp/run"}},
			WorktreeDisabled: true,
		}},
		uiconfig.AllowedTaskSpec{Pattern: "lint", Config: uiconfig.TaskUIConfig{}},
	)
	server := NewServer(repo, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/git/branches", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var items []struct {
		ShortName  string `json:"shortName"`
		CommitHash string `json:"commitHash"`
		CommitDate string `json:"commitDate"`
		Tasks      []struct {
			Label              string   `json:"label"`
			Artifact           bool     `json:"artifact"`
			WorktreeDisabled   bool     `json:"worktreeDisabled"`
			TaskFilePath       string   `json:"taskFilePath"`
			ResolvedTaskLabels []string `json:"resolvedTaskLabels"`
			PreRunTasks        []struct {
				Command string   `json:"command"`
				Args    []string `json:"args"`
				CWD     string   `json:"cwd"`
			} `json:"preRunTasks"`
			Artifacts []struct {
				Path         string `json:"path"`
				Format       string `json:"format"`
				NameTemplate string `json:"nameTemplate"`
			} `json:"artifacts"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ShortName != "main" {
		t.Fatalf("unexpected branches: %+v", items)
	}
	if items[0].CommitDate == "" {
		t.Fatalf("expected commitDate, got %+v", items[0])
	}
	if len(items[0].Tasks) != 1 || items[0].Tasks[0].Label != "build" {
		t.Fatalf("unexpected tasks: %+v", items[0].Tasks)
	}
	if !items[0].Tasks[0].Artifact || !items[0].Tasks[0].WorktreeDisabled {
		t.Fatalf("expected task config flags in response, got %+v", items[0].Tasks[0])
	}
	if items[0].Tasks[0].TaskFilePath != ".vscode/tasks.json" {
		t.Fatalf("taskFilePath = %q, want .vscode/tasks.json", items[0].Tasks[0].TaskFilePath)
	}
	if got, want := strings.Join(items[0].Tasks[0].ResolvedTaskLabels, ","), "build"; got != want {
		t.Fatalf("resolvedTaskLabels = %q, want %q", got, want)
	}
	if len(items[0].Tasks[0].PreRunTasks) != 1 || items[0].Tasks[0].PreRunTasks[0].Command != "echo" {
		t.Fatalf("unexpected preRunTasks: %+v", items[0].Tasks[0].PreRunTasks)
	}
	if len(items[0].Tasks[0].Artifacts) != 1 {
		t.Fatalf("unexpected artifacts: %+v", items[0].Tasks[0].Artifacts)
	}
	if items[0].Tasks[0].Artifacts[0].Format != "zip" || items[0].Tasks[0].Artifacts[0].NameTemplate != uiconfig.DefaultArtifactArchive {
		t.Fatalf("artifact defaults not applied: %+v", items[0].Tasks[0].Artifacts[0])
	}
}

func TestHandleBranchTasksReturnsResolvedTaskLabelsForDependencies(t *testing.T) {
	t.Parallel()

	repo := &serverTestRepo{
		basePath: t.TempDir(),
		readBranchMetadata: func(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
			return "newhash-main", time.Unix(100, 0).UTC(), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"process","command":"echo","dependsOn":["lint","prepare"]},{"label":"prepare","type":"process","command":"echo"},{"label":"lint","type":"process","command":"echo"},{"label":"other","type":"process","command":"echo"}]}`), nil
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Branches = []string{"main"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{}},
		uiconfig.AllowedTaskSpec{Pattern: "prepare", Config: uiconfig.TaskUIConfig{}},
		uiconfig.AllowedTaskSpec{Pattern: "lint", Config: uiconfig.TaskUIConfig{}},
		uiconfig.AllowedTaskSpec{Pattern: "other", Config: uiconfig.TaskUIConfig{}},
	)
	server := NewServer(repo, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/git/branches/main/tasks", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var items []struct {
		Label              string   `json:"label"`
		ResolvedTaskLabels []string `json:"resolvedTaskLabels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	byLabel := make(map[string][]string, len(items))
	for _, item := range items {
		byLabel[item.Label] = item.ResolvedTaskLabels
	}
	if got, want := strings.Join(byLabel["build"], ","), "build,lint,prepare"; got != want {
		t.Fatalf("build resolvedTaskLabels = %q, want %q", got, want)
	}
	if got, want := strings.Join(byLabel["other"], ","), "other"; got != want {
		t.Fatalf("other resolvedTaskLabels = %q, want %q", got, want)
	}
}

func TestHandleBranchTasksUsesPreloadedCache(t *testing.T) {
	t.Parallel()

	repo := &serverTestRepo{
		basePath: t.TempDir(),
		listBranches: func(ctx context.Context) ([]git.Branch, error) {
			return []git.Branch{{FullRef: "refs/heads/main", ShortName: "main", CommitHash: "oldhash"}}, nil
		},
		readBranchMetadata: func(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
			return "newhash-main", time.Unix(100, 0).UTC(), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), nil
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Branches = []string{"main"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{}},
	)
	server := NewServer(repo, cfg, nil, nil)

	for _, path := range []string{"/api/git/branches", "/api/git/branches/main/tasks"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		server.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

func TestWarmBranchMetadataPreloadsBranchCache(t *testing.T) {
	t.Parallel()

	calls := 0
	repo := &serverTestRepo{
		basePath: t.TempDir(),
		listBranches: func(ctx context.Context) ([]git.Branch, error) {
			return []git.Branch{{FullRef: "refs/heads/main", ShortName: "main", CommitHash: "oldhash"}}, nil
		},
		readBranchMetadata: func(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
			calls++
			return "newhash-main", time.Unix(100, 0).UTC(), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), nil
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Branches = []string{"main"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{}},
	)
	server := NewServer(repo, cfg, nil, nil)

	if err := server.WarmBranchMetadata(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/git/branches", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("readBranchMetadata calls = %d, want 1", calls)
	}
}

func TestHandleBranchesKeepsHealthyBranchesWhenOnePreloadFails(t *testing.T) {
	t.Parallel()

	repo := &serverTestRepo{
		basePath: t.TempDir(),
		listBranches: func(ctx context.Context) ([]git.Branch, error) {
			return []git.Branch{
				{FullRef: "refs/heads/dev", ShortName: "dev", CommitHash: "devhash"},
				{FullRef: "refs/heads/main", ShortName: "main", CommitHash: "mainhash"},
			}, nil
		},
		readBranchMetadata: func(ctx context.Context, branch, filePath string) (string, time.Time, []byte, error) {
			if branch == "dev" {
				return "", time.Time{}, nil, fmt.Errorf("boom")
			}
			return "newhash-main", time.Unix(100, 0).UTC(), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), nil
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Branches = []string{"main", "dev"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{}},
	)
	server := NewServer(repo, cfg, nil, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/git/branches", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var items []struct {
		ShortName  string `json:"shortName"`
		CommitDate string `json:"commitDate"`
		LoadError  string `json:"loadError"`
		Tasks      []struct {
			Label string `json:"label"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("unexpected branches: %+v", items)
	}
	if items[0].ShortName != "dev" || items[0].LoadError == "" {
		t.Fatalf("expected dev preload failure, got %+v", items[0])
	}
	if items[1].ShortName != "main" || items[1].CommitDate == "" || len(items[1].Tasks) != 1 {
		t.Fatalf("expected main preload success, got %+v", items[1])
	}
}

func TestHandleMeExposesCanRun(t *testing.T) {
	t.Parallel()

	cfg := &uiconfig.UIConfig{
		Auth: uiconfig.AuthConfig{
			AllowUsers: []uiconfig.UserAccessRule{{Claim: "groups", Value: "runner"}},
			AdminUsers: []uiconfig.UserAccessRule{{Claim: "groups", Value: "admin"}},
		},
	}
	server := NewServer(nil, cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, map[string]interface{}{
		"email":  "alice@example.com",
		"groups": []interface{}{"runner", "admin"},
	}))
	rec := httptest.NewRecorder()

	server.handleMe(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var body struct {
		Authenticated bool `json:"authenticated"`
		CanRun        bool `json:"canRun"`
		IsAdmin       bool `json:"isAdmin"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.Authenticated {
		t.Fatal("expected authenticated")
	}
	if !body.CanRun {
		t.Fatal("expected allowed user to have canRun=true")
	}
	if !body.IsAdmin {
		t.Fatal("expected admin user to have isAdmin=true")
	}
}

func TestHandleSettingsReturnsResolvedSummaryForAdmin(t *testing.T) {
	t.Setenv("REPO_ACCESS_TOKEN", "present")
	repo := &serverTestRepo{basePath: filepath.Join(t.TempDir(), "cache.git")}
	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "https://github.com/example/repo.git"
	cfg.Repository.CachePath = ".cache/repo.git"
	cfg.Repository.Auth = uiconfig.RepositoryAuthConfig{
		Type:     "envToken",
		TokenEnv: "REPO_ACCESS_TOKEN",
	}
	cfg.Auth.AdminUsers = []uiconfig.UserAccessRule{{Claim: "groups", Value: "admin"}}
	cfg.Auth.NoAuth = false
	cfg.Auth.OIDCIssuer = "https://issuer.example.com"
	cfg.Auth.APITokens.Enabled = true
	cfg.Auth.APITokens.Store.Backend = "local"
	cfg.Auth.APITokens.Store.LocalPath = ".runtask/api-tokens.json"
	cfg.Storage.Backend = "local"
	cfg.Storage.HistoryDir = ".runtask/history"
	cfg.Storage.HistoryKeepCount = 42
	cfg.Execution.MaxParallelRuns = 7
	cfg.Metrics.CPUInterval = 2
	cfg.Metrics.MemoryInterval = 3
	cfg.Metrics.StorageInterval = 4
	cfg.Metrics.MemoryHistoryWindow = 120
	historyDir := filepath.Join(t.TempDir(), "workspace", ".runtask", "history")
	history, err := NewHistoryStore(historyDir)
	if err != nil {
		t.Fatal(err)
	}
	auth := &Authenticator{}
	auth.SetTokenService(NewAPITokenService(cfg.Auth.APITokens, NewLocalAPITokenStore(uiconfig.ResolveAPITokenLocalPath(historyDir, cfg.Auth.APITokens.Store.LocalPath))))
	server := NewServer(repo, cfg, NewTaskManager(repo, cfg, history), auth)

	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, map[string]interface{}{
		"groups": []interface{}{"admin"},
	}))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body settingsSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Repository.Source != cfg.Repository.Source {
		t.Fatalf("repository.source = %q, want %q", body.Repository.Source, cfg.Repository.Source)
	}
	if body.Repository.CachePath != filepath.Join(filepath.Dir(filepath.Dir(historyDir)), cfg.Repository.CachePath) {
		t.Fatalf("repository.cachePath = %q", body.Repository.CachePath)
	}
	if !body.Repository.AccessTokenConfigured {
		t.Fatal("expected repository access token to be marked configured")
	}
	if body.Auth.OIDCIssuer != cfg.Auth.OIDCIssuer {
		t.Fatalf("oidcIssuer = %q", body.Auth.OIDCIssuer)
	}
	if !body.Auth.APITokensEnabled {
		t.Fatal("expected apiTokensEnabled")
	}
	if body.Auth.APITokenStore.LocalPath != uiconfig.ResolveAPITokenLocalPath(historyDir, cfg.Auth.APITokens.Store.LocalPath) {
		t.Fatalf("apiTokenStore.localPath = %q", body.Auth.APITokenStore.LocalPath)
	}
	if body.Storage.HistoryDir != historyDir {
		t.Fatalf("storage.historyDir = %q, want %q", body.Storage.HistoryDir, historyDir)
	}
	if body.Execution.MaxParallelRuns != 7 {
		t.Fatalf("maxParallelRuns = %d", body.Execution.MaxParallelRuns)
	}
	if body.Metrics.CPUInterval != 2 || body.Metrics.MemoryInterval != 3 || body.Metrics.StorageInterval != 4 || body.Metrics.MemoryHistoryWindow != 120 {
		t.Fatalf("unexpected metrics summary: %+v", body.Metrics)
	}
	if strings.Contains(rec.Body.String(), "present") {
		t.Fatalf("settings response leaked token value: %s", rec.Body.String())
	}
}

func TestHandleSettingsRejectsNonAdmin(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Auth.AdminUsers = []uiconfig.UserAccessRule{{Claim: "groups", Value: "admin"}}
	server := NewServer(nil, cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, map[string]interface{}{
		"groups": []interface{}{"runner"},
	}))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSettingsReturnsForbiddenInNoAuthMode(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Auth.NoAuth = true
	server := NewServer(nil, cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleSettingsReturnsObjectStorageSummary(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Auth.AdminUsers = []uiconfig.UserAccessRule{{Claim: "groups", Value: "admin"}}
	cfg.Auth.APITokens.Store.Backend = "object"
	cfg.Auth.APITokens.Store.Object = uiconfig.ObjectStorageConfig{
		Endpoint: "https://s3.example.com",
		Bucket:   "token-bucket",
		Region:   "ap-northeast-1",
		Prefix:   "tokens",
	}
	cfg.Storage.Backend = "object"
	cfg.Storage.Object = uiconfig.ObjectStorageConfig{
		Endpoint: "https://storage.example.com",
		Bucket:   "history-bucket",
		Region:   "us-east-1",
		Prefix:   "runs",
	}
	server := NewServer(nil, cfg, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
	req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, map[string]interface{}{
		"groups": []interface{}{"admin"},
	}))
	rec := httptest.NewRecorder()

	server.handleSettings(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body settingsSummaryResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Auth.APITokenStore.Endpoint != "https://s3.example.com" || body.Auth.APITokenStore.Bucket != "token-bucket" {
		t.Fatalf("unexpected token object store summary: %+v", body.Auth.APITokenStore)
	}
	if body.Storage.Object.Endpoint != "https://storage.example.com" || body.Storage.Object.Bucket != "history-bucket" {
		t.Fatalf("unexpected storage object summary: %+v", body.Storage.Object)
	}
}

func TestHandleRunsPostReturnsPreinitializedTasks(t *testing.T) {
	t.Parallel()

	repo := &serverTestRepo{
		basePath: t.TempDir(),
		readTasksJSON: func(ctx context.Context, branch string) ([]byte, error) {
			return []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"process","command":"echo","dependsOn":["lint"]},{"label":"lint","type":"process","command":"echo"}]}`), nil
		},
		prepareRunWorkspace: func(ctx context.Context, branch, workspacePath string, sparsePaths []string) (string, error) {
			if err := os.MkdirAll(filepath.Join(workspacePath, ".vscode"), 0o755); err != nil {
				return "", err
			}
			if err := os.WriteFile(filepath.Join(workspacePath, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"process","command":"sh","args":["-c","printf ok"],"dependsOn":["lint"]},{"label":"lint","type":"process","command":"sh","args":["-c","printf lint"]}]}`), 0o644); err != nil {
				return "", err
			}
			return workspacePath, nil
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Branches = []string{"main"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{PreRunTasks: []uiconfig.PreRunTaskConfig{{Command: "echo"}}}},
		uiconfig.AllowedTaskSpec{Pattern: "lint", Config: uiconfig.TaskUIConfig{}},
	)
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(repo, cfg, NewTaskManager(repo, cfg, history), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"branch":"main","taskLabel":"build"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		RunID     string `json:"runId"`
		RunNumber int    `json:"runNumber"`
		Tasks     []struct {
			Label     string   `json:"label"`
			Status    string   `json:"status"`
			DependsOn []string `json:"dependsOn"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.RunID == "" || body.RunNumber == 0 {
		t.Fatalf("unexpected response: %+v", body)
	}
	if len(body.Tasks) != 4 {
		t.Fatalf("tasks = %+v, want 4 entries", body.Tasks)
	}
	byLabel := make(map[string]struct {
		Status    string
		DependsOn []string
	}, len(body.Tasks))
	for _, task := range body.Tasks {
		byLabel[task.Label] = struct {
			Status    string
			DependsOn []string
		}{Status: task.Status, DependsOn: task.DependsOn}
	}
	for _, label := range []string{workspacePrepareTaskLabel, "pre-run #1", "lint", "build"} {
		task, ok := byLabel[label]
		if !ok {
			t.Fatalf("missing task %q in %+v", label, body.Tasks)
		}
		if task.Status != "pending" {
			t.Fatalf("task %q status = %q, want pending", label, task.Status)
		}
	}
	if got := byLabel["lint"].DependsOn; len(got) != 1 || got[0] != "pre-run #1" {
		t.Fatalf("lint dependsOn = %#v, want pre-run #1", got)
	}
	active := server.manager.GetActiveRunByID(body.RunID)
	if active == nil {
		t.Fatalf("expected active run %q", body.RunID)
	}
	<-active.done
}

func TestHandleRunsPostFailsWhenRunGraphCannotBeInitialized(t *testing.T) {
	t.Parallel()

	repo := &serverTestRepo{
		basePath: t.TempDir(),
		readTasksJSON: func(ctx context.Context, branch string) ([]byte, error) {
			return nil, fmt.Errorf("boom")
		},
	}
	cfg := uiconfig.DefaultConfig()
	cfg.Branches = []string{"main"}
	cfg.Tasks = serverAllowedTaskSpecs(
		uiconfig.AllowedTaskSpec{Pattern: "build", Config: uiconfig.TaskUIConfig{}},
	)
	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	server := NewServer(repo, cfg, NewTaskManager(repo, cfg, history), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/runs", strings.NewReader(`{"branch":"main","taskLabel":"build"}`))
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	metas, err := history.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 0 {
		t.Fatalf("expected no visible runs after init failure, got %+v", metas)
	}
}

func TestHandleRunArtifacts(t *testing.T) {
	t.Parallel()

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta := &RunMeta{
		RunID:        "run-1",
		RunKey:       "run-1",
		Branch:       "main",
		TaskLabel:    "build",
		RunNumber:    1,
		Status:       RunStatusSuccess,
		HasArtifacts: true,
		Artifacts: []ArtifactRef{
			{Source: "bin/app", Dest: "bin/app", Format: "file"},
		},
	}
	if err := history.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(history.ArtifactDir(meta.RunID), "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(history.ArtifactDir(meta.RunID), "bin", "app"), []byte("artifact"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewTaskManager(nil, uiconfig.DefaultConfig(), history)
	server := NewServer(nil, uiconfig.DefaultConfig(), manager, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/artifacts", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var items []struct {
		Path       string `json:"path"`
		Download   string `json:"downloadUrl"`
		CreatedAt  string `json:"createdAt"`
		HashSHA256 string `json:"hashSha256"`
		SizeBytes  int64  `json:"sizeBytes"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Path != "bin/app" {
		t.Fatalf("unexpected response: %+v", items)
	}
	if items[0].Download != "/api/runs/run-1/artifacts/bin/app" {
		t.Fatalf("downloadUrl = %q", items[0].Download)
	}
	if items[0].SizeBytes != int64(len("artifact")) {
		t.Fatalf("sizeBytes = %d", items[0].SizeBytes)
	}
	wantHash := sha256.Sum256([]byte("artifact"))
	if items[0].HashSHA256 != hex.EncodeToString(wantHash[:]) {
		t.Fatalf("hashSha256 = %q", items[0].HashSHA256)
	}
	if _, err := time.Parse(time.RFC3339, items[0].CreatedAt); err != nil {
		t.Fatalf("createdAt parse error: %v", err)
	}
}

// fakeRunStore wraps a LocalRunStore and overrides PresignWorktreeURL for testing.
type fakeRunStore struct {
	inner      *LocalRunStore
	presignURL string
}

func (f *fakeRunStore) RunDir(runID string) string  { return f.inner.RunDir(runID) }
func (f *fakeRunStore) LogPath(runID string) string { return f.inner.LogPath(runID) }
func (f *fakeRunStore) TaskLogPath(runID, taskLabel string) string {
	return f.inner.TaskLogPath(runID, taskLabel)
}
func (f *fakeRunStore) WorktreePath(runID string) string { return f.inner.WorktreePath(runID) }
func (f *fakeRunStore) ArtifactDir(runID string) string  { return f.inner.ArtifactDir(runID) }
func (f *fakeRunStore) MetaPath(runID string) string     { return f.inner.MetaPath(runID) }
func (f *fakeRunStore) WriteMeta(meta *RunMeta) error    { return f.inner.WriteMeta(meta) }
func (f *fakeRunStore) ListMetas(ctx context.Context) ([]*RunMeta, error) {
	return f.inner.ListMetas(ctx)
}
func (f *fakeRunStore) ReadMeta(runID string) (*RunMeta, error) { return f.inner.ReadMeta(runID) }
func (f *fakeRunStore) ReadLog(runID string) ([]byte, error)    { return f.inner.ReadLog(runID) }
func (f *fakeRunStore) ReadTaskLog(runID, taskLabel string) ([]byte, error) {
	return f.inner.ReadTaskLog(runID, taskLabel)
}
func (f *fakeRunStore) TailLog(runID string, byteOffset int64) ([]byte, error) {
	return f.inner.TailLog(runID, byteOffset)
}
func (f *fakeRunStore) ListWorktreeFiles(runID string) ([]string, error) {
	return f.inner.ListWorktreeFiles(runID)
}
func (f *fakeRunStore) ReadWorktreeFile(runID, filePath string) ([]byte, error) {
	return f.inner.ReadWorktreeFile(runID, filePath)
}
func (f *fakeRunStore) ListArtifactFiles(runID string) ([]string, error) {
	return f.inner.ListArtifactFiles(runID)
}
func (f *fakeRunStore) StatArtifactFile(runID, filePath string) (ArtifactFileInfo, error) {
	return f.inner.StatArtifactFile(runID, filePath)
}
func (f *fakeRunStore) ReadArtifactFile(runID, filePath string) ([]byte, error) {
	return f.inner.ReadArtifactFile(runID, filePath)
}
func (f *fakeRunStore) ReadWorktreeZip(runID string) ([]byte, error) {
	return f.inner.ReadWorktreeZip(runID)
}
func (f *fakeRunStore) PresignWorktreeURL(runID string, expiry time.Duration) (string, error) {
	if f.presignURL == "" {
		return "", fmt.Errorf("not supported")
	}
	return f.presignURL, nil
}
func (f *fakeRunStore) DeleteRun(runID string) error   { return f.inner.DeleteRun(runID) }
func (f *fakeRunStore) FinalizeRun(runID string) error { return f.inner.FinalizeRun(runID) }

func TestHandleRunWorktreeZip_Local(t *testing.T) {
	t.Parallel()

	historyDir := t.TempDir()
	history, err := NewHistoryStore(historyDir)
	if err != nil {
		t.Fatal(err)
	}

	runID := "local-run-zip"
	meta := &RunMeta{
		RunID:        runID,
		RunKey:       runID,
		Branch:       "main",
		TaskLabel:    "build",
		RunNumber:    1,
		Status:       RunStatusSuccess,
		WorktreeKept: true,
	}
	if err := history.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	// create a worktree file
	worktreeRoot := filepath.Join(history.WorktreePath(runID))
	if err := os.MkdirAll(worktreeRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worktreeRoot, "hello.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	manager := NewTaskManager(nil, uiconfig.DefaultConfig(), history)
	server := NewServer(nil, uiconfig.DefaultConfig(), manager, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/worktree.zip", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("Content-Type = %q", ct)
	}
	// check zip header
	data := rec.Body.Bytes()
	if len(data) < 4 || !bytes.Equal(data[:4], []byte{'P', 'K', 0x03, 0x04}) {
		t.Fatalf("response not a zip archive, len=%d", len(data))
	}
	// try to open zip
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range r.File {
		if f.Name == "hello.txt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("zip did not contain hello.txt")
	}
}

func TestHandleRunWorktreePresign_Fake(t *testing.T) {
	t.Parallel()

	historyDir := t.TempDir()
	// create a real local run store and wrap
	local := NewLocalRunStore(historyDir)
	fake := &fakeRunStore{inner: local, presignURL: "https://example.com/download"}

	// create history store with fake run store
	indexStore := NewLocalIndexStore(historyDir)
	history, err := NewHistoryStoreWithStores(historyDir, indexStore, fake)
	if err != nil {
		t.Fatal(err)
	}

	runID := "presign-run"
	meta := &RunMeta{
		RunID:        runID,
		RunKey:       runID,
		Branch:       "main",
		TaskLabel:    "build",
		RunNumber:    1,
		Status:       RunStatusSuccess,
		WorktreeKept: true,
	}
	if err := history.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}

	manager := NewTaskManager(nil, uiconfig.DefaultConfig(), history)
	server := NewServer(nil, uiconfig.DefaultConfig(), manager, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/"+runID+"/worktree/presign", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if got := body["url"]; got != "https://example.com/download" {
		t.Fatalf("unexpected presign url %q", got)
	}
}

func TestHandleRunDetailOmitsInputValues(t *testing.T) {
	t.Parallel()

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta := &RunMeta{
		RunID:       "run-input-redacted",
		RunKey:      "run-input-redacted",
		Branch:      "main",
		TaskLabel:   "deploy",
		RunNumber:   1,
		Status:      RunStatusSuccess,
		InputValues: map[string]string{"client_secret": "hidden"},
	}
	if err := history.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	manager := NewTaskManager(nil, uiconfig.DefaultConfig(), history)
	server := NewServer(nil, uiconfig.DefaultConfig(), manager, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-input-redacted", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "inputValues") || strings.Contains(rec.Body.String(), "hidden") {
		t.Fatalf("run detail leaked input values: %s", rec.Body.String())
	}
}

func TestEphemeralEmulationResetsCacheAfterIdle(t *testing.T) {
	t.Setenv("RUNTASK_EPHEMERAL_EMULATION_IDLE", "1m")
	var logBuffer bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&logBuffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})
	cachePath := filepath.Join(t.TempDir(), "cache.git")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cachePath, "marker"), []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	maintenanceCalls := 0
	repo := &serverTestRepo{
		basePath: cachePath,
		maintenance: func(ctx context.Context) error {
			maintenanceCalls++
			return os.MkdirAll(cachePath, 0o755)
		},
	}
	server := NewServer(repo, uiconfig.DefaultConfig(), nil, nil)
	server.lastRequestAt = time.Now().Add(-2 * time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if maintenanceCalls != 1 {
		t.Fatalf("maintenanceCalls = %d, want 1", maintenanceCalls)
	}
	if _, err := os.Stat(filepath.Join(cachePath, "marker")); !os.IsNotExist(err) {
		t.Fatalf("expected marker to be removed, got err=%v", err)
	}
	if !strings.Contains(logBuffer.String(), "runtask ephemeral emulation deleting repository cache=") {
		t.Fatalf("expected deletion log, got %q", logBuffer.String())
	}
}

func TestEphemeralEmulationSkipsResetWithActiveRuns(t *testing.T) {
	t.Setenv("RUNTASK_EPHEMERAL_EMULATION_IDLE", "1m")
	cachePath := filepath.Join(t.TempDir(), "cache.git")
	if err := os.MkdirAll(cachePath, 0o755); err != nil {
		t.Fatal(err)
	}
	repo := &serverTestRepo{basePath: cachePath}
	manager := &TaskManager{activeRuns: map[string]*ActiveRun{"run-1": {}}}
	server := NewServer(repo, uiconfig.DefaultConfig(), manager, nil)
	server.lastRequestAt = time.Now().Add(-2 * time.Minute)

	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("expected cachePath preserved: %v", err)
	}
}

func TestHandleRunArtifactsEmpty(t *testing.T) {
	t.Parallel()

	history, err := NewHistoryStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	meta := &RunMeta{
		RunID:     "run-1",
		RunKey:    "run-1",
		Branch:    "main",
		TaskLabel: "build",
		RunNumber: 1,
		Status:    RunStatusSuccess,
	}
	if err := history.WriteMeta(meta); err != nil {
		t.Fatal(err)
	}
	manager := NewTaskManager(nil, uiconfig.DefaultConfig(), history)
	server := NewServer(nil, uiconfig.DefaultConfig(), manager, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/runs/run-1/artifacts", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var items []struct{}
	if err := json.Unmarshal(rec.Body.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("unexpected response: %+v", items)
	}
}

func TestHandleMetricsDisabled(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Metrics.Enabled = false
	server := NewServer(nil, cfg, NewTaskManager(nil, cfg, &HistoryStore{historyDir: t.TempDir()}), nil)

	req := httptest.NewRequest(http.MethodGet, "/api/metrics/stream", nil)
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}
