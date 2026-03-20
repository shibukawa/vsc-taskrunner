package tasks

import "encoding/json"

func NewCargoTasks(root string) []Task {
	return []Task{
		newProcessTask("cargo", "cargo", "build", root, []string{"build"}, json.RawMessage(`["$cargo","$cargo-panic"]`), nil),
		newProcessTask("cargo", "cargo", "test", root, []string{"test"}, json.RawMessage(`["$cargo","$cargo-panic"]`), nil),
	}
}

func FindCargoProjects(workspaceRoot string) ([]string, error) {
	return findRootsByMarker(workspaceRoot, "Cargo.toml")
}

func rustProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$cargo": {
			Owner:    "rust",
			Source:   "cargo",
			Severity: "error",
			Pattern: mustMarshal([]patternConfig{
				{
					Regexp:   `^(error|warning)(?:\[(E\d+)\])?:\s+(.*)$`,
					Severity: 1,
					Code:     2,
					Message:  3,
				},
				{
					Regexp: `^\s*(?:-->|at)\s+(.+):(\d+):(\d+)`,
					File:   1,
					Line:   2,
					Column: 3,
				},
			}),
		},
		"$cargo-panic": {
			Owner:    "rust",
			Source:   "cargo",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:  `^thread '.*' panicked at (?:.*?, )?(.+):(\d+):(\d+):\s*(.*)$`,
				File:    1,
				Line:    2,
				Column:  3,
				Message: 4,
			}),
		},
	}
}

func collectRustCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	roots, err := FindCargoProjects(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(roots)*2)
	for _, root := range roots {
		candidates = appendRootTaskCandidates(candidates, "rust", NewCargoTasks(root), candidateDetail(root, "Cargo.toml"))
	}
	return candidates, nil
}
