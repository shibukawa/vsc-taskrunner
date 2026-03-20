package tasks

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

func newProcessTask(toolLabel string, command string, action string, root string, args []string, matcher json.RawMessage, windows *TaskDefaults) Task {
	task := Task{
		Label:   formatToolLabel(toolLabel, action, root),
		Type:    "process",
		Command: TokenValue{Value: command, Set: true},
		Args:    stringsToTokens(args),
		Options: &Options{CWD: workspaceCWD(root)},
		Windows: windows,
		Group:   taskGroup(action),
	}
	if len(matcher) > 0 {
		task.ProblemMatcher = cloneBytes(matcher)
	}
	return task
}

func taskGroup(action string) json.RawMessage {
	switch action {
	case "build", "watch":
		return MustTaskGroup("build", false)
	case "test":
		return MustTaskGroup("test", false)
	default:
		return nil
	}
}

func commandLabel(command string) string {
	trimmed := strings.TrimPrefix(command, "./")
	trimmed = strings.TrimSuffix(trimmed, ".bat")
	return strings.TrimSuffix(trimmed, ".cmd")
}

func formatToolLabel(tool string, action string, root string) string {
	return canonicalTaskLabel(tool, action, root)
}

func workspaceCWD(root string) string {
	root = normalizeRelative(root)
	if root == "" {
		return "${workspaceFolder}"
	}
	return "${workspaceFolder}/" + root
}

func candidateDetail(root string, marker string) string {
	root = normalizeRelative(root)
	if root == "" {
		return marker
	}
	return root + "/" + marker
}

func pathFromFile(relativePath string) string {
	return normalizeRelative(filepath.ToSlash(filepath.Dir(relativePath)))
}

func findRootsByMarker(workspaceRoot string, marker string) ([]string, error) {
	return findRootsByAnyMarker(workspaceRoot, []string{marker})
}

func findRootsByAnyMarker(workspaceRoot string, markers []string) ([]string, error) {
	markerSet := make(map[string]bool, len(markers))
	for _, marker := range markers {
		markerSet[marker] = true
	}
	seen := make(map[string]bool)
	files, err := findWorkspaceFiles(workspaceRoot, func(relativePath string) bool {
		return markerSet[filepath.Base(relativePath)]
	})
	if err != nil {
		return nil, err
	}
	roots := make([]string, 0, len(files))
	for _, file := range files {
		root := pathFromFile(file)
		if !seen[root] {
			seen[root] = true
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots, nil
}

func findWorkspaceFiles(workspaceRoot string, match func(relativePath string) bool) ([]string, error) {
	results := make([]string, 0)
	err := filepath.WalkDir(workspaceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() {
			if shouldSkipDir(name) {
				return filepath.SkipDir
			}
			return nil
		}
		relative, err := filepath.Rel(workspaceRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if match(relative) {
			results = append(results, normalizeRelative(relative))
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(results)
	return results, nil
}

func shouldSkipDir(name string) bool {
	return name == ".git" || name == "node_modules"
}
