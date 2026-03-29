package tasks

import (
	"io"
	"strings"
	"testing"
)

func TestTaskDefinitionCatalogLookupUsesDefaultBuildGroup(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{
			{Label: "go-build", Group: []byte(`{"kind":"build","isDefault":true}`)},
		},
	}

	catalog := BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json")
	lookup := catalog.LookupTask("build")
	if lookup.Label != "go-build" {
		t.Fatalf("label = %q, want go-build", lookup.Label)
	}
}

func TestTaskDefinitionCatalogLookupReturnsAmbiguousActionAliasCandidates(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{
			{Label: "go-lint"},
			{Label: "npm-lint"},
		},
	}

	catalog := BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json")
	lookup := catalog.LookupTask("lint")
	if lookup.Label != "" {
		t.Fatalf("label = %q, want empty", lookup.Label)
	}
	if got, want := strings.Join(lookup.Candidates, ","), "go-lint,npm-lint"; got != want {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
}

func TestResolveTaskSelectionSkipsInputsOutsideSelectedGraph(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Inputs: []Input{
			{ID: "unused", Type: "promptString", Description: "unused"},
		},
		Tasks: []Task{
			{
				Label:   "build",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "ok", Set: true}},
			},
			{
				Label:   "other",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "${input:unused}", Set: true}},
			},
		},
	}

	catalog, err := ResolveTaskSelection(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "build", ResolveOptions{
		WorkspaceRoot:  "/tmp/demo",
		TaskFilePath:   "/tmp/demo/.vscode/tasks.json",
		Inputs:         file.Inputs,
		Stdin:          strings.NewReader(""),
		Stdout:         io.Discard,
		NonInteractive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tasks) != 1 {
		t.Fatalf("resolved tasks = %d, want 1", len(catalog.Tasks))
	}
	if _, ok := catalog.Tasks["other"]; ok {
		t.Fatal("unexpected unrelated task in resolved catalog")
	}
}

func TestResolveTaskSelectionResolvesInputsInsideSelectedGraph(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Inputs: []Input{
			{ID: "target", Type: "promptString", Description: "target"},
		},
		Tasks: []Task{
			{
				Label:   "build",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "ok", Set: true}},
				Dependencies: DependencyList{
					Set:   true,
					Items: []Dependency{{Label: "prepare"}},
				},
			},
			{
				Label:   "prepare",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "${input:target}", Set: true}},
			},
		},
	}

	catalog, err := ResolveTaskSelection(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "build", ResolveOptions{
		WorkspaceRoot: "/tmp/demo",
		TaskFilePath:  "/tmp/demo/.vscode/tasks.json",
		Inputs:        file.Inputs,
		InputValues:   map[string]string{"target": "release"},
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(catalog.Tasks["prepare"].Args, ","), "release"; got != want {
		t.Fatalf("args = %q, want %q", got, want)
	}
}

func TestResolveTaskSelectionSkipsUnsupportedVariablesOutsideSelectedGraph(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{
			{
				Label:   "build",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "ok", Set: true}},
			},
			{
				Label:   "broken",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "${command:python.interpreterPath}", Set: true}},
			},
		},
	}

	_, err := ResolveTaskSelection(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "build", ResolveOptions{
		WorkspaceRoot: "/tmp/demo",
		TaskFilePath:  "/tmp/demo/.vscode/tasks.json",
		Stdin:         strings.NewReader(""),
		Stdout:        io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolveTaskSelectionRejectsUnsupportedVariablesInsideSelectedGraph(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{
			{
				Label:   "build",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "${command:python.interpreterPath}", Set: true}},
			},
		},
	}

	_, err := ResolveTaskSelection(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "build", ResolveOptions{
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

func TestResolveTaskSelectionLabelsReturnsSortedSelectedGraph(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{
			{
				Label: "build",
				Dependencies: DependencyList{
					Set: true,
					Items: []Dependency{
						{Label: "lint"},
						{Label: "prepare"},
					},
				},
			},
			{Label: "prepare"},
			{Label: "lint"},
			{Label: "other"},
		},
	}

	labels, err := ResolveTaskSelectionLabels(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "build", ResolveOptions{
		WorkspaceRoot:  "/tmp/demo",
		TaskFilePath:   "/tmp/demo/.vscode/tasks.json",
		Stdin:          strings.NewReader(""),
		Stdout:         io.Discard,
		NonInteractive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(labels, ","), "build,lint,prepare"; got != want {
		t.Fatalf("labels = %q, want %q", got, want)
	}
}

func TestResolveTaskSelectionLabelsSkipsInputResolution(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Inputs: []Input{
			{ID: "demoMessage", Type: "promptString", Description: "message"},
		},
		Tasks: []Task{
			{
				Label:   "show-input",
				Type:    "process",
				Command: TokenValue{Value: "echo", Set: true},
				Args:    []TokenValue{{Value: "${input:demoMessage}", Set: true}},
			},
		},
	}

	labels, err := ResolveTaskSelectionLabels(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "show-input", ResolveOptions{
		WorkspaceRoot:  "/tmp/demo",
		TaskFilePath:   "/tmp/demo/.vscode/tasks.json",
		Inputs:         file.Inputs,
		NonInteractive: true,
		Stdin:          strings.NewReader(""),
		Stdout:         io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(labels, ","), "show-input"; got != want {
		t.Fatalf("labels = %q, want %q", got, want)
	}
}

func TestResolveTaskSelectionLabelsSkipsUnknownRootTask(t *testing.T) {
	t.Parallel()

	file := &File{
		Version: "2.0.0",
		Tasks: []Task{
			{Label: "build"},
		},
	}

	labels, err := ResolveTaskSelectionLabels(BuildTaskDefinitionCatalog(file, "/tmp/demo", "/tmp/demo/.vscode/tasks.json"), "missing", ResolveOptions{
		WorkspaceRoot:  "/tmp/demo",
		TaskFilePath:   "/tmp/demo/.vscode/tasks.json",
		Stdin:          strings.NewReader(""),
		Stdout:         io.Discard,
		NonInteractive: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) != 0 {
		t.Fatalf("labels = %#v, want empty", labels)
	}
}
