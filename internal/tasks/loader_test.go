package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadFileSupportsJSONCAndDefaults(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasksDir := filepath.Join(workspace, ".vscode")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
		t.Fatal(err)
	}

	content := `{
		// comment
		"version": "2.0.0",
		"windows": {
			"options": {
				"cwd": "C:/tmp"
			}
		},
		"tasks": [
			{
				"taskName": "build",
				"command": "go",
				"args": ["test", "./..."],
				"type": "process"
			}
		]
	}`

	tasksPath := filepath.Join(tasksDir, "tasks.json")
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := LoadFile(ResolveLoadOptions(tasksPath, workspace))
	if err != nil {
		t.Fatal(err)
	}

	if got := file.Tasks[0].Label; got != "build" {
		t.Fatalf("label = %q, want build", got)
	}
	if got := file.Tasks[0].EffectiveType(); got != "process" {
		t.Fatalf("type = %q, want process", got)
	}
}

func TestLoadFileInfersProviderTaskLabel(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	tasksDir := filepath.Join(workspace, ".vscode")
	if err := os.MkdirAll(tasksDir, 0o755); err != nil {
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

	tasksPath := filepath.Join(tasksDir, "tasks.json")
	if err := os.WriteFile(tasksPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	file, err := LoadFile(ResolveLoadOptions(tasksPath, workspace))
	if err != nil {
		t.Fatal(err)
	}

	if got, want := file.Tasks[0].Label, "npm-lint"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
}
