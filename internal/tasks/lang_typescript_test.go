package tasks

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNewTypeScriptTaskSetsBuildGroup(t *testing.T) {
	t.Parallel()

	task := NewTypeScriptTask("tsconfig.json", "build")
	group, ok := ParseTaskGroup(task.Group)
	if !ok || group.Kind != "build" {
		t.Fatalf("group = %+v, want build group", group)
	}
}

func TestAvailableTypeScriptTaskModesUseWorkspaceRootPackageJSON(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"scripts":{"build":"tsc -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "app", "package.json"), []byte(`{"scripts":{"watch":"tsc -w -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	modes, err := AvailableTypeScriptTaskModes(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := modes, []string{"watch"}; !equalStrings(got, want) {
		t.Fatalf("modes = %v, want %v", got, want)
	}
}

func TestDetectTaskCandidatesSuppressesTypeScriptModesProvidedByRootNPM(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(`{"scripts":{"build":"tsc -p ."}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "tsconfig.json"), []byte(`{"compilerOptions":{"target":"ES2022"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates, err := DetectTaskCandidates(workspace)
	if err != nil {
		t.Fatal(err)
	}
	labels := make([]string, 0)
	for _, candidate := range candidates {
		if candidate.Ecosystem != "typescript" {
			continue
		}
		labels = append(labels, candidate.Label)
	}
	sort.Strings(labels)
	if got, want := labels, []string{"tsc-watch-tsconfig.json"}; !equalStrings(got, want) {
		t.Fatalf("labels = %v, want %v", got, want)
	}
}
