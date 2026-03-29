package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEnsureRepositoryUsesLocalPathAsIs(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	got, err := EnsureRepository(context.Background(), repo, "")
	if err != nil {
		t.Fatal(err)
	}
	if got != repo {
		t.Fatalf("repo path = %q, want %q", got, repo)
	}
}

func TestEnsureRepositoryClonesRemoteIntoLocalPath(t *testing.T) {
	t.Parallel()

	origin := initBareRemote(t, "main")
	originURL := "file://" + origin
	clonePath := filepath.Join(t.TempDir(), "clone")
	got, err := EnsureRepository(context.Background(), originURL, clonePath)
	if err != nil {
		t.Fatal(err)
	}
	if got != clonePath {
		t.Fatalf("repo path = %q, want %q", got, clonePath)
	}
	if _, err := os.Stat(filepath.Join(clonePath, ".git")); err != nil {
		t.Fatalf("expected cloned .git directory: %v", err)
	}
}

func TestEnsureRepositoryRejectsOriginMismatch(t *testing.T) {
	t.Parallel()

	firstOrigin := initBareRemote(t, "main")
	secondOrigin := initBareRemote(t, "dev")
	firstOriginURL := "file://" + firstOrigin
	secondOriginURL := "file://" + secondOrigin
	clonePath := filepath.Join(t.TempDir(), "clone")
	if _, err := EnsureRepository(context.Background(), firstOriginURL, clonePath); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureRepository(context.Background(), secondOriginURL, clonePath); err == nil {
		t.Fatal("expected origin mismatch to fail")
	}
}

func initGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitCommand(t, repo, "init", "-b", "main")
	runGitCommand(t, repo, "config", "user.name", "Test User")
	runGitCommand(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "add", "README.md")
	runGitCommand(t, repo, "commit", "-m", "init")
	return repo
}

func initBareRemote(t *testing.T, branch string) string {
	t.Helper()
	source := initGitRepo(t)
	if branch != "main" {
		runGitCommand(t, source, "checkout", "-b", branch)
	}
	bare := filepath.Join(t.TempDir(), "remote.git")
	runGitCommand(t, "", "clone", "--bare", source, bare)
	return bare
}

func runGitCommand(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(out))
	}
}
