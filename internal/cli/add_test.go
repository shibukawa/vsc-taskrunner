package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"vsc-taskrunner/internal/tasks"
)

func TestSplitCommandLine(t *testing.T) {
	t.Parallel()

	args, err := splitCommandLine(`test "./pkg with spaces/..." --run '^TestAdd$'`)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(args, "|")
	want := "test|./pkg with spaces/...|--run|^TestAdd$"
	if got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestAppRunAddCreatesTask(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	stdin := bytes.NewBufferString("build\nprocess\ngo\ntest ./...\ny\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	exitCode := app.Run([]string{"add"})
	if exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	task := file.Tasks[0]
	if task.Label != "build" || task.Type != "process" || task.Command.Value != "go" {
		t.Fatalf("unexpected task: %+v", task)
	}
	if got := len(task.Args); got != 2 {
		t.Fatalf("expected 2 args, got %d", got)
	}
	if !strings.Contains(stdout.String(), "added task \"build\"") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddCancelled(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	stdin := bytes.NewBufferString("build\nshell\necho\nhello\nn\n")
	var stdout bytes.Buffer
	var stderr bytes.Buffer

	app := NewApp(stdin, &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	exitCode := app.Run([]string{"add"})
	if exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "task creation cancelled") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestAppRunAddNPMAll(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{
		"scripts": {
			"build": "tsc -p .",
			"test": "go test ./...",
			"lint:fix": "eslint . --fix",
			"prebuild": "node prep.js"
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "npm", "--all"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Type, "npm"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
	byScript := make(map[string]bool, len(file.Tasks))
	for _, task := range file.Tasks {
		byScript[task.Script] = true
	}
	if byScript["lint:fix"] {
		t.Fatalf("saved npm scripts = %#v, did not expect lint:fix", byScript)
	}
	if !byScript["build"] || !byScript["test"] || !byScript["prebuild"] {
		t.Fatalf("saved npm scripts = %#v, want build/test/prebuild", byScript)
	}
	if !strings.Contains(stdout.String(), "added 3 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddTypeScriptAll(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2022"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "typescript", "--all"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Type, "typescript"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
	buildGroup, ok := tasks.ParseTaskGroup(file.Tasks[0].Group)
	if !ok || buildGroup.Kind != "build" || !buildGroup.IsDefault {
		t.Fatalf("build group = %+v, want default build group", buildGroup)
	}
}

func TestAppRunAddTypeScriptAllSkipsModesProvidedByRootNPM(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"scripts":{"build":"tsc -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2022"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "typescript", "--all"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Label, "tsc-watch-tsconfig.json"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "added task \"tsc-watch-tsconfig.json\"") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddTypeScriptTaskSelectionSkipsSuppressedModes(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"scripts":{"build":"tsc -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2022"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "typescript", "--task", "build,watch"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Option, "watch"; got != want {
		t.Fatalf("option = %q, want %q", got, want)
	}
}

func TestAppRunAddTypeScriptFailsWhenRootNPMProvidesAllModes(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"scripts":{"build":"tsc -p .","watch":"tsc -w -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2022"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "typescript", "--all"}); exitCode == 0 {
		t.Fatal("expected non-zero exit code")
	}
	if !strings.Contains(stderr.String(), "workspace-root npm scripts take priority for: build, watch") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestAppRunAddGoCreatesCommonTasks(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if got, want := string(byLabel["go-build"].ProblemMatcher), `"$go"`; got != want {
		t.Fatalf("problemMatcher = %s, want %s", string(got), want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["go-build"].Args), ","), "build,-trimpath,-ldflags=-s -w,./..."; got != want {
		t.Fatalf("go-build args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["go-test"].Args), ","), "test,-v,./..."; got != want {
		t.Fatalf("go-test args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["go-bench"].Args), ","), "test,-run=^$,-bench=.,-benchmem,./..."; got != want {
		t.Fatalf("go-bench args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["go-cover"].Args), ","), "test,-coverprofile=coverage.out,./..."; got != want {
		t.Fatalf("go-cover args = %q, want %q", got, want)
	}
	if got, want := byLabel["go-lint"].Type, "shell"; got != want {
		t.Fatalf("go-lint type = %q, want %q", got, want)
	}
	if got, want := byLabel["go-lint"].Command.Value, "gofmt -l -w . && go vet ./..."; got != want {
		t.Fatalf("go-lint command = %q, want %q", got, want)
	}
	buildGroup, ok := tasks.ParseTaskGroup(byLabel["go-build"].Group)
	if !ok || buildGroup.Kind != "build" || !buildGroup.IsDefault {
		t.Fatalf("build group = %+v, want default build group", buildGroup)
	}
	testGroup, ok := tasks.ParseTaskGroup(byLabel["go-test"].Group)
	if !ok || testGroup.Kind != "test" || !testGroup.IsDefault {
		t.Fatalf("test group = %+v, want default test group", testGroup)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["go-bench"].Group); ok {
		t.Fatalf("bench group = %+v, want no group", group)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["go-cover"].Group); ok {
		t.Fatalf("cover group = %+v, want no group", group)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["go-lint"].Group); ok {
		t.Fatalf("lint group = %+v, want no group", group)
	}
	if !strings.Contains(stdout.String(), "added 5 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddGoCreatesCommandBuildTasks(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, commandName := range []string{"runtask", "worker"} {
		commandDir := filepath.Join(workspace, "cmd", commandName)
		if err := os.MkdirAll(commandDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(commandDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 7 {
		t.Fatalf("expected 7 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if got, want := byLabel["go-build"].Dependencies.Labels(), []string{"go-build-runtask", "go-build-worker"}; !equalStringSlices(got, want) {
		t.Fatalf("go-build dependsOn = %v, want %v", got, want)
	}
	if got, want := byLabel["go-build"].DependsOrder, "parallel"; got != want {
		t.Fatalf("go-build dependsOrder = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["go-build-runtask"].Args), ","), "build,-trimpath,-ldflags=-s -w,-o,./bin/runtask,./cmd/runtask"; got != want {
		t.Fatalf("go-build-runtask args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["go-build-worker"].Args), ","), "build,-trimpath,-ldflags=-s -w,-o,./bin/worker,./cmd/worker"; got != want {
		t.Fatalf("go-build-worker args = %q, want %q", got, want)
	}
	if got, want := byLabel["go-build-runtask"].Options.CWD, "${workspaceFolder}"; got != want {
		t.Fatalf("go-build-runtask cwd = %q, want %q", got, want)
	}
	if got, want := byLabel["go-build-worker"].Options.CWD, "${workspaceFolder}"; got != want {
		t.Fatalf("go-build-worker cwd = %q, want %q", got, want)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["go-build-runtask"].Group); ok {
		t.Fatalf("go-build-runtask group = %+v, want no group", group)
	}
	if !strings.Contains(stdout.String(), "added 7 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddGoKeepsExistingDefaultBuildTask(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tasksPath := filepath.Join(workspace, ".vscode", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(tasksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "npm-build",
				"type": "npm",
				"script": "build",
				"group": {"kind": "build", "isDefault": true}
			}
		]
	}`
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 6 {
		t.Fatalf("expected 6 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	buildGroup, ok := tasks.ParseTaskGroup(byLabel["go-build"].Group)
	if !ok || buildGroup.Kind != "build" || buildGroup.IsDefault {
		t.Fatalf("build group = %+v, want non-default build group", buildGroup)
	}
	testGroup, ok := tasks.ParseTaskGroup(byLabel["go-test"].Group)
	if !ok || testGroup.Kind != "test" || !testGroup.IsDefault {
		t.Fatalf("test group = %+v, want default test group", testGroup)
	}
	if !strings.Contains(stderr.String(), "A default build task already exists") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	if strings.Contains(stderr.String(), "A default test task already exists") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestAppRunAddGoBenchOnly(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "go", "--task", "bench"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Label, "go-bench"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "added task \"go-bench\"") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddRustCreatesExtraTasks(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "Cargo.toml"), []byte("[package]\nname = \"demo\"\nversion = \"0.1.0\"\nedition = \"2021\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "rust"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if got, want := strings.Join(tokensToStrings(byLabel["cargo-check"].Args), ","), "check"; got != want {
		t.Fatalf("cargo-check args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["cargo-bench"].Args), ","), "bench"; got != want {
		t.Fatalf("cargo-bench args = %q, want %q", got, want)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["cargo-check"].Group); ok {
		t.Fatalf("cargo-check group = %+v, want no group", group)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["cargo-bench"].Group); ok {
		t.Fatalf("cargo-bench group = %+v, want no group", group)
	}
	if !strings.Contains(stdout.String(), "added 4 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddSwiftCreatesExtraTasks(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := "// swift-tools-version: 5.9\nimport PackageDescription\nlet package = Package(name: \"Demo\")\n"
	if err := os.WriteFile(filepath.Join(workspace, "Package.swift"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "swift"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 4 {
		t.Fatalf("expected 4 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if got, want := strings.Join(tokensToStrings(byLabel["swift-clean"].Args), ","), "package,clean"; got != want {
		t.Fatalf("swift-clean args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["swift-run"].Args), ","), "run"; got != want {
		t.Fatalf("swift-run args = %q, want %q", got, want)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["swift-clean"].Group); ok {
		t.Fatalf("swift-clean group = %+v, want no group", group)
	}
	if group, ok := tasks.ParseTaskGroup(byLabel["swift-run"].Group); ok {
		t.Fatalf("swift-run group = %+v, want no group", group)
	}
	if !strings.Contains(stdout.String(), "added 4 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddGradleCleanAndLintOnly(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "build.gradle"), []byte("plugins {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "gradle", "--task", "clean,lint"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if got, want := strings.Join(tokensToStrings(byLabel["gradle-clean"].Args), ","), "clean"; got != want {
		t.Fatalf("gradle-clean args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["gradle-lint"].Args), ","), "check"; got != want {
		t.Fatalf("gradle-lint args = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "added 2 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddMavenCleanAndLintOnly(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "pom.xml"), []byte("<project/>\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "maven", "--task", "clean,lint"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if got, want := strings.Join(tokensToStrings(byLabel["maven-clean"].Args), ","), "clean"; got != want {
		t.Fatalf("maven-clean args = %q, want %q", got, want)
	}
	if got, want := strings.Join(tokensToStrings(byLabel["maven-lint"].Args), ","), "verify"; got != want {
		t.Fatalf("maven-lint args = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "added 2 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppRunAddGoLintOnly(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "go", "--task", "lint"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Label, "go-lint"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	if got, want := file.Tasks[0].Type, "shell"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
}

func TestAppRunAddGoCoverOnly(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "go", "--task", "cover"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Label, "go-cover"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}

func TestAppRunAddDetectSaveIncludesGoBench(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "detect", "--save", "--ecosystem", "go", "--all"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 5 {
		t.Fatalf("expected 5 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if _, ok := byLabel["go-bench"]; !ok {
		t.Fatalf("saved tasks = %#v, want go-bench", byLabel)
	}
	if _, ok := byLabel["go-cover"]; !ok {
		t.Fatalf("saved tasks = %#v, want go-cover", byLabel)
	}
	if _, ok := byLabel["go-lint"]; !ok {
		t.Fatalf("saved tasks = %#v, want go-lint", byLabel)
	}
}

func TestAppRunAddDetectSaveIncludesGoCommandBuilds(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	commandDir := filepath.Join(workspace, "cmd", "runtask")
	if err := os.MkdirAll(commandDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(commandDir, "main.go"), []byte("package main\nfunc main() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "detect", "--save", "--ecosystem", "go", "--all"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 6 {
		t.Fatalf("expected 6 tasks, got %d", len(file.Tasks))
	}
	byLabel := make(map[string]tasks.Task, len(file.Tasks))
	for _, task := range file.Tasks {
		byLabel[task.Label] = task
	}
	if _, ok := byLabel["go-build-runtask"]; !ok {
		t.Fatalf("saved tasks = %#v, want go-build-runtask", byLabel)
	}
	if got, want := byLabel["go-build"].Dependencies.Labels(), []string{"go-build-runtask"}; !equalStringSlices(got, want) {
		t.Fatalf("go-build dependsOn = %v, want %v", got, want)
	}
}

func tokensToStrings(values []tasks.TokenValue) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		result = append(result, value.Value)
	}
	return result
}

func equalStringSlices(got []string, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for index := range got {
		if got[index] != want[index] {
			return false
		}
	}
	return true
}

func TestAppRunAddDetectSaveFiltersByEcosystem(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{
		"scripts": {
			"build": "tsc -p .",
			"test": "vitest"
		}
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2022"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "detect", "--save", "--ecosystem", "npm"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}
	for _, task := range file.Tasks {
		if task.Type != "npm" {
			t.Fatalf("unexpected task type: %s", task.Type)
		}
	}
}

func TestAppRunAddGulpAutoDetectAll(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := `const gulp = require('gulp')
	gulp.task('build', () => {})
	gulp.task('lint', () => {})
	`
	if err := os.WriteFile(filepath.Join(workspace, "gulpfile.js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }

	if exitCode := app.Run([]string{"add", "gulp", "--all"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	file, err := tasks.LoadFile(tasks.ResolveLoadOptions("", workspace))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Type, "gulp"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "added 2 tasks") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestAppListShowsGroupInJSON(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("failed to create tasks")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app = NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"list", "--json"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	var items []struct {
		Label         string `json:"label"`
		Group         string `json:"group"`
		WorkspaceRoot string `json:"workspaceRoot"`
		TaskFilePath  string `json:"taskFilePath"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &items); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]string)
	for _, item := range items {
		seen[item.Label] = item.Group
	}
	if got, want := seen["go-build"], "build"; got != want {
		t.Fatalf("group = %q, want %q", got, want)
	}
	if got, want := seen["go-test"], "test"; got != want {
		t.Fatalf("group = %q, want %q", got, want)
	}
	for _, item := range items {
		if item.WorkspaceRoot != workspace {
			t.Fatalf("workspaceRoot = %q, want %q", item.WorkspaceRoot, workspace)
		}
		if item.TaskFilePath != filepath.Join(workspace, ".vscode", "tasks.json") {
			t.Fatalf("taskFilePath = %q", item.TaskFilePath)
		}
	}
}

func TestAppRunDryRunShowsGroup(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("failed to create tasks")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app = NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"run", "--dry-run", "go-build"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "group: build") {
		t.Fatalf("unexpected dry-run output: %q", stdout.String())
	}
}

func TestAppRunDryRunWithoutRunSubcommand(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("failed to create tasks")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app = NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"go-build", "--dry-run"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "group: build") {
		t.Fatalf("unexpected dry-run output: %q", stdout.String())
	}
}

func TestAppRunDryRunWithoutRunSubcommandAllowsFlagsBeforeTaskName(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("failed to create tasks")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app = NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"--dry-run", "go-build"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "group: build") {
		t.Fatalf("unexpected dry-run output: %q", stdout.String())
	}
}

func TestAppRunBuildUsesDefaultBuildTask(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "go.mod"), []byte("module example.com/demo\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := NewApp(strings.NewReader(""), io.Discard, io.Discard)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"add", "go"}); exitCode != 0 {
		t.Fatalf("failed to create tasks")
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app = NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"build", "--dry-run"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "- go-build") {
		t.Fatalf("unexpected dry-run output: %q", stdout.String())
	}
}

func TestAppRunBuildWithoutDefaultShowsCandidates(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasksPath := filepath.Join(workspace, ".vscode", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(tasksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "go-build",
				"type": "process",
				"command": "go",
				"args": ["build", "./..."],
				"group": "build"
			},
			{
				"label": "npm-build",
				"type": "npm",
				"script": "build",
				"group": "build"
			}
		]
	}`
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"build", "--dry-run"}); exitCode != 1 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "no default build task is configured") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "go-build, npm-build") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestAppRunLintUsesUniqueShorthand(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasksPath := filepath.Join(workspace, ".vscode", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(tasksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"version": "2.0.0",
		"tasks": [
			{
				"type": "npm",
				"script": "lint"
			}
		]
	}`
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"lint", "--dry-run"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "- npm-lint") {
		t.Fatalf("unexpected dry-run output: %q", stdout.String())
	}
}

func TestAppRunLintShowsCandidatesWhenAmbiguous(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasksPath := filepath.Join(workspace, ".vscode", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(tasksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "go-lint",
				"type": "process",
				"command": "go",
				"args": ["vet", "./..."]
			},
			{
				"label": "npm-lint",
				"type": "npm",
				"script": "lint"
			}
		]
	}`
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"lint", "--dry-run"}); exitCode != 1 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "task selector is ambiguous: lint") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "go-lint, npm-lint") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestAppRunDryRunJSONShowsProvenance(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasksPath := filepath.Join(workspace, ".vscode", "tasks.json")
	if err := os.MkdirAll(filepath.Dir(tasksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{
		"version": "2.0.0",
		"tasks": [
			{
				"label": "build",
				"identifier": "sample-build",
				"type": "process",
				"command": "go",
				"args": ["build", "./..."],
				"group": "build"
			}
		]
	}`
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	app := NewApp(strings.NewReader(""), &stdout, &stderr)
	app.wd = func() (string, error) { return workspace, nil }
	if exitCode := app.Run([]string{"run", "--json", "--dry-run", "build"}); exitCode != 0 {
		t.Fatalf("exitCode = %d, stderr = %s", exitCode, stderr.String())
	}

	var task struct {
		Label         string `json:"label"`
		Group         string `json:"group"`
		SourceTaskID  string `json:"sourceTaskId"`
		WorkspaceRoot string `json:"workspaceRoot"`
		TaskFilePath  string `json:"taskFilePath"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &task); err != nil {
		t.Fatal(err)
	}
	if task.Label != "build" {
		t.Fatalf("label = %q", task.Label)
	}
	if task.SourceTaskID != "sample-build" {
		t.Fatalf("sourceTaskId = %q", task.SourceTaskID)
	}
	if task.WorkspaceRoot != workspace {
		t.Fatalf("workspaceRoot = %q, want %q", task.WorkspaceRoot, workspace)
	}
	if task.TaskFilePath != tasksPath {
		t.Fatalf("taskFilePath = %q, want %q", task.TaskFilePath, tasksPath)
	}
}
