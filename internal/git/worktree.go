package git

import (
	"bufio"
	"bytes"
	"fmt"
	"strings"
)

// Worktree represents a single git worktree entry.
type Worktree struct {
	// Path is the absolute filesystem path of the worktree.
	Path string `json:"path"`
	// Branch is the full ref of the checked-out branch, e.g. refs/heads/main.
	// Empty for detached HEAD worktrees.
	Branch string `json:"branch"`
	// CommitHash is the HEAD commit of the worktree.
	CommitHash string `json:"commitHash"`
	// IsMain is true for the main worktree (the original clone).
	IsMain bool `json:"isMain"`
}

// AddWorktree creates a new worktree at worktreePath checked out to a new branch
// created from startPoint. It runs
// `git worktree add -b <branchName> <worktreePath> <startPoint>`.
func AddWorktree(repoRoot, worktreePath, branchName, startPoint string) error {
	if _, err := runGit(repoRoot, "worktree", "add", "-b", branchName, worktreePath, startPoint); err != nil {
		return fmt.Errorf("git worktree add: %w", err)
	}
	return nil
}

// RemoveWorktree removes the worktree at worktreePath and unregisters it.
// It runs `git worktree remove --force <worktreePath>`.
func RemoveWorktree(repoRoot, worktreePath string) error {
	if _, err := runGit(repoRoot, "worktree", "remove", "--force", worktreePath); err != nil {
		return fmt.Errorf("git worktree remove: %w", err)
	}
	return nil
}

// DeleteBranch removes a local branch after its worktree has been removed.
func DeleteBranch(repoRoot, branchName string) error {
	if _, err := runGit(repoRoot, "branch", "-D", branchName); err != nil {
		return fmt.Errorf("git branch -D: %w", err)
	}
	return nil
}

// ListWorktrees returns all worktrees for the repository (main + linked).
// It parses `git worktree list --porcelain` output.
func ListWorktrees(repoRoot string) ([]Worktree, error) {
	out, err := runGit(repoRoot, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("git worktree list: %w", err)
	}
	return parseWorktreeList(out), nil
}

func parseWorktreeList(data []byte) []Worktree {
	var result []Worktree
	var current Worktree
	isFirst := true

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Empty line separates worktree entries
			if current.Path != "" {
				if isFirst {
					current.IsMain = true
					isFirst = false
				}
				result = append(result, current)
				current = Worktree{}
			}
			continue
		}

		if after, ok := cutPrefix(line, "worktree "); ok {
			current.Path = after
		} else if after, ok := cutPrefix(line, "HEAD "); ok {
			current.CommitHash = after
		} else if after, ok := cutPrefix(line, "branch "); ok {
			current.Branch = after
		}
	}

	// Last entry (no trailing blank line)
	if current.Path != "" {
		if isFirst {
			current.IsMain = true
		}
		result = append(result, current)
	}

	return result
}

func cutPrefix(s, prefix string) (string, bool) {
	if strings.HasPrefix(s, prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
