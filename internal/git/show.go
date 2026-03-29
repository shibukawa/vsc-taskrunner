package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// newContextCmd creates an exec.Cmd for a git command in dir with context support.
func newContextCmd(ctx context.Context, dir string, args ...string) *exec.Cmd {
	allArgs := append([]string{}, args...)
	cmd := exec.CommandContext(ctx, "git", allArgs...)
	cmd.Dir = dir
	return cmd
}

// ShowFile returns the content of filePath at the given branch/ref without
// checking out a working tree. It runs `git show <ref>:<filePath>`.
func ShowFile(repoRoot, ref, filePath string) ([]byte, error) {
	object := fmt.Sprintf("%s:%s", ref, filePath)
	out, err := runGit(repoRoot, "show", object)
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", object, err)
	}
	return out, nil
}

// CurrentCommitHash returns the current HEAD commit hash for repoRoot/worktree.
func CurrentCommitHash(repoRoot string) (string, error) {
	out, err := runGit(repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
