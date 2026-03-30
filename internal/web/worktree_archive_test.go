package web

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestWorktreeArchiveHelpersRespectIgnoreRules(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	write := func(rel string, content string) {
		t.Helper()
		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write(".runtaskignore", "node_modules/\n*.tmp\n!important.tmp\n")
	write("src/app.js", "console.log('ok')\n")
	write("debug.tmp", "tmp\n")
	write("important.tmp", "keep\n")
	write("node_modules/pkg/index.js", "ignored\n")
	write(".git/config", "ignored\n")

	files, err := listVisibleWorktreeFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, file := range files {
		got[file] = true
	}
	for _, want := range []string{".runtaskignore", "important.tmp", "src/app.js"} {
		if !got[want] {
			t.Fatalf("expected visible file %s", want)
		}
	}
	for _, hidden := range []string{"debug.tmp", "node_modules/pkg/index.js", ".git/config"} {
		if got[hidden] {
			t.Fatalf("did not expect visible file %s", hidden)
		}
	}

	if _, err := readVisibleWorktreeFile(root, "node_modules/pkg/index.js"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("excluded file read err = %v, want not exist", err)
	}
	data, err := readVisibleWorktreeFile(root, "src/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "console.log('ok')\n" {
		t.Fatalf("visible file contents = %q", string(data))
	}

	archiveFile, err := createWorktreeArchiveTemp(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = archiveFile.Close()
		_ = os.Remove(archiveFile.Name())
	}()
	archiveData, err := os.ReadFile(archiveFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	archiveFiles, err := listWorktreeArchiveFiles(archiveData)
	if err != nil {
		t.Fatal(err)
	}
	archived := map[string]bool{}
	for _, file := range archiveFiles {
		archived[file] = true
	}
	for _, want := range []string{".runtaskignore", "important.tmp", "src/app.js"} {
		if !archived[want] {
			t.Fatalf("expected archived file %s", want)
		}
	}
	for _, hidden := range []string{"debug.tmp", "node_modules/pkg/index.js", ".git/config"} {
		if archived[hidden] {
			t.Fatalf("did not expect archived file %s", hidden)
		}
	}
	content, err := readWorktreeArchiveFile(archiveData, "src/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "console.log('ok')\n" {
		t.Fatalf("archived file contents = %q", string(content))
	}
	if _, err := readWorktreeArchiveFile(archiveData, "../escape"); err == nil {
		t.Fatal("expected invalid archive path error")
	}
}
