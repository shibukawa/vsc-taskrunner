package tasks

import "encoding/json"

func NewGoTasks(root string) []Task {
	return []Task{
		newProcessTask("go", "go", "build", root, []string{"build", "./..."}, json.RawMessage(`"$go"`), nil),
		newProcessTask("go", "go", "test", root, []string{"test", "./..."}, json.RawMessage(`"$go"`), nil),
	}
}

func FindGoModules(workspaceRoot string) ([]string, error) {
	return findRootsByMarker(workspaceRoot, "go.mod")
}

func goProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$go": {
			Owner:    "go",
			Source:   "go",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:  `^(.*):(\d+):(\d+):\s+(.*)$`,
				File:    1,
				Line:    2,
				Column:  3,
				Message: 4,
			}),
		},
	}
}

func collectGoCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	roots, err := FindGoModules(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(roots)*2)
	for _, root := range roots {
		candidates = appendRootTaskCandidates(candidates, "go", NewGoTasks(root), candidateDetail(root, "go.mod"))
	}
	return candidates, nil
}
