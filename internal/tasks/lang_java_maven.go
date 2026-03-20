package tasks

import (
	"encoding/json"
	"path/filepath"
)

func NewMavenTasks(workspaceRoot string, root string) []Task {
	command := "mvn"
	windows := (*TaskDefaults)(nil)
	if fileExists(filepath.Join(workspaceRoot, filepath.FromSlash(root), "mvnw")) {
		command = "./mvnw"
		windows = &TaskDefaults{Command: TokenValue{Value: "mvnw.cmd", Set: true}}
	}
	return []Task{
		newProcessTask("maven", command, "build", root, []string{"package"}, json.RawMessage(`["$maven","$maven-kotlin"]`), windows),
		newProcessTask("maven", command, "test", root, []string{"test"}, json.RawMessage(`["$maven","$maven-kotlin"]`), windows),
	}
}

func FindMavenProjects(workspaceRoot string) ([]string, error) {
	return findRootsByAnyMarker(workspaceRoot, []string{"mvnw", "pom.xml"})
}

func mavenDetailFile(workspaceRoot string, root string) string {
	rootPath := filepath.Join(workspaceRoot, filepath.FromSlash(root))
	for _, name := range []string{"mvnw", "pom.xml"} {
		if fileExists(filepath.Join(rootPath, name)) {
			return name
		}
	}
	return "pom.xml"
}

func mavenProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$maven": {
			Owner:    "maven",
			Source:   "maven",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^\[(ERROR|WARNING|INFO)\]\s+(?:\[[^\]]+\]\s+)?(.+):\[(\d+),(\d+)\]\s+(?:(\w+\d+):\s+)?(.*)$`,
				Severity: 1,
				File:     2,
				Line:     3,
				Column:   4,
				Code:     5,
				Message:  6,
			}),
		},
		"$maven-kotlin": {
			Owner:    "maven",
			Source:   "kotlin",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^\[(ERROR|WARNING|INFO)\]\s+(?:file://)?(.+?):\s+\((\d+),\s*(\d+)\)\s+(.*)$`,
				Severity: 1,
				File:     2,
				Line:     3,
				Column:   4,
				Message:  5,
			}),
		},
	}
}

func collectMavenCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	roots, err := FindMavenProjects(workspaceRoot)
	if err != nil {
		return nil, err
	}
	candidates := make([]TaskCandidate, 0, len(roots)*2)
	for _, root := range roots {
		candidates = appendRootTaskCandidates(candidates, "maven", NewMavenTasks(workspaceRoot, root), candidateDetail(root, mavenDetailFile(workspaceRoot, root)))
	}
	return candidates, nil
}
