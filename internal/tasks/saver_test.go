package tasks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveTaskCreatesNewFile(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	options := ResolveLoadOptions("", workspace)
	task := Task{
		Label:   "build",
		Type:    "process",
		Command: TokenValue{Value: "go", Set: true},
		Args: []TokenValue{
			{Value: "test", Set: true},
			{Value: "./...", Set: true},
		},
	}

	if err := SaveTask(options, task); err != nil {
		t.Fatal(err)
	}

	file, err := LoadFile(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if file.Tasks[0].Label != "build" {
		t.Fatalf("label = %q", file.Tasks[0].Label)
	}
}

func TestSaveTaskAppendsToExistingHuJSON(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	options := ResolveLoadOptions("", workspace)
	if err := os.MkdirAll(filepath.Dir(options.Path), 0o755); err != nil {
		t.Fatal(err)
	}

	content := `{
	  // existing tasks
	  "version": "2.0.0",
	  "tasks": [
	    {
	      "label": "lint",
	      "type": "process",
	      "command": "go",
	      "args": ["vet", "./..."]
	    }
	  ]
	}`
	if err := os.WriteFile(options.Path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := SaveTask(options, Task{
		Label:   "test",
		Type:    "process",
		Command: TokenValue{Value: "go", Set: true},
		Args:    []TokenValue{{Value: "test", Set: true}, {Value: "./...", Set: true}},
	}); err != nil {
		t.Fatal(err)
	}

	file, err := LoadFile(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}

	raw, err := os.ReadFile(options.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "existing tasks") {
		t.Fatalf("expected comment to be preserved, got %q", string(raw))
	}
}

func TestSaveTaskRejectsDuplicateLabel(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	options := ResolveLoadOptions("", workspace)
	if err := SaveTask(options, Task{
		Label:   "build",
		Type:    "shell",
		Command: TokenValue{Value: "go", Set: true},
		Args:    []TokenValue{{Value: "test", Set: true}},
	}); err != nil {
		t.Fatal(err)
	}

	err := SaveTask(options, Task{
		Label:   "build",
		Type:    "shell",
		Command: TokenValue{Value: "echo", Set: true},
	})
	if err == nil {
		t.Fatal("expected duplicate label error")
	}
	if !strings.Contains(err.Error(), "task already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveTaskSupportsProviderType(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	options := ResolveLoadOptions("", workspace)

	if err := SaveTask(options, Task{Type: "npm", Script: "test"}); err != nil {
		t.Fatal(err)
	}

	file, err := LoadFile(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(file.Tasks))
	}
	if got, want := file.Tasks[0].Label, "npm-test"; got != want {
		t.Fatalf("label = %q, want %q", got, want)
	}
	if got, want := file.Tasks[0].Type, "npm"; got != want {
		t.Fatalf("type = %q, want %q", got, want)
	}
}

func TestSaveTasksCreatesMultipleTasks(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	options := ResolveLoadOptions("", workspace)

	if err := SaveTasks(options, []Task{{Type: "npm", Script: "build"}, {Type: "npm", Script: "test"}}); err != nil {
		t.Fatal(err)
	}

	file, err := LoadFile(options)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(file.Tasks))
	}
}

func TestSaveTaskOmitsUnsetStructuredFields(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	options := ResolveLoadOptions("", workspace)

	if err := SaveTask(options, Task{Type: "npm", Script: "test"}); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(options.Path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw)
	if strings.Contains(content, `"command": null`) {
		t.Fatalf("unexpected null command field: %s", content)
	}
	if strings.Contains(content, `"dependsOn": null`) {
		t.Fatalf("unexpected null dependsOn field: %s", content)
	}
}
