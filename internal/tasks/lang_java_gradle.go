package tasks

import (
	"encoding/json"
	"path/filepath"
)

func NewGradleTasks(workspaceRoot string, root string) []Task {
	command := "gradle"
	windows := (*TaskDefaults)(nil)
	if fileExists(filepath.Join(workspaceRoot, filepath.FromSlash(root), "gradlew")) {
		command = "./gradlew"
		windows = &TaskDefaults{Command: TokenValue{Value: "gradlew.bat", Set: true}}
	}
	return []Task{
		newProcessTask("gradle", command, "build", root, []string{"build"}, json.RawMessage(`["$gradle","$gradle-kotlin"]`), windows),
		newProcessTask("gradle", command, "test", root, []string{"test"}, json.RawMessage(`["$gradle","$gradle-kotlin"]`), windows),
		newProcessTask("gradle", command, "clean", root, []string{"clean"}, json.RawMessage(`["$gradle","$gradle-kotlin"]`), windows),
		newProcessTask("gradle", command, "lint", root, []string{"check"}, json.RawMessage(`["$gradle","$gradle-kotlin"]`), windows),
	}
}

func FindGradleProjects(workspaceRoot string) ([]string, error) {
	return findRootsByAnyMarker(workspaceRoot, []string{"gradlew", "build.gradle", "build.gradle.kts"})
}

func gradleDetailFile(workspaceRoot string, root string) string {
	rootPath := filepath.Join(workspaceRoot, filepath.FromSlash(root))
	for _, name := range []string{"gradlew", "build.gradle.kts", "build.gradle"} {
		if fileExists(filepath.Join(rootPath, name)) {
			return name
		}
	}
	return "build.gradle"
}

func gradleProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$gradle": {
			Owner:    "gradle",
			Source:   "gradle",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^(?:\[[^\]]+\]\s+)?(.*?)(?::(\d+)(?::(\d+))?)?:\s+((?:fatal +)?error|warning|info)(?:\s+([A-Za-z][A-Za-z0-9_]*\d+))?:\s+(.*)$`,
				File:     1,
				Line:     2,
				Column:   3,
				Severity: 4,
				Code:     5,
				Message:  6,
			}),
		},
		"$gradle-kotlin": {
			Owner:    "gradle",
			Source:   "kotlin",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^([ew]):\s+(?:file://)?(.+?):\s+\((\d+),\s*(\d+)\):\s+(.*)$`,
				Severity: 1,
				File:     2,
				Line:     3,
				Column:   4,
				Message:  5,
			}),
		},
	}
}

func collectGradleCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	roots, err := FindGradleProjects(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(roots)*4)
	for _, root := range roots {
		candidates = appendRootTaskCandidates(candidates, "gradle", NewGradleTasks(workspaceRoot, root), candidateDetail(root, gradleDetailFile(workspaceRoot, root)))
	}
	return candidates, nil
}
