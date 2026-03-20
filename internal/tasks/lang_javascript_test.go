package tasks

import (
	"os"
	"path/filepath"
	"testing"
)

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
