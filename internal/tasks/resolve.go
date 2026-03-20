package tasks

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"unicode"
)

var variablePattern = regexp.MustCompile(`\$\{([^}]+)}`)

func ResolveFile(file *File, options ResolveOptions) (*Catalog, error) {
	inputResolver, err := newInputResolver(file.Inputs, options)
	if err != nil {
		return nil, err
	}

	defaults := TaskDefaults{
		Options:      file.Options,
		Presentation: file.Presentation,
		RunOptions:   file.RunOptions,
	}

	catalog := &Catalog{
		WorkspaceRoot: options.WorkspaceRoot,
		TaskFilePath:  options.TaskFilePath,
		Tasks:         make(map[string]ResolvedTask, len(file.Tasks)),
		Order:         make([]string, 0, len(file.Tasks)),
	}

	for _, task := range file.Tasks {
		merged := mergeTaskDefaults(task, &defaults)
		merged = mergeTaskDefaults(merged, selectDefaults(task.Windows, task.OSX, task.Linux))

		resolved, err := resolveTask(merged, options, inputResolver)
		if err != nil {
			return nil, fmt.Errorf("resolve task %s: %w", task.Label, err)
		}
		catalog.Tasks[resolved.Label] = resolved
		catalog.Order = append(catalog.Order, resolved.Label)
	}

	sort.Strings(catalog.Order)
	for _, task := range catalog.Tasks {
		for _, dep := range task.DependsOn {
			if _, ok := catalog.Tasks[dep]; !ok {
				return nil, fmt.Errorf("task %s depends on unknown task %s", task.Label, dep)
			}
		}
	}

	return catalog, nil
}

func resolveTask(task Task, options ResolveOptions, inputs *inputResolver) (ResolvedTask, error) {
	for _, dependency := range task.Dependencies.Items {
		if dependency.Unsupported != "" {
			return ResolvedTask{}, fmt.Errorf("task %s has unsupported dependency: %s", task.Label, dependency.Unsupported)
		}
	}

	resolver := variableResolver{
		workspaceRoot: options.WorkspaceRoot,
		cwd:           options.WorkspaceRoot,
		env:           envMap(options.Env),
		inputs:        inputs,
	}

	adaptedTask, err := applyTaskAdapter(task, options.WorkspaceRoot, resolver)
	if err != nil {
		return ResolvedTask{}, err
	}
	task = adaptedTask

	resolved := ResolvedTask{
		Label:          task.Label,
		Type:           task.EffectiveType(),
		Group:          cloneBytes(task.Group),
		SourceTaskID:   task.Identifier,
		WorkspaceRoot:  options.WorkspaceRoot,
		TaskFilePath:   options.TaskFilePath,
		DependsOn:      task.Dependencies.Labels(),
		DependsOrder:   task.DependsOrder,
		Presentation:   task.Presentation,
		RunOptions:     task.RunOptions,
		ProblemMatcher: cloneBytes(task.ProblemMatcher),
		Hide:           task.Hide,
		IsBackground:   task.IsBackground,
	}
	if resolved.DependsOrder == "" {
		resolved.DependsOrder = "parallel"
	}

	if task.Command.Set {
		text, err := resolver.Resolve(task.Command.Value)
		if err != nil {
			return ResolvedTask{}, err
		}
		resolved.Command = text
		resolved.CommandToken = ResolvedToken{Value: text, Quoting: task.Command.Quoting}
	}

	resolved.Args = make([]string, 0, len(task.Args))
	resolved.ArgTokens = make([]ResolvedToken, 0, len(task.Args))
	for _, argument := range task.Args {
		text, err := resolver.Resolve(argument.Value)
		if err != nil {
			return ResolvedTask{}, err
		}
		resolved.Args = append(resolved.Args, text)
		resolved.ArgTokens = append(resolved.ArgTokens, ResolvedToken{Value: text, Quoting: argument.Quoting})
	}

	resolved.Options = ResolvedOptions{
		CWD: options.WorkspaceRoot,
		Env: make(map[string]string),
	}
	if task.Options != nil {
		if task.Options.CWD != "" {
			cwd, err := resolver.Resolve(task.Options.CWD)
			if err != nil {
				return ResolvedTask{}, err
			}
			resolved.Options.CWD = cwd
			resolver.cwd = cwd
		}
		for key, value := range task.Options.Env {
			resolvedValue, err := resolver.Resolve(value)
			if err != nil {
				return ResolvedTask{}, err
			}
			resolved.Options.Env[key] = resolvedValue
		}
		resolved.Options.Shell = defaultShell(task.Options.Shell)
		if task.Options.Shell != nil {
			executable, err := resolver.Resolve(task.Options.Shell.Executable)
			if err != nil {
				return ResolvedTask{}, err
			}
			resolved.Options.Shell.Executable = executable
			resolved.Options.Shell.Args = make([]string, 0, len(task.Options.Shell.Args))
			for _, item := range task.Options.Shell.Args {
				resolvedArg, err := resolver.Resolve(item)
				if err != nil {
					return ResolvedTask{}, err
				}
				resolved.Options.Shell.Args = append(resolved.Options.Shell.Args, resolvedArg)
			}
			resolved.Options.Shell.Family = shellFamily(resolved.Options.Shell.Executable)
		}
	} else {
		resolved.Options.Shell = defaultShell(nil)
	}

	if resolved.Options.CWD == "" {
		resolved.Options.CWD = options.WorkspaceRoot
	}

	return resolved, nil
}

type variableResolver struct {
	workspaceRoot string
	cwd           string
	env           map[string]string
	inputs        *inputResolver
}

func (v variableResolver) Resolve(value string) (string, error) {
	matches := variablePattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		builder.WriteString(value[last:match[0]])
		name := value[match[2]:match[3]]
		resolved, err := v.resolveVariable(name)
		if err != nil {
			return "", err
		}
		builder.WriteString(resolved)
		last = match[1]
	}
	builder.WriteString(value[last:])
	return builder.String(), nil
}

func (v variableResolver) resolveVariable(name string) (string, error) {
	switch {
	case name == "workspaceFolder":
		return v.workspaceRoot, nil
	case name == "workspaceFolderBasename":
		return filepath.Base(v.workspaceRoot), nil
	case name == "cwd":
		if v.cwd != "" {
			return v.cwd, nil
		}
		return v.workspaceRoot, nil
	case name == "pathSeparator":
		return string(filepath.Separator), nil
	case strings.HasPrefix(name, "env:"):
		return v.env[strings.TrimPrefix(name, "env:")], nil
	case strings.HasPrefix(name, "input:"):
		return v.inputs.Resolve(strings.TrimPrefix(name, "input:"))
	case strings.HasPrefix(name, "command:"):
		return "", fmt.Errorf("unsupported variable: ${%s}", name)
	case strings.HasPrefix(name, "config:"):
		return "", fmt.Errorf("unsupported variable: ${%s}", name)
	case strings.HasPrefix(name, "file"):
		return "", fmt.Errorf("unsupported variable: ${%s}", name)
	default:
		return "", fmt.Errorf("unsupported variable: ${%s}", name)
	}
}

type inputResolver struct {
	inputs         map[string]Input
	values         map[string]string
	env            map[string]string
	reader         *bufio.Reader
	writer         io.Writer
	nonInteractive bool
}

func newInputResolver(inputs []Input, options ResolveOptions) (*inputResolver, error) {
	items := make(map[string]Input, len(inputs))
	for _, input := range inputs {
		if input.ID == "" {
			return nil, fmt.Errorf("input is missing id")
		}
		items[input.ID] = input
	}
	resolver := &inputResolver{
		inputs:         items,
		values:         cloneMap(options.InputValues),
		env:            envMap(options.Env),
		reader:         bufio.NewReader(options.Stdin),
		writer:         options.Stdout,
		nonInteractive: options.NonInteractive,
	}
	return resolver, nil
}

func (r *inputResolver) Resolve(id string) (string, error) {
	if value, ok := r.values[id]; ok {
		return value, nil
	}
	if value, ok := r.env[inputEnvKey(id)]; ok {
		r.values[id] = value
		return value, nil
	}

	input, ok := r.inputs[id]
	if !ok {
		return "", fmt.Errorf("unknown input: %s", id)
	}
	if r.nonInteractive {
		return "", fmt.Errorf("input %s requires a value in non-interactive mode", id)
	}

	value, err := r.prompt(input)
	if err != nil {
		return "", err
	}
	r.values[id] = value
	return value, nil
}

func (r *inputResolver) prompt(input Input) (string, error) {
	switch input.Type {
	case "promptString":
		message := input.Description
		if message == "" {
			message = input.ID
		}
		if input.Default != "" {
			fmt.Fprintf(r.writer, "%s [%s]: ", message, input.Default)
		} else {
			fmt.Fprintf(r.writer, "%s: ", message)
		}
		line, err := r.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			line = input.Default
		}
		return line, nil
	case "pickString":
		fmt.Fprintf(r.writer, "%s\n", input.Description)
		for index, option := range input.Options {
			fmt.Fprintf(r.writer, "  %d. %s\n", index+1, option.Label)
		}
		if input.Default != "" {
			fmt.Fprintf(r.writer, "select [%s]: ", input.Default)
		} else {
			fmt.Fprint(r.writer, "select: ")
		}
		line, err := r.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			return input.Default, nil
		}
		for index, option := range input.Options {
			if fmt.Sprintf("%d", index+1) == line || option.Value == line || option.Label == line {
				return option.Value, nil
			}
		}
		return "", fmt.Errorf("invalid selection for input %s", input.ID)
	default:
		return "", fmt.Errorf("unsupported input type: %s", input.Type)
	}
}

func envMap(items []string) map[string]string {
	result := make(map[string]string, len(items))
	for _, item := range items {
		parts := strings.SplitN(item, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

func inputEnvKey(id string) string {
	var builder strings.Builder
	builder.WriteString("VSCTASK_INPUT_")
	for _, r := range id {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(unicode.ToUpper(r))
			continue
		}
		builder.WriteRune('_')
	}
	return builder.String()
}

func defaultShell(config *ShellConfiguration) ShellRuntime {
	if config != nil && config.Executable != "" {
		runtimeShell := ShellRuntime{
			Executable: config.Executable,
			Args:       append([]string(nil), config.Args...),
			Family:     shellFamily(config.Executable),
		}
		if len(runtimeShell.Args) == 0 {
			runtimeShell.Args = defaultShellArgs(runtimeShell.Family)
		}
		return runtimeShell
	}

	if runtime.GOOS == "windows" {
		return ShellRuntime{Executable: "cmd.exe", Args: []string{"/d", "/c"}, Family: "cmd"}
	}
	executable := "/bin/sh"
	return ShellRuntime{Executable: executable, Args: []string{"-c"}, Family: shellFamily(executable)}
}

func defaultShellArgs(family string) []string {
	switch family {
	case "cmd":
		return []string{"/d", "/c"}
	case "powershell":
		return []string{"-Command"}
	default:
		return []string{"-c"}
	}
}

func shellFamily(executable string) string {
	base := strings.ToLower(filepath.Base(executable))
	switch base {
	case "cmd", "cmd.exe":
		return "cmd"
	case "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return "powershell"
	default:
		return "posix"
	}
}
