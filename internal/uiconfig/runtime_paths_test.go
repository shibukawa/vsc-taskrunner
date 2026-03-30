package uiconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveRuntimePathsUsesTempDirForObjectDefaultHistoryDir(t *testing.T) {
	t.Parallel()

	wd := t.TempDir()
	cfg := DefaultConfig()
	cfg.Repository.Source = "https://example.com/acme/demo.git"
	cfg.Repository.CachePath = filepath.Join("state", "repos", "demo-cache.git")
	cfg.Storage.Backend = "object"

	paths := ResolveRuntimePaths(wd, cfg)
	want := filepath.Join(os.TempDir(), "runtask", "history")
	if paths.HistoryDir != want {
		t.Fatalf("HistoryDir = %q, want %q", paths.HistoryDir, want)
	}
}

func TestResolveRuntimePathsKeepsExplicitObjectHistoryDir(t *testing.T) {
	t.Parallel()

	wd := t.TempDir()
	cfg := DefaultConfig()
	cfg.Repository.Source = "https://example.com/acme/demo.git"
	cfg.Repository.CachePath = filepath.Join(wd, "state", "repos", "demo-cache.git")
	cfg.Storage.Backend = "object"
	cfg.Storage.HistoryDir = filepath.Join(wd, "custom", "history")

	paths := ResolveRuntimePaths(wd, cfg)
	if paths.HistoryDir != cfg.Storage.HistoryDir {
		t.Fatalf("HistoryDir = %q, want %q", paths.HistoryDir, cfg.Storage.HistoryDir)
	}
}

func TestResolveRuntimePathsKeepsExplicitObjectRelativeHistoryDir(t *testing.T) {
	t.Parallel()

	wd := t.TempDir()
	cfg := DefaultConfig()
	cfg.Repository.Source = "https://example.com/acme/demo.git"
	cfg.Repository.CachePath = filepath.Join(wd, "state", "repos", "demo-cache.git")
	cfg.Storage.Backend = "object"
	cfg.Storage.HistoryDir = "custom/history"

	paths := ResolveRuntimePaths(wd, cfg)
	want := filepath.Join(wd, "state", "custom", "history")
	if paths.HistoryDir != want {
		t.Fatalf("HistoryDir = %q, want %q", paths.HistoryDir, want)
	}
}

func TestResolveRuntimePathsKeepsLocalHistoryDirUnderRepository(t *testing.T) {
	t.Parallel()

	wd := t.TempDir()
	repo := filepath.Join(wd, "repo")
	cfg := DefaultConfig()
	cfg.Repository.Source = repo
	cfg.Storage.Backend = "local"

	paths := ResolveRuntimePaths(wd, cfg)
	want := filepath.Join(repo, DefaultHistoryDir)
	if paths.HistoryDir != want {
		t.Fatalf("HistoryDir = %q, want %q", paths.HistoryDir, want)
	}
}
