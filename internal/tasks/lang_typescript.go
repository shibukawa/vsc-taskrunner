package tasks

import (
	"path/filepath"
	"sort"
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

func AvailableTypeScriptTaskModes(workspaceRoot string) ([]string, error) {
	scripts, err := readWorkspaceRootPackageScripts(workspaceRoot)
	if err != nil {
		return nil, err
	}
	available := make([]string, 0, 2)
	for _, mode := range []string{"build", "watch"} {
		if _, exists := scripts[mode]; exists {
			continue
		}
		available = append(available, mode)
	}
	sort.Strings(available)
	return available, nil
}

func collectTypeScriptCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	tsconfigs, err := FindTypeScriptConfigs(workspaceRoot)
	if err != nil {
		return nil, err
	}
	availableModes, err := AvailableTypeScriptTaskModes(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(tsconfigs)*len(availableModes))
	for _, tsconfig := range tsconfigs {
		for _, option := range availableModes {
			task := NewTypeScriptTask(tsconfig, option)
			candidates = append(candidates, newTaskCandidate("typescript", inferTaskLabel(task, workspaceRoot), task, candidateDetail(pathFromFile(tsconfig), tsconfig)))
		}
	}
	return candidates, nil
}
