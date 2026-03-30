package uiconfig

import (
	"os"
	"path/filepath"
)

type RuntimePaths struct {
	RepositorySource  string
	CachePath         string
	WorkspaceBase     string
	HistoryDir        string
	APITokenLocalPath string
}

func ResolveRuntimePaths(wd string, cfg *UIConfig) RuntimePaths {
	source := cfg.Repository.Source
	if source != "" && !cfg.Repository.IsRemoteSource() && !filepath.IsAbs(source) {
		source = filepath.Join(wd, source)
	}

	cachePath := cfg.Repository.CachePath
	if cachePath != "" && !filepath.IsAbs(cachePath) {
		cachePath = filepath.Join(wd, cachePath)
	}

	workspaceBase := cachePath
	if !cfg.Repository.IsRemoteSource() {
		workspaceBase = source
	}

	historyDir := resolveHistoryDir(cfg, workspaceBase, cachePath)

	return RuntimePaths{
		RepositorySource:  source,
		CachePath:         cachePath,
		WorkspaceBase:     workspaceBase,
		HistoryDir:        historyDir,
		APITokenLocalPath: ResolveAPITokenLocalPath(historyDir, cfg.Auth.APITokens.Store.LocalPath),
	}
}

func resolveHistoryDir(cfg *UIConfig, workspaceBase string, cachePath string) string {
	historyDir := cfg.Storage.HistoryDir
	if shouldUseObjectTempHistoryDir(cfg.Storage.Backend, historyDir) {
		return filepath.Join(os.TempDir(), "runtask", "history")
	}
	if historyDir != "" && !filepath.IsAbs(historyDir) {
		if cfg.Repository.IsRemoteSource() {
			stagingRoot := filepath.Dir(filepath.Dir(cachePath))
			historyDir = filepath.Join(stagingRoot, historyDir)
		} else {
			historyDir = filepath.Join(workspaceBase, historyDir)
		}
	}
	return historyDir
}

func shouldUseObjectTempHistoryDir(backend string, historyDir string) bool {
	return backend == "object" && (historyDir == "" || historyDir == DefaultHistoryDir)
}

func ResolveAPITokenLocalPath(historyDir string, localPath string) string {
	if localPath == "" || filepath.IsAbs(localPath) {
		return localPath
	}
	return filepath.Join(filepath.Dir(filepath.Dir(historyDir)), localPath)
}
