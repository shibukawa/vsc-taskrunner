package cli

import (
	"bufio"
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"vsc-taskrunner/internal/uiconfig"
)

func TestPromptTasksFallbackUsesCommaSeparatedInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("build,test\n"))
	var output strings.Builder

	selected, err := promptTasks(reader, &output, nil, nil, []string{"build", "test", "lint"}, []string{"build", "test", "lint"}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(selected, ","), "build,test"; got != want {
		t.Fatalf("selected = %q, want %q", got, want)
	}
	if !strings.Contains(output.String(), "Available tasks: build, test, lint") {
		t.Fatalf("unexpected prompt output: %q", output.String())
	}
}

func TestPromptBranchesFallbackUsesCommaSeparatedInput(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader("main,feature/demo\n"))
	var output strings.Builder

	selected, err := promptBranches(reader, &output, nil, nil, []string{"dev", "feature/demo", "main"}, []string{"dev", "main"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(selected, ","), "main,feature/demo"; got != want {
		t.Fatalf("selected = %q, want %q", got, want)
	}
	if !strings.Contains(output.String(), "Available branches: dev, feature/demo, main") {
		t.Fatalf("unexpected prompt output: %q", output.String())
	}
}

func TestAppRunUIInitWritesConfig(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	repo := filepath.Join(workspace, "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "init", "-b", "main")
	runGitForUITest(t, repo, "config", "user.email", "test@example.com")
	runGitForUITest(t, repo, "config", "user.name", "tester")
	if err := os.WriteFile(filepath.Join(repo, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"go-build","type":"process","command":"go"},{"label":"go-test","type":"process","command":"go"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "add", ".vscode/tasks.json")
	runGitForUITest(t, repo, "commit", "-m", "init")
	runGitForUITest(t, repo, "checkout", "-b", "feature/demo")
	runGitForUITest(t, repo, "checkout", "main")

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString(strings.Join([]string{
		"",
		"",
		"",
		"main",
		"go-build,go-test",
		"dist/go-build",
		"",
		"oidc",
		"https://issuer.example.com",
		"client-id",
		"client-secret",
		"local",
		"",
		"",
		"",
	}, "\n"))
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "init"}); exitCode != 0 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}

	configPath := filepath.Join(repo, "runtask-ui.yaml")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# yaml-language-server: $schema="+uiconfig.SchemaURL+"\n") {
		t.Fatalf("generated config missing schema comment:\n%s", string(data))
	}
	cfg, err := uiconfig.LoadConfig(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Repository.Source != repo {
		t.Fatalf("repository.source = %q, want %q", cfg.Repository.Source, repo)
	}
	if got, want := strings.Join(cfg.Branches, ","), "main"; got != want {
		t.Fatalf("branches = %q, want %q", got, want)
	}
	if cfg.Server.Port != 8080 {
		t.Fatalf("server.port = %d, want 8080", cfg.Server.Port)
	}
	if !cfg.MatchTask("go-build") || !cfg.MatchTask("go-test") {
		t.Fatalf("tasks = %+v", cfg.Tasks)
	}
	taskCfg, ok := cfg.TaskConfig("go-build")
	if !ok || len(taskCfg.Artifacts) != 1 || taskCfg.Artifacts[0].Path != "dist/go-build" {
		t.Fatalf("go-build task config = %+v", taskCfg)
	}
	testCfg, ok := cfg.TaskConfig("go-test")
	if !ok || len(testCfg.Artifacts) != 0 {
		t.Fatalf("go-test task config = %+v", testCfg)
	}
	if cfg.Auth.NoAuth {
		t.Fatalf("expected oidc auth, got %+v", cfg.Auth)
	}
	if cfg.Auth.OIDCIssuer != "https://issuer.example.com" || cfg.Auth.OIDCClientID != "client-id" || cfg.Auth.OIDCClientSecret != "client-secret" {
		t.Fatalf("auth = %+v", cfg.Auth)
	}
	if cfg.Storage.Backend != "local" || cfg.Storage.HistoryDir != uiconfig.DefaultHistoryDir {
		t.Fatalf("storage = %+v", cfg.Storage)
	}
	if !cfg.Metrics.Enabled {
		t.Fatal("expected metrics enabled")
	}
	if cfg.Execution.MaxParallelRuns != 4 {
		t.Fatalf("execution.maxParallelRuns = %d, want 4", cfg.Execution.MaxParallelRuns)
	}
}

func TestAppRunUIInitOverwritesExistingConfigOnlyAfterConfirmation(t *testing.T) {
	t.Parallel()

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "init", "-b", "main")
	runGitForUITest(t, repo, "config", "user.email", "test@example.com")
	runGitForUITest(t, repo, "config", "user.name", "tester")
	if err := os.WriteFile(filepath.Join(repo, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "add", ".")
	runGitForUITest(t, repo, "commit", "-m", "init")
	configPath := filepath.Join(repo, "runtask-ui.yaml")
	if err := os.WriteFile(configPath, []byte("repository:\n  source: /tmp/original\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString(strings.Join([]string{
		"",
		"",
		"main",
		"build",
		"",
		"noauth",
		"local",
		"",
		"",
		"",
		"n",
	}, "\n"))
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "init"}); exitCode != 1 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "repository:\n  source: /tmp/original\n" {
		t.Fatalf("config changed unexpectedly: %s", string(data))
	}
}

func TestAppRunUIInitWriteFalsePrintsSchemaComment(t *testing.T) {
	t.Parallel()

	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "init", "-b", "main")
	runGitForUITest(t, repo, "config", "user.email", "test@example.com")
	runGitForUITest(t, repo, "config", "user.name", "tester")
	if err := os.WriteFile(filepath.Join(repo, ".vscode", "tasks.json"), []byte(`{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo ok"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "add", ".")
	runGitForUITest(t, repo, "commit", "-m", "init")

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString(strings.Join([]string{
		"",
		"",
		"main",
		"build",
		"",
		"noauth",
		"local",
		"",
		"",
		"",
	}, "\n"))
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "init", "--write=false"}); exitCode != 0 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}
	if !strings.HasPrefix(stdout.String(), "# yaml-language-server: $schema="+uiconfig.SchemaURL+"\n") {
		t.Fatalf("stdout missing schema comment:\n%s", stdout.String())
	}
}

func TestUIInitBranchChoicesFiltersNestedBranches(t *testing.T) {
	t.Parallel()

	repo := filepath.Join(t.TempDir(), "repo")
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
	runGitForUITest(t, repo, "checkout", "-b", "dev")
	runGitForUITest(t, repo, "checkout", "main")
	runGitForUITest(t, repo, "checkout", "-b", "feature/demo")

	branches, defaults, current, err := uiInitBranchChoices(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(branches, ","), "dev,feature/demo,main"; got != want {
		t.Fatalf("branches = %q, want %q", got, want)
	}
	if got, want := strings.Join(defaults, ","), "dev,main"; got != want {
		t.Fatalf("defaults = %q, want %q", got, want)
	}
	if current != "feature/demo" {
		t.Fatalf("current = %q, want feature/demo", current)
	}
}

func TestAppRunUIEditTaskAddWritesConfig(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"},{"label":"test","type":"shell","command":"echo test"}]}`)
	writeUIEditConfig(t, repo, strings.Join([]string{
		"server:",
		"  port: 8080",
		"repository:",
		"  source: " + repo,
		"auth:",
		"  noAuth: true",
		"branches:",
		"  - main",
		"tasks:",
		"  build: {}",
		"execution:",
		"  maxParallelRuns: 4",
		"metrics:",
		"  enabled: true",
		"storage:",
		"  backend: local",
		"  historyDir: .runtask/history",
		"",
	}, "\n"))

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString("build,test\n\ndist/test\ny\n")
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "edit", "task"}); exitCode != 0 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}

	cfg, err := uiconfig.LoadConfig(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# yaml-language-server: $schema="+uiconfig.SchemaURL+"\n") {
		t.Fatalf("edited config missing schema comment:\n%s", string(data))
	}
	taskCfg, ok := cfg.FindExactTaskConfig("test")
	if !ok || len(taskCfg.Artifacts) != 1 || taskCfg.Artifacts[0].Path != "dist/test" {
		t.Fatalf("task config = %+v", taskCfg)
	}
}

func TestAppRunUIEditTaskUpdatePreservesOtherFields(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"}]}`)
	writeUIEditConfig(t, repo, strings.Join([]string{
		"server:",
		"  port: 8080",
		"repository:",
		"  source: " + repo,
		"auth:",
		"  noAuth: true",
		"branches:",
		"  - main",
		"tasks:",
		"  build:",
		"    artifacts:",
		"      - path: dist/old",
		"    preRunTask:",
		"      - command: echo",
		"        args:",
		"          - prepare",
		"    worktreeDisabled: true",
		"execution:",
		"  maxParallelRuns: 4",
		"metrics:",
		"  enabled: true",
		"storage:",
		"  backend: local",
		"  historyDir: .runtask/history",
		"",
	}, "\n"))

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString("build\ndist/new\ny\n")
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "edit", "task"}); exitCode != 0 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}

	cfg, err := uiconfig.LoadConfig(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	taskCfg, ok := cfg.FindExactTaskConfig("build")
	if !ok {
		t.Fatal("expected build task")
	}
	if len(taskCfg.Artifacts) != 1 || taskCfg.Artifacts[0].Path != "dist/new" {
		t.Fatalf("artifacts = %+v", taskCfg.Artifacts)
	}
	if len(taskCfg.PreRunTasks) != 1 || taskCfg.PreRunTasks[0].Command != "echo" {
		t.Fatalf("preRunTasks = %+v", taskCfg.PreRunTasks)
	}
	if !taskCfg.WorktreeDisabled {
		t.Fatalf("expected worktreeDisabled to be preserved: %+v", taskCfg)
	}
}

func TestAppRunUIEditTaskRemoveKeepsOtherTasks(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"},{"label":"test","type":"shell","command":"echo test"}]}`)
	writeUIEditConfig(t, repo, strings.Join([]string{
		"server:",
		"  port: 8080",
		"repository:",
		"  source: " + repo,
		"auth:",
		"  noAuth: true",
		"branches:",
		"  - main",
		"tasks:",
		"  build: {}",
		"  test: {}",
		"execution:",
		"  maxParallelRuns: 4",
		"metrics:",
		"  enabled: true",
		"storage:",
		"  backend: local",
		"  historyDir: .runtask/history",
		"",
	}, "\n"))

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString("test\n\ny\n")
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "edit", "task"}); exitCode != 0 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}

	cfg, err := uiconfig.LoadConfig(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg.FindExactTaskConfig("build"); ok {
		t.Fatal("expected build to be removed")
	}
	if _, ok := cfg.FindExactTaskConfig("test"); !ok {
		t.Fatal("expected test to remain")
	}
}

func TestAppRunUIEditBranchAddAndSetDefault(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"}]}`)
	runGitForUITest(t, repo, "checkout", "-b", "dev")
	runGitForUITest(t, repo, "checkout", "main")
	writeUIEditConfig(t, repo, strings.Join([]string{
		"server:",
		"  port: 8080",
		"repository:",
		"  source: " + repo,
		"auth:",
		"  noAuth: true",
		"branches:",
		"  - main",
		"tasks:",
		"  build: {}",
		"execution:",
		"  maxParallelRuns: 4",
		"metrics:",
		"  enabled: true",
		"storage:",
		"  backend: local",
		"  historyDir: .runtask/history",
		"",
	}, "\n"))

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString("dev,main\ndev\ny\n")
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }
	if exitCode := app.Run([]string{"ui", "edit", "branch"}); exitCode != 0 {
		t.Fatalf("edit branch exitCode = %d stderr=%s", exitCode, stderr.String())
	}

	cfg, err := uiconfig.LoadConfig(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "# yaml-language-server: $schema="+uiconfig.SchemaURL+"\n") {
		t.Fatalf("edited config missing schema comment:\n%s", string(data))
	}
	if got, want := strings.Join(cfg.Branches, ","), "dev,main"; got != want {
		t.Fatalf("branches = %q, want %q", got, want)
	}
}

func TestAppRunUIEditBranchRemoveLastBranch(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"}]}`)
	writeUIEditConfig(t, repo, strings.Join([]string{
		"server:",
		"  port: 8080",
		"repository:",
		"  source: " + repo,
		"auth:",
		"  noAuth: true",
		"branches:",
		"  - main",
		"tasks:",
		"  build: {}",
		"execution:",
		"  maxParallelRuns: 4",
		"metrics:",
		"  enabled: true",
		"storage:",
		"  backend: local",
		"  historyDir: .runtask/history",
		"",
	}, "\n"))

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString("-\ny\n")
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }
	if exitCode := app.Run([]string{"ui", "edit", "branch"}); exitCode != 0 {
		t.Fatalf("exitCode = %d stderr=%s", exitCode, stderr.String())
	}

	cfg, err := uiconfig.LoadConfig(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Branches) != 0 {
		t.Fatalf("branches = %+v, want empty", cfg.Branches)
	}
}

func TestAppRunUIEditFailsWithoutConfig(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"}]}`)
	app := NewApp(strings.NewReader(""), &strings.Builder{}, &strings.Builder{})
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "edit", "task"}); exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1", exitCode)
	}
}

func TestAppRunUIEditTaskCancelKeepsConfig(t *testing.T) {
	t.Parallel()

	repo := initUIEditRepo(t, `{"version":"2.0.0","tasks":[{"label":"build","type":"shell","command":"echo build"}]}`)
	original := strings.Join([]string{
		"server:",
		"  port: 8080",
		"repository:",
		"  source: " + repo,
		"auth:",
		"  noAuth: true",
		"branches:",
		"  - main",
		"tasks:",
		"  build: {}",
		"execution:",
		"  maxParallelRuns: 4",
		"metrics:",
		"  enabled: true",
		"storage:",
		"  backend: local",
		"  historyDir: .runtask/history",
		"",
	}, "\n")
	writeUIEditConfig(t, repo, original)

	var stdout strings.Builder
	var stderr strings.Builder
	stdin := bytes.NewBufferString("build\ndist/new\nn\n")
	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return repo, nil }

	if exitCode := app.Run([]string{"ui", "edit", "task"}); exitCode != 1 {
		t.Fatalf("exitCode = %d, want 1 stderr=%s", exitCode, stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(repo, "runtask-ui.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("config changed unexpectedly:\n%s", string(data))
	}
}

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

func initUIEditRepo(t *testing.T, tasksJSON string) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".vscode"), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "init", "-b", "main")
	runGitForUITest(t, repo, "config", "user.email", "test@example.com")
	runGitForUITest(t, repo, "config", "user.name", "tester")
	if err := os.WriteFile(filepath.Join(repo, ".vscode", "tasks.json"), []byte(tasksJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForUITest(t, repo, "add", ".vscode/tasks.json")
	runGitForUITest(t, repo, "commit", "-m", "init")
	return repo
}

func writeUIEditConfig(t *testing.T, repo string, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "runtask-ui.yaml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
