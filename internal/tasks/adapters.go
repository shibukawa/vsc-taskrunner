package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/tailscale/hujson"
)

type taskAdapter interface {
	Resolve(task Task, workspaceRoot string, resolver variableResolver) (Task, error)
	Label(task Task, workspaceRoot string) string
}

func providerAdapters() map[string]taskAdapter {
	return map[string]taskAdapter{
		"npm":        npmTaskAdapter{},
		"typescript": typescriptTaskAdapter{},
		"gulp":       simpleNodeTaskAdapter{taskType: "gulp", flagName: "--gulpfile"},
		"grunt":      simpleNodeTaskAdapter{taskType: "grunt", flagName: "--gruntfile"},
		"jake":       simpleNodeTaskAdapter{taskType: "jake", flagName: "-f"},
	}
}

func applyTaskAdapter(task Task, workspaceRoot string, resolver variableResolver) (Task, error) {
	effectiveType := task.EffectiveType()
	if effectiveType == "shell" || effectiveType == "process" {
		return task, nil
	}
	adapter, ok := providerAdapters()[effectiveType]
	if !ok {
		return Task{}, fmt.Errorf("unsupported provider type: %s", effectiveType)
	}
	return adapter.Resolve(task, workspaceRoot, resolver)
}

func inferTaskLabel(task Task, workspaceRoot string) string {
	if label := task.EffectiveLabel(); label != "" {
		return label
	}
	adapter, ok := providerAdapters()[task.EffectiveType()]
	if !ok {
		return ""
	}
	return adapter.Label(task, workspaceRoot)
}

type npmTaskAdapter struct{}

func (npmTaskAdapter) Resolve(task Task, workspaceRoot string, resolver variableResolver) (Task, error) {
	if task.Script == "" {
		return Task{}, fmt.Errorf("npm task is missing script")
	}
	script, err := resolver.Resolve(task.Script)
	if err != nil {
		return Task{}, err
	}
	packageDir, relativePath, err := resolvePackageDir(workspaceRoot, task.Path, resolver)
	if err != nil {
		return Task{}, err
	}
	command := detectPackageManager(packageDir)
	args := []string{"run", script}
	task.Type = "shell"
	task.Command = TokenValue{Value: command, Set: true}
	task.Args = stringsToTokens(args)
	task.Options = mergeOptions(&Options{CWD: packageDir}, task.Options)
	if task.Label == "" {
		task.Label = canonicalTaskLabel("npm", script, relativePath)
	}
	return task, nil
}

func (npmTaskAdapter) Label(task Task, workspaceRoot string) string {
	if task.Script == "" {
		return ""
	}
	label := task.Script
	if task.Path != "" {
		relative := normalizeRelative(task.Path)
		if relative != "" {
			return canonicalTaskLabel("npm", label, relative)
		}
	}
	return canonicalTaskLabel("npm", label, "")
}

type typescriptTaskAdapter struct{}

func (typescriptTaskAdapter) Resolve(task Task, workspaceRoot string, resolver variableResolver) (Task, error) {
	if task.TSConfig == "" {
		return Task{}, fmt.Errorf("typescript task is missing tsconfig")
	}
	tsconfig, err := resolver.Resolve(task.TSConfig)
	if err != nil {
		return Task{}, err
	}
	if strings.Contains(tsconfig, `\\`) {
		return Task{}, fmt.Errorf("typescript task tsconfig must use / instead of \\\\ ")
	}
	absTSConfig := filepath.Join(workspaceRoot, filepath.FromSlash(tsconfig))
	command := detectLocalNodeBinary(filepath.Dir(absTSConfig), workspaceRoot, "tsc")
	args := []string{"-p", absTSConfig}
	if hasTSConfigReferences(absTSConfig) {
		args = []string{"-b", absTSConfig}
	}
	switch task.Option {
	case "", "build":
		if len(task.ProblemMatcher) == 0 {
			task.ProblemMatcher = json.RawMessage(`"$tsc"`)
		}
	case "watch":
		args = append(args, "--watch")
		task.IsBackground = true
		if len(task.ProblemMatcher) == 0 {
			task.ProblemMatcher = json.RawMessage(`"$tsc-watch"`)
		}
	default:
		return Task{}, fmt.Errorf("unsupported typescript option: %s", task.Option)
	}
	task.Type = "shell"
	task.Command = TokenValue{Value: command, Set: true}
	task.Args = stringsToTokens(args)
	task.Options = mergeOptions(&Options{CWD: workspaceRoot}, task.Options)
	if task.Label == "" {
		task.Label = typescriptLabel(tsconfig, task.Option)
	}
	return task, nil
}

func (typescriptTaskAdapter) Label(task Task, workspaceRoot string) string {
	if task.TSConfig == "" {
		return ""
	}
	return typescriptLabel(task.TSConfig, task.Option)
}

type simpleNodeTaskAdapter struct {
	taskType string
	flagName string
}

func (a simpleNodeTaskAdapter) Resolve(task Task, workspaceRoot string, resolver variableResolver) (Task, error) {
	if task.ProviderTask == "" {
		return Task{}, fmt.Errorf("%s task is missing task", a.taskType)
	}
	providerTask, err := resolver.Resolve(task.ProviderTask)
	if err != nil {
		return Task{}, err
	}
	args := make([]string, 0, 3)
	if task.ProviderFile != "" {
		fileValue, err := resolver.Resolve(task.ProviderFile)
		if err != nil {
			return Task{}, err
		}
		args = append(args, a.flagName, fileValue)
	}
	args = append(args, providerTask)
	task.Type = "shell"
	task.Command = TokenValue{Value: detectLocalNodeBinary(workspaceRoot, workspaceRoot, a.taskType), Set: true}
	task.Args = stringsToTokens(args)
	task.Options = mergeOptions(&Options{CWD: workspaceRoot}, task.Options)
	if task.Label == "" {
		task.Label = a.Label(task, workspaceRoot)
	}
	return task, nil
}

func (a simpleNodeTaskAdapter) Label(task Task, workspaceRoot string) string {
	if task.ProviderTask == "" {
		return ""
	}
	return canonicalTaskLabel(a.taskType, task.ProviderTask, "")
}

func stringsToTokens(values []string) []TokenValue {
	result := make([]TokenValue, 0, len(values))
	for _, value := range values {
		result = append(result, TokenValue{Value: value, Set: true})
	}
	return result
}

func resolvePackageDir(workspaceRoot string, rawPath string, resolver variableResolver) (string, string, error) {
	if rawPath == "" {
		return workspaceRoot, "", nil
	}
	resolvedPath, err := resolver.Resolve(rawPath)
	if err != nil {
		return "", "", err
	}
	cleanRelative := normalizeRelative(resolvedPath)
	return filepath.Join(workspaceRoot, filepath.FromSlash(cleanRelative)), cleanRelative, nil
}

func normalizeRelative(value string) string {
	cleaned := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	if cleaned == "." {
		return ""
	}
	return strings.TrimSuffix(cleaned, "/")
}

func detectPackageManager(packageDir string) string {
	packageManager := readPackageManagerField(filepath.Join(packageDir, "package.json"))
	switch packageManager {
	case "pnpm", "yarn", "npm":
		return packageManager
	}
	if fileExists(filepath.Join(packageDir, "pnpm-lock.yaml")) {
		return "pnpm"
	}
	if fileExists(filepath.Join(packageDir, "yarn.lock")) {
		return "yarn"
	}
	return "npm"
}

func readPackageManagerField(packageJSON string) string {
	content, err := os.ReadFile(packageJSON)
	if err != nil {
		return ""
	}
	standardized, err := hujson.Standardize(content)
	if err != nil {
		return ""
	}
	var raw struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(standardized, &raw); err != nil {
		return ""
	}
	if raw.PackageManager == "" {
		return ""
	}
	parts := strings.SplitN(raw.PackageManager, "@", 2)
	return parts[0]
}

func detectLocalNodeBinary(primaryDir string, workspaceRoot string, name string) string {
	platformName := name
	if runtime.GOOS == "windows" {
		platformName += ".cmd"
	}
	for _, root := range []string{primaryDir, workspaceRoot} {
		candidate := filepath.Join(root, "node_modules", ".bin", platformName)
		if fileExists(candidate) {
			if runtime.GOOS == "windows" {
				return filepath.Join(".", "node_modules", ".bin", platformName)
			}
			return filepath.Join(".", "node_modules", ".bin", name)
		}
	}
	return name
}

func hasTSConfigReferences(tsconfigPath string) bool {
	content, err := os.ReadFile(tsconfigPath)
	if err != nil {
		return false
	}
	standardized, err := hujson.Standardize(content)
	if err != nil {
		return false
	}
	var raw struct {
		References []json.RawMessage `json:"references"`
	}
	if err := json.Unmarshal(standardized, &raw); err != nil {
		return false
	}
	return len(raw.References) > 0
}

func typescriptLabel(tsconfig string, option string) string {
	label := "build"
	if option == "watch" {
		label = "watch"
	}
	return canonicalTaskLabel("tsc", label, filepath.ToSlash(tsconfig))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
