package cli

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vsc-taskrunner/internal/uiconfig"
)

func TestLoadUIContextResolvesRemoteRelativeHistoryDirOutsideRepoCache(t *testing.T) {
	t.Parallel()

	wd := t.TempDir()
	repo := filepath.Join(wd, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "init", "-b", "main")
	runGitForUITest(t, repo, "config", "user.email", "test@example.com")
	runGitForUITest(t, repo, "config", "user.name", "tester")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "add", "README.md")
	runGitForUITest(t, repo, "commit", "-m", "init")
	remote := filepath.Join(wd, "remote.git")
	runGitForUITest(t, wd, "clone", "--bare", repo, remote)

	configPath := filepath.Join(wd, "runtask-ui.yaml")
	cachePath := filepath.Join(wd, "state", "repos", "demo-cache.git")
	config := strings.Join([]string{
		"repository:",
		"  source: file://" + remote,
		"  cachePath: " + cachePath,
		"storage:",
		"  historyDir: .runtask/history",
		"",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), &strings.Builder{}, &strings.Builder{})
	app.wd = func() (string, error) { return wd, nil }

	_, _, historyDir, err := app.loadUIContext("", configPath)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(wd, "state", ".runtask", "history")
	if historyDir != want {
		t.Fatalf("historyDir = %q, want %q", historyDir, want)
	}
}

func TestNewHistoryStoreWithKindsUsesLocalStores(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Storage.Backend = "local"

	history, indexStoreKind, runStoreKind, err := newHistoryStoreWithKinds(context.Background(), filepath.Join(t.TempDir(), "history"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if history == nil {
		t.Fatal("expected history store")
	}
	if indexStoreKind != "*web.LocalIndexStore" || runStoreKind != "*web.LocalRunStore" {
		t.Fatalf("unexpected store kinds: %q %q", indexStoreKind, runStoreKind)
	}
}

func TestNewHistoryStoreWithKindsUsesObjectStores(t *testing.T) {
	t.Parallel()

	cfg := uiconfig.DefaultConfig()
	cfg.Repository.Source = "/tmp/repo"
	cfg.Storage.Backend = "object"
	cfg.Storage.Object.Endpoint = "http://127.0.0.1:8333"
	cfg.Storage.Object.Bucket = "runtask"
	cfg.Storage.Object.Region = "us-east-1"
	cfg.Storage.Object.AccessKey = "admin"
	cfg.Storage.Object.SecretKey = "admin123"
	cfg.Storage.Object.Prefix = "runtask"
	cfg.Storage.Object.ForcePathStyle = true

	history, indexStoreKind, runStoreKind, err := newHistoryStoreWithKinds(context.Background(), filepath.Join(t.TempDir(), "history"), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if history == nil {
		t.Fatal("expected history store")
	}
	if indexStoreKind != "*web.ObjectIndexStore" || runStoreKind != "*web.ObjectRunStore" {
		t.Fatalf("unexpected store kinds: %q %q", indexStoreKind, runStoreKind)
	}
}

func runGitForUITest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, string(out))
	}
}
