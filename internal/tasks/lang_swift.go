package tasks

import "encoding/json"

func NewSwiftTasks(root string) []Task {
	return []Task{
		newProcessTask("swift", "swift", "build", root, []string{"build"}, json.RawMessage(`"$swift"`), nil),
		newProcessTask("swift", "swift", "test", root, []string{"test"}, json.RawMessage(`"$swift"`), nil),
	}
}

func FindSwiftPackages(workspaceRoot string) ([]string, error) {
	return findRootsByMarker(workspaceRoot, "Package.swift")
}

func swiftProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$swift": {
			Owner:    "swift",
			Source:   "swift",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^(.*):(\d+):(\d+):\s+(error|warning|note):\s+(.*)$`,
				File:     1,
				Line:     2,
				Column:   3,
				Severity: 4,
				Message:  5,
			}),
		},
	}
}

func collectSwiftCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	roots, err := FindSwiftPackages(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(roots)*2)
	for _, root := range roots {
		candidates = appendRootTaskCandidates(candidates, "swift", NewSwiftTasks(root), candidateDetail(root, "Package.swift"))
	}
	return candidates, nil
}
