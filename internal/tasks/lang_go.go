package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

func NewGoTasks(workspaceRoot string, root string) []Task {
	matcher := json.RawMessage(`"$go"`)
	buildTask := newProcessTask("go", "go", "build", root, []string{"build", "-trimpath", "-ldflags=-s -w", "./..."}, matcher, nil)
	tasks := []Task{
		buildTask,
		newProcessTask("go", "go", "test", root, []string{"test", "-v", "./..."}, matcher, nil),
		newProcessTask("go", "go", "bench", root, []string{"test", "-run=^$", "-bench=.", "-benchmem", "./..."}, matcher, nil),
		newProcessTask("go", "go", "cover", root, []string{"test", "-coverprofile=coverage.out", "./..."}, matcher, nil),
		newShellTask("go", "gofmt -l -w . && go vet ./...", "lint", root, matcher),
	}

	commandNames := findGoCommandNames(workspaceRoot, root)
	if len(commandNames) == 0 {
		return tasks
	}

	dependencies := make([]Dependency, 0, len(commandNames))
	for _, commandName := range commandNames {
		commandTask := newGoCommandBuildTask(root, commandName, matcher)
		dependencies = append(dependencies, Dependency{Label: commandTask.Label})
		tasks = append(tasks, commandTask)
	}
	tasks[0].DependsOrder = "parallel"
	tasks[0].Dependencies = DependencyList{Items: dependencies, Set: true}
	return tasks
}

func newGoCommandBuildTask(root string, commandName string, matcher json.RawMessage) Task {
	task := newProcessTask("go", "go", "build", root, []string{"build", "-trimpath", "-ldflags=-s -w", "-o", goCommandOutputPath(commandName), goCommandPackagePath(commandName)}, matcher, nil)
	task.Label = formatToolLabel("go", "build", goCommandTaskDetail(root, commandName))
	task.Group = nil
	return task
}

func goCommandTaskDetail(root string, commandName string) string {
	if root == "" {
		return commandName
	}
	return filepath.ToSlash(filepath.Join(root, commandName))
}

func goCommandOutputPath(commandName string) string {
	return "./bin/" + commandName
}

func goCommandPackagePath(commandName string) string {
	return "./cmd/" + commandName
}

func findGoCommandNames(workspaceRoot string, root string) []string {
	cmdRoot := filepath.Join(workspaceRoot, filepath.FromSlash(normalizeRelative(root)), "cmd")
	entries, err := os.ReadDir(cmdRoot)
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || shouldSkipDir(entry.Name()) {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names
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
	candidates := make([]TaskCandidate, 0, len(roots)*5)
	for _, root := range roots {
		candidates = appendRootTaskCandidates(candidates, "go", NewGoTasks(workspaceRoot, root), candidateDetail(root, "go.mod"))
	}
	return candidates, nil
}
