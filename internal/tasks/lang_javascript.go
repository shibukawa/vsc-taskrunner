package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
)

type ProviderTaskDefinition struct {
	Task string `json:"task"`
	File string `json:"file,omitempty"`
}

func NewNPMTask(script string, packageDir string) Task {
	return Task{
		Type:   "npm",
		Script: script,
		Path:   normalizeRelative(packageDir),
	}
}

func FindNPMPackages(workspaceRoot string) ([]string, error) {
	files, err := findWorkspaceFiles(workspaceRoot, func(relativePath string) bool {
		return filepath.Base(relativePath) == "package.json"
	})
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(files))
	for _, file := range files {
		result = append(result, pathFromFile(file))
	}
	return result, nil
}

func NPMScripts(workspaceRoot string, packageDir string) ([]string, error) {
	packagePath := filepath.Join(workspaceRoot, filepath.FromSlash(normalizeRelative(packageDir)), "package.json")
	content, err := os.ReadFile(packagePath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", packagePath, err)
	}
	standardized, err := hujson.Standardize(content)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", packagePath, err)
	}
	var raw struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(standardized, &raw); err != nil {
		return nil, fmt.Errorf("decode %s: %w", packagePath, err)
	}
	items := make([]string, 0, len(raw.Scripts))
	for name := range raw.Scripts {
		items = append(items, name)
	}
	sort.Strings(items)
	return items, nil
}

func NewProviderTask(taskType string, providerTask string, providerFile string) Task {
	task := Task{
		Type:         taskType,
		ProviderTask: providerTask,
		ProviderFile: normalizeRelative(providerFile),
	}
	task.Label = inferTaskLabel(task, "")
	return task
}

func FindProviderTasks(workspaceRoot string, taskType string) ([]ProviderTaskDefinition, error) {
	files, err := FindProviderFiles(workspaceRoot, taskType)
	if err != nil {
		return nil, err
	}
	definitions := make([]ProviderTaskDefinition, 0)
	seen := make(map[string]bool)
	for _, file := range files {
		tasksInFile, err := parseProviderTasks(filepath.Join(workspaceRoot, filepath.FromSlash(file)), taskType)
		if err != nil {
			return nil, err
		}
		for _, name := range tasksInFile {
			key := file + "\x00" + name
			if seen[key] {
				continue
			}
			seen[key] = true
			definitions = append(definitions, ProviderTaskDefinition{Task: name, File: file})
		}
	}
	sort.Slice(definitions, func(i, j int) bool {
		if definitions[i].Task != definitions[j].Task {
			return definitions[i].Task < definitions[j].Task
		}
		return definitions[i].File < definitions[j].File
	})
	return definitions, nil
}

func FindProviderFiles(workspaceRoot string, taskType string) ([]string, error) {
	patterns, err := providerFilePatterns(taskType)
	if err != nil {
		return nil, err
	}
	patternSet := make(map[string]bool, len(patterns))
	for _, pattern := range patterns {
		patternSet[pattern] = true
	}
	return findWorkspaceFiles(workspaceRoot, func(relativePath string) bool {
		return patternSet[filepath.Base(relativePath)]
	})
}

func providerFilePatterns(taskType string) ([]string, error) {
	switch taskType {
	case "gulp":
		return []string{"gulpfile.js", "gulpfile.cjs", "gulpfile.mjs", "gulpfile.ts"}, nil
	case "grunt":
		return []string{"Gruntfile.js", "Gruntfile.cjs", "Gruntfile.mjs", "Gruntfile.ts"}, nil
	case "jake":
		return []string{"Jakefile", "Jakefile.js", "Jakefile.cjs", "Jakefile.mjs", "Jakefile.ts"}, nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", taskType)
	}
}

func parseProviderTasks(path string, taskType string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	pattern, err := providerTaskPattern(taskType)
	if err != nil {
		return nil, err
	}
	matches := pattern.FindAllStringSubmatch(string(content), -1)
	seen := make(map[string]bool)
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		name := strings.TrimSpace(match[1])
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		items = append(items, name)
	}
	sort.Strings(items)
	return items, nil
}

func providerTaskPattern(taskType string) (*regexp.Regexp, error) {
	switch taskType {
	case "gulp":
		return regexp.MustCompile(`gulp\.task\(\s*['\"]([^'\"]+)['\"]`), nil
	case "grunt":
		return regexp.MustCompile(`grunt\.registerTask\(\s*['\"]([^'\"]+)['\"]`), nil
	case "jake":
		return regexp.MustCompile(`(?:^|\W)task\(\s*['\"]([^'\"]+)['\"]`), nil
	default:
		return nil, fmt.Errorf("unsupported provider type: %s", taskType)
	}
}

func javascriptProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$eslint-stylish": {
			Owner:        "eslint",
			Source:       "eslint",
			Severity:     "error",
			FileLocation: mustMarshal([]string{"relative"}),
			Pattern: mustMarshal([]patternConfig{
				{
					Regexp: `^([^\s].*)$`,
					File:   1,
				},
				{
					Regexp:   `^\s+(\d+):(\d+)\s+(error|warning|info)\s+(.*)\s\s+(.*)$`,
					Line:     1,
					Column:   2,
					Severity: 3,
					Message:  4,
					Code:     5,
					Loop:     true,
				},
			}),
		},
		"$eslint-compact": {
			Owner:        "eslint",
			Source:       "eslint",
			Severity:     "error",
			FileLocation: mustMarshal([]string{"relative"}),
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^(.+):\sline\s(\d+),\scol\s(\d+),\s(Error|Warning|Info)\s-\s(.+)\s\((.+)\)$`,
				File:     1,
				Line:     2,
				Column:   3,
				Severity: 4,
				Message:  5,
				Code:     6,
			}),
		},
		"$jshint": {
			Owner:    "jshint",
			Source:   "jshint",
			Severity: "error",
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^(.*):\s+line\s+(\d+),\s+col\s+(\d+),\s(.+?)(?:\s+\((\w)(\d+)\))?$`,
				File:     1,
				Line:     2,
				Column:   3,
				Message:  4,
				Severity: 5,
				Code:     6,
			}),
		},
		"$jshint-stylish": {
			Owner:    "jshint",
			Source:   "jshint",
			Severity: "error",
			Pattern: mustMarshal([]patternConfig{
				{
					Regexp: `^(.+)$`,
					File:   1,
				},
				{
					Regexp:   `^\s+line\s+(\d+)\s+col\s+(\d+)\s+(.+?)(?:\s+\((\w)(\d+)\))?$`,
					Line:     1,
					Column:   2,
					Message:  3,
					Severity: 4,
					Code:     5,
					Loop:     true,
				},
			}),
		},
	}
}

func collectJavaScriptCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	candidates := make([]TaskCandidate, 0)

	npmPackages, err := FindNPMPackages(workspaceRoot)
	if err != nil {
		return nil, err
	}
	for _, packageDir := range npmPackages {
		scripts, err := NPMScripts(workspaceRoot, packageDir)
		if err != nil {
			return nil, err
		}
		for _, script := range scripts {
			task := NewNPMTask(script, packageDir)
			candidates = append(candidates, newTaskCandidate("npm", inferTaskLabel(task, workspaceRoot), task, candidateDetail(packageDir, "package.json")))
		}
	}

	for _, providerType := range []string{"gulp", "grunt", "jake"} {
		definitions, err := FindProviderTasks(workspaceRoot, providerType)
		if err != nil {
			return nil, err
		}
		for _, definition := range definitions {
			task := NewProviderTask(providerType, definition.Task, definition.File)
			candidates = append(candidates, newTaskCandidate(providerType, inferTaskLabel(task, workspaceRoot), task, definition.File))
		}
	}

	return candidates, nil
}
