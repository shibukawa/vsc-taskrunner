package tasks

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveFileExpandsVariablesAndInputs(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Inputs: []Input{{
			ID:          "target",
			Type:        "promptString",
			Description: "target",
			Default:     "./...",
		}},
		Tasks: []Task{{
			Label:   "test",
			Type:    "process",
			Command: TokenValue{Value: "go", Set: true},
			Args: []TokenValue{
				{Value: "test", Set: true},
				{Value: "${input:target}", Set: true},
				{Value: "${workspaceFolderBasename}", Set: true},
			},
			Options: &Options{
				CWD: "${workspaceFolder}",
				Env: map[string]string{"MODE": "${env:MODE}"},
			},
		}},
	}

	catalog, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: "/tmp/demo",
		TaskFilePath:  "/tmp/demo/.vscode/tasks.json",
		InputValues:   map[string]string{"target": "./internal/..."},
		Env:           []string{"MODE=ci"},
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := catalog.Tasks["test"]
	if task.Options.CWD != "/tmp/demo" {
		t.Fatalf("cwd = %q, want /tmp/demo", task.Options.CWD)
	}
	if task.Options.Env["MODE"] != "ci" {
		t.Fatalf("env MODE = %q, want ci", task.Options.Env["MODE"])
	}
	if got, want := strings.Join(task.Args, ","), "test,./internal/...,demo"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestResolveFileRejectsUnsupportedVariables(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{{
			Label:   "broken",
			Type:    "process",
			Command: TokenValue{Value: "echo", Set: true},
			Args:    []TokenValue{{Value: "${command:python.interpreterPath}", Set: true}},
		}},
	}

	_, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: "/tmp/demo",
		TaskFilePath:  "/tmp/demo/.vscode/tasks.json",
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported variable") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveFileTracksRedactedDisplayValues(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Inputs: []Input{{
			ID:   "client_secret",
			Type: "promptString",
		}},
		Tasks: []Task{{
			Label:   "deploy",
			Type:    "process",
			Command: TokenValue{Value: "aws", Set: true},
			Args: []TokenValue{
				{Value: "deploy", Set: true},
				{Value: "--token=${env:AWS_SECRET_ACCESS_KEY}", Set: true},
				{Value: "--secret=${input:client_secret}", Set: true},
				{Value: "--safe=${env:MONKEY}", Set: true},
			},
		}},
	}

	catalog, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: "/tmp/demo",
		TaskFilePath:  "/tmp/demo/.vscode/tasks.json",
		InputValues:   map[string]string{"client_secret": "input-secret"},
		Redaction:     DefaultRedactionPolicy(),
		Env:           []string{"AWS_SECRET_ACCESS_KEY=env-secret", "MONKEY=banana"},
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := catalog.Tasks["deploy"]
	if got, want := task.Args[1], "--token=env-secret"; got != want {
		t.Fatalf("arg[1] = %q, want %q", got, want)
	}
	if got, want := task.DisplayArgs[1], "--token=***"; got != want {
		t.Fatalf("display arg[1] = %q, want %q", got, want)
	}
	if got, want := task.DisplayArgs[2], "--secret=***"; got != want {
		t.Fatalf("display arg[2] = %q, want %q", got, want)
	}
	if got, want := task.DisplayArgs[3], "--safe=banana"; got != want {
		t.Fatalf("display arg[3] = %q, want %q", got, want)
	}
}

func TestResolveFileResolvesNPMTask(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"packageManager":"pnpm@9.0.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "pnpm-lock.yaml"), []byte("lockfileVersion: '9.0'\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{{
			Type:   "npm",
			Script: "lint",
		}},
	}

	catalog, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: workspace,
		TaskFilePath:  filepath.Join(workspace, ".vscode", "tasks.json"),
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := catalog.Tasks["npm-lint"]
	if got, want := task.Command, "pnpm"; got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if got, want := strings.Join(task.Args, " "), "run lint"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	if got, want := task.Options.CWD, workspace; got != want {
		t.Fatalf("cwd = %q, want %q", got, want)
	}
}

func TestResolveFileResolvesTypeScriptWatchTask(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "node_modules", ".bin", "tsc"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"references":[{"path":"./pkg"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{{
			Type:     "typescript",
			TSConfig: "tsconfig.json",
			Option:   "watch",
		}},
	}

	catalog, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: workspace,
		TaskFilePath:  filepath.Join(workspace, ".vscode", "tasks.json"),
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := catalog.Tasks["tsc-watch-tsconfig.json"]
	if got, want := task.Command, filepath.Join(".", "node_modules", ".bin", "tsc"); got != want {
		t.Fatalf("command = %q, want %q", got, want)
	}
	if got, want := strings.Join(task.Args, " "), "-b "+filepath.Join(workspace, "tsconfig.json")+" --watch"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
	if string(task.ProblemMatcher) != `"$tsc-watch"` {
		t.Fatalf("problemMatcher = %s", string(task.ProblemMatcher))
	}
	if !task.IsBackground {
		t.Fatal("watch task should be background")
	}
}

func TestResolveFileResolvesGulpTask(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, "node_modules", ".bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "node_modules", ".bin", "gulp"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{{
			Type:         "gulp",
			ProviderTask: "build",
			ProviderFile: "build/gulpfile.js",
		}},
	}

	catalog, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: workspace,
		TaskFilePath:  filepath.Join(workspace, ".vscode", "tasks.json"),
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}

	task := catalog.Tasks["gulp-build"]
	if got, want := strings.Join(task.Args, " "), "--gulpfile build/gulpfile.js build"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestResolveFileRejectsUnsupportedProviderType(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{{
			Label: "custom",
			Type:  "maven",
		}},
	}

	_, err := ResolveFile(file, ResolveOptions{
		WorkspaceRoot: "/tmp/demo",
		TaskFilePath:  "/tmp/demo/.vscode/tasks.json",
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unsupported provider type") {
		t.Fatalf("unexpected error: %v", err)
	}
}
