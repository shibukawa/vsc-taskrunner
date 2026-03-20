package tasks

import (
	"path/filepath"
	"strings"
)

func NewTypeScriptTask(tsconfig string, option string) Task {
	task := Task{
		Type:     "typescript",
		TSConfig: normalizeRelative(tsconfig),
		Option:   option,
		Label:    typescriptLabel(normalizeRelative(tsconfig), option),
	}
	if option == "build" {
		task.Group = MustTaskGroup("build", false)
	}
	return task
}

func FindTypeScriptConfigs(workspaceRoot string) ([]string, error) {
	return findWorkspaceFiles(workspaceRoot, func(relativePath string) bool {
		name := filepath.Base(relativePath)
		return strings.HasPrefix(name, "tsconfig") && strings.HasSuffix(name, ".json")
	})
}

func typescriptProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$tsc": {
			Owner:    "typescript",
			Source:   "tsc",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^([^\s].*)\((\d+|\d+,\d+|\d+,\d+,\d+,\d+)\):\s+(error|warning|info)\s+(TS\d+)\s*:\s*(.*)$`,
				File:     1,
				Location: 2,
				Severity: 3,
				Code:     4,
				Message:  5,
			}),
		},
		"$tsc-watch": {
			Base: "$tsc",
		},
	}
}

func collectTypeScriptCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	tsconfigs, err := FindTypeScriptConfigs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(tsconfigs)*2)
	for _, tsconfig := range tsconfigs {
		for _, option := range []string{"build", "watch"} {
			task := NewTypeScriptTask(tsconfig, option)
			candidates = append(candidates, newTaskCandidate("typescript", inferTaskLabel(task, workspaceRoot), task, candidateDetail(pathFromFile(tsconfig), tsconfig)))
		}
	}
	return candidates, nil
}
