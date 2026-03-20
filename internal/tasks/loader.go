package tasks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/tailscale/hujson"
)

func ResolveLoadOptions(tasksPath string, workspaceRoot string) LoadOptions {
	if tasksPath == "" {
		tasksPath = filepath.Join(workspaceRoot, ".vscode", "tasks.json")
	}
	return LoadOptions{
		Path:          tasksPath,
		WorkspaceRoot: workspaceRoot,
	}
}

func LoadFile(options LoadOptions) (*File, error) {
	content, err := os.ReadFile(options.Path)
	if err != nil {
		return nil, fmt.Errorf("read tasks file %s: %w", options.Path, err)
	}

	standardized, err := hujson.Standardize(content)
	if err != nil {
		return nil, fmt.Errorf("parse tasks file %s: %w", options.Path, err)
	}

	var file File
	if err := json.Unmarshal(standardized, &file); err != nil {
		return nil, fmt.Errorf("decode tasks file %s: %w", options.Path, err)
	}

	if file.Version != "2.0.0" {
		return nil, fmt.Errorf("unsupported tasks.json version %q", file.Version)
	}

	applyFileDefaults(&file)

	for index := range file.Tasks {
		task := &file.Tasks[index]
		task.Label = inferTaskLabel(*task, options.WorkspaceRoot)
		if task.Label == "" {
			return nil, fmt.Errorf("task at index %d is missing label", index)
		}
	}
	return &file, nil
}

func applyFileDefaults(file *File) {
	platformDefaults := selectDefaults(file.Windows, file.OSX, file.Linux)
	if platformDefaults == nil {
		return
	}

	file.Options = mergeOptions(file.Options, platformDefaults.Options)
	file.Presentation = mergePresentation(file.Presentation, platformDefaults.Presentation)
	file.RunOptions = mergeRunOptions(file.RunOptions, platformDefaults.RunOptions)
	if len(platformDefaults.ProblemMatcher) > 0 && len(file.Tasks) > 0 {
		for index := range file.Tasks {
			if len(file.Tasks[index].ProblemMatcher) == 0 {
				file.Tasks[index].ProblemMatcher = cloneBytes(platformDefaults.ProblemMatcher)
			}
		}
	}
	if platformDefaults.Type != "" {
		for index := range file.Tasks {
			if file.Tasks[index].Type == "" {
				file.Tasks[index].Type = platformDefaults.Type
			}
		}
	}
}

func selectDefaults(windows *TaskDefaults, osx *TaskDefaults, linux *TaskDefaults) *TaskDefaults {
	switch runtime.GOOS {
	case "windows":
		return windows
	case "darwin":
		return osx
	default:
		return linux
	}
}

func mergeTaskDefaults(base Task, defaults *TaskDefaults) Task {
	if defaults == nil {
		return base
	}

	if base.Type == "" {
		base.Type = defaults.Type
	}
	if !base.Command.Set && defaults.Command.Set {
		base.Command = defaults.Command
	}
	if len(base.Args) == 0 && len(defaults.Args) > 0 {
		base.Args = cloneTokens(defaults.Args)
	}
	if !base.Dependencies.Set && defaults.Dependencies.Set {
		base.Dependencies = cloneDependencies(defaults.Dependencies)
	}
	if base.DependsOrder == "" {
		base.DependsOrder = defaults.DependsOrder
	}
	if len(base.Group) == 0 && len(defaults.Group) > 0 {
		base.Group = cloneBytes(defaults.Group)
	}
	base.Options = mergeOptions(defaults.Options, base.Options)
	base.Presentation = mergePresentation(defaults.Presentation, base.Presentation)
	base.RunOptions = mergeRunOptions(defaults.RunOptions, base.RunOptions)
	if len(base.ProblemMatcher) == 0 && len(defaults.ProblemMatcher) > 0 {
		base.ProblemMatcher = cloneBytes(defaults.ProblemMatcher)
	}
	if defaults.Hide != nil {
		base.Hide = *defaults.Hide
	}
	if defaults.IsBackground != nil {
		base.IsBackground = *defaults.IsBackground
	}
	return base
}

func mergeOptions(base *Options, overlay *Options) *Options {
	if base == nil && overlay == nil {
		return nil
	}

	result := &Options{}
	if base != nil {
		*result = *base
		if base.Env != nil {
			result.Env = cloneMap(base.Env)
		}
		if base.Shell != nil {
			shellCopy := *base.Shell
			shellCopy.Args = append([]string(nil), base.Shell.Args...)
			result.Shell = &shellCopy
		}
	}
	if overlay == nil {
		return result
	}
	if overlay.CWD != "" {
		result.CWD = overlay.CWD
	}
	if overlay.Env != nil {
		if result.Env == nil {
			result.Env = make(map[string]string, len(overlay.Env))
		}
		for key, value := range overlay.Env {
			result.Env[key] = value
		}
	}
	if overlay.Shell != nil {
		shellCopy := *overlay.Shell
		shellCopy.Args = append([]string(nil), overlay.Shell.Args...)
		result.Shell = &shellCopy
	}
	return result
}

func mergePresentation(base *Presentation, overlay *Presentation) *Presentation {
	if base == nil && overlay == nil {
		return nil
	}
	result := &Presentation{}
	if base != nil {
		*result = *base
	}
	if overlay == nil {
		return result
	}
	if overlay.Echo != nil {
		result.Echo = overlay.Echo
	}
	if overlay.Reveal != "" {
		result.Reveal = overlay.Reveal
	}
	if overlay.RevealProblems != "" {
		result.RevealProblems = overlay.RevealProblems
	}
	if overlay.Focus != nil {
		result.Focus = overlay.Focus
	}
	if overlay.Panel != "" {
		result.Panel = overlay.Panel
	}
	if overlay.ShowReuseMessage != nil {
		result.ShowReuseMessage = overlay.ShowReuseMessage
	}
	if overlay.Clear != nil {
		result.Clear = overlay.Clear
	}
	if overlay.Close != nil {
		result.Close = overlay.Close
	}
	if overlay.Group != "" {
		result.Group = overlay.Group
	}
	return result
}

func mergeRunOptions(base *RunOptions, overlay *RunOptions) *RunOptions {
	if base == nil && overlay == nil {
		return nil
	}
	result := &RunOptions{}
	if base != nil {
		*result = *base
	}
	if overlay == nil {
		return result
	}
	if overlay.ReevaluateOnRerun != nil {
		result.ReevaluateOnRerun = overlay.ReevaluateOnRerun
	}
	if overlay.RunOn != "" {
		result.RunOn = overlay.RunOn
	}
	if overlay.InstanceLimit != nil {
		result.InstanceLimit = overlay.InstanceLimit
	}
	if overlay.InstancePolicy != "" {
		result.InstancePolicy = overlay.InstancePolicy
	}
	return result
}

func cloneTokens(tokens []TokenValue) []TokenValue {
	return append([]TokenValue(nil), tokens...)
}

func cloneDependencies(list DependencyList) DependencyList {
	items := append([]Dependency(nil), list.Items...)
	return DependencyList{Items: items, Set: list.Set}
}

func cloneBytes(data []byte) []byte {
	return append([]byte(nil), data...)
}

func cloneMap(source map[string]string) map[string]string {
	if source == nil {
		return nil
	}
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
