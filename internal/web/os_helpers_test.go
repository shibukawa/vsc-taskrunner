package web

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithExclusiveFileLockRunsCallback(t *testing.T) {
	t.Parallel()

	lockPath := filepath.Join(t.TempDir(), "locks", "history.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		t.Fatal(err)
	}

	called := false
	if err := withExclusiveFileLock(lockPath, "open test lock", "lock test lock", func() error {
		called = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if !called {
		t.Fatal("expected callback to run")
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("expected lock file to exist: %v", err)
	}
}

func TestWithExclusiveFileLockWrapsOpenErrors(t *testing.T) {
	t.Parallel()

	base := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(base, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := withExclusiveFileLock(filepath.Join(base, "lock"), "open test lock", "lock test lock", func() error {
		t.Fatal("callback should not run")
		return nil
	})
	if err == nil {
		t.Fatal("expected open error")
	}
	if !strings.Contains(err.Error(), "open test lock") {
		t.Fatalf("error %q did not include open label", err)
	}
}

func TestFreeBytesForPath(t *testing.T) {
	t.Parallel()

	if got := freeBytesForPath(""); got != 0 {
		t.Fatalf("freeBytesForPath(\"\") = %d, want 0", got)
	}

	if got := freeBytesForPath(t.TempDir()); got == 0 {
		t.Fatal("expected free bytes for temp dir to be greater than zero")
	}
}
