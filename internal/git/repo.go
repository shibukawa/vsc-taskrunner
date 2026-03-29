package git

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// FindRepoRoot returns the root directory of the git repository containing path.
func FindRepoRoot(path string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = path
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return "", fmt.Errorf("git executable not found; this runtime needs git installed to inspect repositories")
		}
		return "", fmt.Errorf("not a git repository (or any of the parent directories): %s", path)
	}
	return strings.TrimSpace(string(out)), nil
}

// IsGitRepo reports whether path is inside a git repository.
func IsGitRepo(path string) bool {
	_, err := FindRepoRoot(path)
	return err == nil
}

// runGit runs a git command in the given directory and returns stdout.
func runGit(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("git executable not found; this runtime needs git installed to run repository operations")
		}
		return nil, fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, stderr.String())
	}
	return stdout.Bytes(), nil
}
