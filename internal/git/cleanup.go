package git

import (
	"fmt"
	"strings"
)

// PruneWorktreeAdmin runs `git worktree prune` to remove stale admin entries.
func PruneWorktreeAdmin(repoRoot string) error {
	if _, err := runGit(repoRoot, "worktree", "prune"); err != nil {
		return fmt.Errorf("git worktree prune: %w", err)
	}
	return nil
}

// CleanupRuntimeBranches deletes local branches under the given prefix that are
// not currently attached to any worktree.
func CleanupRuntimeBranches(repoRoot, prefix string) error {
	worktrees, err := ListWorktrees(repoRoot)
	if err != nil {
		return err
	}
	activeBranches := make(map[string]struct{}, len(worktrees))
	for _, wt := range worktrees {
		if wt.Branch == "" {
			continue
		}
		activeBranches[wt.Branch] = struct{}{}
	}

	out, err := runGit(repoRoot, "for-each-ref", "--format=%(refname)", "refs/heads/"+prefix)
	if err != nil {
		return fmt.Errorf("list runtime branches: %w", err)
	}
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		ref := strings.TrimSpace(line)
		if ref == "" {
			continue
		}
		if _, ok := activeBranches[ref]; ok {
			continue
		}
		branch := strings.TrimPrefix(ref, "refs/heads/")
		if err := DeleteBranch(repoRoot, branch); err != nil {
			return err
		}
	}
	return nil
}
