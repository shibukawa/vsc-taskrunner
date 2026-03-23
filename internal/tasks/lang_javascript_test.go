package tasks

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestNPMScriptsFiltersNamedCandidates(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := `{
		"scripts": {
			"build": "vite build",
			"lint:fix": "eslint . --fix",
			"prebuild": "node prep.js",
			"postdeploy:prod": "node post.js",
			"dev:watch": "vite"
		}
	}`
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := NPMScripts(workspace, "")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scripts, []string{"build", "postdeploy:prod", "prebuild"}; !equalStrings(got, want) {
		t.Fatalf("scripts = %v, want %v", got, want)
	}
}

func TestDetectTaskCandidatesIncludesProviderTasks(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := `module.exports = function (grunt) {
		grunt.registerTask('build', [])
		grunt.registerTask('test', [])
	}`
	if err := os.WriteFile(filepath.Join(workspace, "Gruntfile.js"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates, err := DetectTaskCandidates(workspace)
	if err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for _, candidate := range candidates {
		seen[candidate.Label] = true
	}
	if !seen["grunt-build"] {
		t.Fatal("expected grunt-build candidate")
	}
	if !seen["grunt-test"] {
		t.Fatal("expected grunt-test candidate")
	}
}

func TestDetectTaskCandidatesFiltersNPMScripts(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	content := `{
		"scripts": {
			"build": "vite build",
			"lint:fix": "eslint . --fix",
			"prebuild": "node prep.js",
			"postdeploy:prod": "node post.js",
			"dev:watch": "vite"
		}
	}`
	if err := os.WriteFile(filepath.Join(workspace, "package.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	candidates, err := DetectTaskCandidates(workspace)
	if err != nil {
		t.Fatal(err)
	}
	scripts := make([]string, 0)
	for _, candidate := range candidates {
		if candidate.Ecosystem != "npm" {
			continue
		}
		scripts = append(scripts, candidate.Task.Script)
	}
	sort.Strings(scripts)
	if got, want := scripts, []string{"build", "postdeploy:prod", "prebuild"}; !equalStrings(got, want) {
		t.Fatalf("scripts = %v, want %v", got, want)
	}
}
