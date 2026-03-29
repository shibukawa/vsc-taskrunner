package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"vsc-taskrunner/internal/uiconfig"
)

func TestBareRepositoryStoreListBranchesAndReadTasks(t *testing.T) {
	t.Parallel()

	origin := initRemoteWithTasks(t)
	store, err := NewBareRepositoryStore("file://"+origin, filepath.Join(t.TempDir(), "cache.git"), 1, []string{".vscode"}, uiconfig.RepositoryAuthConfig{})
	if err != nil {
		t.Fatal(err)
	}

	branches, err := store.ListBranches(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(branches) != 2 {
		t.Fatalf("branches = %d, want 2", len(branches))
	}
	if branches[0].ShortName != "dev" || branches[1].ShortName != "main" {
		t.Fatalf("unexpected branches: %+v", branches)
	}

	data, err := store.ReadTasksJSON(context.Background(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) == "" {
		t.Fatal("expected tasks.json contents")
	}

	commitHash, commitDate, branchData, err := store.ReadBranchMetadata(context.Background(), "main", ".vscode/tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	if commitHash == "" || commitDate.IsZero() {
		t.Fatalf("expected commit metadata, got hash=%q date=%v", commitHash, commitDate)
	}
	if string(branchData) == "" {
		t.Fatal("expected branch tasks contents")
	}
}

func TestBareRepositoryStorePrepareRunWorkspace(t *testing.T) {
	t.Parallel()

	origin := initRemoteWithTasks(t)
	store, err := NewBareRepositoryStore("file://"+origin, filepath.Join(t.TempDir(), "cache.git"), 1, []string{".vscode"}, uiconfig.RepositoryAuthConfig{})
	if err != nil {
		t.Fatal(err)
	}

	workspace := filepath.Join(t.TempDir(), "run-workspace")
	if _, err := store.PrepareRunWorkspace(context.Background(), "main", workspace, []string{".vscode", "README.md"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".vscode", "tasks.json")); err != nil {
		t.Fatalf("expected tasks.json in workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "README.md")); err != nil {
		t.Fatalf("expected README.md in workspace: %v", err)
	}
}

func TestBareRepositoryStorePrepareRunWorkspaceWithTasksSparsePaths(t *testing.T) {
	t.Parallel()

	origin := initRemoteWithTasks(t)
	store, err := NewBareRepositoryStore("file://"+origin, filepath.Join(t.TempDir(), "cache.git"), 1, []string{".vscode"}, uiconfig.RepositoryAuthConfig{})
	if err != nil {
		t.Fatal(err)
	}

	workspace := filepath.Join(t.TempDir(), "run-workspace")
	if _, err := store.PrepareRunWorkspace(context.Background(), "dev", workspace, []string{".vscode"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(workspace, ".vscode", "tasks.json")); err != nil {
		t.Fatalf("expected tasks.json in workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(workspace, "README.md")); !os.IsNotExist(err) {
		t.Fatalf("expected README.md to be excluded from sparse workspace, err=%v", err)
	}
}

func initRemoteWithTasks(t *testing.T) string {
	t.Helper()
	repo := initGitRepo(t)
	if err := os.MkdirAll(filepath.Join(repo, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "add", ".vscode/tasks.json")
	runGitCommand(t, repo, "commit", "-m", "add tasks")
	runGitCommand(t, repo, "checkout", "-b", "dev")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dev\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitCommand(t, repo, "add", "README.md")
	runGitCommand(t, repo, "commit", "-m", "dev update")
	runGitCommand(t, repo, "checkout", "main")

	bare := filepath.Join(t.TempDir(), "remote.git")
	runGitCommand(t, "", "clone", "--bare", repo, bare)
	return bare
}
