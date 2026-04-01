package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"vsc-taskrunner/internal/uiconfig"
)

type settingsSummaryResponse struct {
	RuntimeMode RuntimeMode               `json:"runtimeMode"`
	Repository  settingsRepositorySummary `json:"repository"`
	Auth        settingsAuthSummary       `json:"auth"`
	Execution   settingsExecutionSummary  `json:"execution"`
	Metrics     settingsMetricsSummary    `json:"metrics"`
	Storage     settingsStorageSummary    `json:"storage"`
}

type settingsRepositorySummary struct {
	Source                string `json:"source"`
	CachePath             string `json:"cachePath"`
	AccessTokenConfigured bool   `json:"accessTokenConfigured"`
}

type settingsAuthSummary struct {
	NoAuth           bool                 `json:"noAuth"`
	OIDCIssuer       string               `json:"oidcIssuer"`
	APITokensEnabled bool                 `json:"apiTokensEnabled"`
	APITokenStore    settingsStoreSummary `json:"apiTokenStore"`
}

type settingsExecutionSummary struct {
	MaxParallelRuns int `json:"maxParallelRuns"`
}

type settingsMetricsSummary struct {
	Enabled             bool `json:"enabled"`
	CPUInterval         int  `json:"cpuInterval"`
	MemoryInterval      int  `json:"memoryInterval"`
	StorageInterval     int  `json:"storageInterval"`
	MemoryHistoryWindow int  `json:"memoryHistoryWindow"`
}

type settingsStorageSummary struct {
	Backend          string               `json:"backend"`
	HistoryDir       string               `json:"historyDir"`
	HistoryKeepCount int                  `json:"historyKeepCount"`
	Worktree         settingsWorktreeKeep `json:"worktree"`
	Object           settingsStoreSummary `json:"object"`
}

type settingsWorktreeKeep struct {
	KeepOnSuccess int `json:"keepOnSuccess"`
	KeepOnFailure int `json:"keepOnFailure"`
}

type settingsStoreSummary struct {
	Backend   string `json:"backend"`
	LocalPath string `json:"localPath,omitempty"`
	Endpoint  string `json:"endpoint,omitempty"`
	Bucket    string `json:"bucket,omitempty"`
	Region    string `json:"region,omitempty"`
	Prefix    string `json:"prefix,omitempty"`
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	claims := ClaimsFromContext(r.Context())
	if claims == nil || !s.config.IsAdminUser(claims) {
		s.writeError(w, http.StatusForbidden, "user is not allowed to view settings")
		return
	}
	s.writeJSON(w, http.StatusOK, s.settingsSummary())
}

func (s *Server) settingsSummary() settingsSummaryResponse {
	paths := uiconfig.RuntimePaths{}
	if s.manager != nil && s.manager.history != nil {
		paths = uiconfig.ResolveRuntimePaths(workingDirFromHistoryDir(s.manager.history.historyDir), s.config)
		paths.HistoryDir = s.manager.history.historyDir
		paths.APITokenLocalPath = uiconfig.ResolveAPITokenLocalPath(paths.HistoryDir, s.config.Auth.APITokens.Store.LocalPath)
	}

	if paths.RepositorySource == "" {
		paths.RepositorySource = s.config.Repository.Source
	}
	if paths.CachePath == "" && s.repo != nil {
		paths.CachePath = s.repo.BasePath()
	}
	if paths.HistoryDir == "" && s.config.Storage.HistoryDir != "" {
		paths.HistoryDir = s.config.Storage.HistoryDir
	}

	return settingsSummaryResponse{
		RuntimeMode: s.runtimeMode,
		Repository: settingsRepositorySummary{
			Source:                paths.RepositorySource,
			CachePath:             paths.CachePath,
			AccessTokenConfigured: repositoryTokenConfigured(s.config.Repository.Auth),
		},
		Auth: settingsAuthSummary{
			NoAuth:           s.config.Auth.NoAuth,
			OIDCIssuer:       s.config.Auth.OIDCIssuer,
			APITokensEnabled: s.auth != nil && s.auth.TokenService() != nil && s.auth.TokenService().Enabled(),
			APITokenStore:    summarizeAPITokenStore(paths.HistoryDir, s.config.Auth.APITokens),
		},
		Execution: settingsExecutionSummary{
			MaxParallelRuns: s.config.Execution.MaxParallelRuns,
		},
		Metrics: settingsMetricsSummary{
			Enabled:             s.config.Metrics.Enabled,
			CPUInterval:         s.config.Metrics.CPUInterval,
			MemoryInterval:      s.config.Metrics.MemoryInterval,
			StorageInterval:     s.config.Metrics.StorageInterval,
			MemoryHistoryWindow: s.config.Metrics.MemoryHistoryWindow,
		},
		Storage: settingsStorageSummary{
			Backend:          s.config.Storage.Backend,
			HistoryDir:       paths.HistoryDir,
			HistoryKeepCount: s.config.Storage.HistoryKeepCount,
			Worktree: settingsWorktreeKeep{
				KeepOnSuccess: s.config.Storage.Worktree.KeepOnSuccess,
				KeepOnFailure: s.config.Storage.Worktree.KeepOnFailure,
			},
			Object: summarizeObjectStore(s.config.Storage.Backend, s.config.Storage.Object),
		},
	}
}

func summarizeAPITokenStore(historyDir string, cfg uiconfig.APITokenConfig) settingsStoreSummary {
	summary := settingsStoreSummary{Backend: cfg.Store.Backend}
	if summary.Backend == "" {
		summary.Backend = "local"
	}
	switch summary.Backend {
	case "local":
		summary.LocalPath = uiconfig.ResolveAPITokenLocalPath(historyDir, cfg.Store.LocalPath)
	case "object":
		summary.Endpoint = cfg.Store.Object.Endpoint
		summary.Bucket = cfg.Store.Object.Bucket
		summary.Region = cfg.Store.Object.Region
		summary.Prefix = cfg.Store.Object.Prefix
	}
	return summary
}

func summarizeObjectStore(backend string, cfg uiconfig.ObjectStorageConfig) settingsStoreSummary {
	summary := settingsStoreSummary{Backend: backend}
	if summary.Backend == "object" {
		summary.Endpoint = cfg.Endpoint
		summary.Bucket = cfg.Bucket
		summary.Region = cfg.Region
		summary.Prefix = cfg.Prefix
	}
	return summary
}

func repositoryTokenConfigured(cfg uiconfig.RepositoryAuthConfig) bool {
	if cfg.Type != "envToken" || strings.TrimSpace(cfg.TokenEnv) == "" {
		return false
	}
	return strings.TrimSpace(os.Getenv(cfg.TokenEnv)) != ""
}

func workingDirFromHistoryDir(path string) string {
	if strings.TrimSpace(path) == "" {
		return "."
	}
	return filepath.Dir(filepath.Dir(path))
}
