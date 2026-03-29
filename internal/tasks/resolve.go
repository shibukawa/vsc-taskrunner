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

func VariableNames(value string) []string {
	matches := variablePattern.FindAllStringSubmatch(value, -1)
	if len(matches) == 0 {
		return nil
	}
	items := make([]string, 0, len(matches))
	for _, match := range matches {
		if len(match) > 1 {
			items = append(items, match[1])
		}
	}
	return items
}

type resolvedText struct {
	value        string
	displayValue string
	redacted     bool
}

func ResolveFile(file *File, options ResolveOptions) (*Catalog, error) {
	options.Inputs = file.Inputs
	definitions := BuildTaskDefinitionCatalog(file, options.WorkspaceRoot, options.TaskFilePath)
	catalog := &Catalog{
		WorkspaceRoot: options.WorkspaceRoot,
		TaskFilePath:  options.TaskFilePath,
		Tasks:         make(map[string]ResolvedTask, len(definitions.Tasks)),
		Order:         make([]string, 0, len(definitions.Order)),
	}
	for _, label := range definitions.Order {
		partial, err := ResolveTaskSelection(definitions, label, options)
		if err != nil {
			return nil, err
		}
		task, ok := partial.Tasks[label]
		if !ok {
			continue
		}
		catalog.Tasks[label] = task
		catalog.Order = append(catalog.Order, label)
	}
	sort.Strings(catalog.Order)
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
		redaction:     options.Redaction,
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
		text, err := resolver.ResolveToken(task.Command.Value)
		if err != nil {
			return ResolvedTask{}, err
		}
		resolved.Command = text.value
		resolved.DisplayCommand = text.displayValue
		resolved.CommandToken = ResolvedToken{Value: text.value, DisplayValue: text.displayValue, Quoting: task.Command.Quoting}
	}

	resolved.Args = make([]string, 0, len(task.Args))
	resolved.DisplayArgs = make([]string, 0, len(task.Args))
	resolved.ArgTokens = make([]ResolvedToken, 0, len(task.Args))
	for _, argument := range task.Args {
		text, err := resolver.ResolveToken(argument.Value)
		if err != nil {
			return ResolvedTask{}, err
		}
		resolved.Args = append(resolved.Args, text.value)
		resolved.DisplayArgs = append(resolved.DisplayArgs, text.displayValue)
		resolved.ArgTokens = append(resolved.ArgTokens, ResolvedToken{Value: text.value, DisplayValue: text.displayValue, Quoting: argument.Quoting})
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
	redaction     RedactionPolicy
}

func (v variableResolver) Resolve(value string) (string, error) {
	result, err := v.ResolveToken(value)
	if err != nil {
		return "", err
	}
	return result.value, nil
}

func (v variableResolver) ResolveToken(value string) (resolvedText, error) {
	matches := variablePattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return resolvedText{value: value, displayValue: value}, nil
	}

	var valueBuilder strings.Builder
	var displayBuilder strings.Builder
	last := 0
	redacted := false
	for _, match := range matches {
		literal := value[last:match[0]]
		valueBuilder.WriteString(literal)
		displayBuilder.WriteString(literal)
		name := value[match[2]:match[3]]
		resolved, err := v.resolveVariable(name)
		if err != nil {
			return resolvedText{}, err
		}
		valueBuilder.WriteString(resolved.value)
		displayBuilder.WriteString(resolved.displayValue)
		redacted = redacted || resolved.redacted
		last = match[1]
	}
	valueBuilder.WriteString(value[last:])
	displayBuilder.WriteString(value[last:])
	return resolvedText{
		value:        valueBuilder.String(),
		displayValue: displayBuilder.String(),
		redacted:     redacted,
	}, nil
}

func (v variableResolver) resolveVariable(name string) (resolvedText, error) {
	switch {
	case name == "workspaceFolder":
		return resolvedText{value: v.workspaceRoot, displayValue: v.workspaceRoot}, nil
	case name == "workspaceFolderBasename":
		value := filepath.Base(v.workspaceRoot)
		return resolvedText{value: value, displayValue: value}, nil
	case name == "cwd":
		if v.cwd != "" {
			return resolvedText{value: v.cwd, displayValue: v.cwd}, nil
		}
		return resolvedText{value: v.workspaceRoot, displayValue: v.workspaceRoot}, nil
	case name == "pathSeparator":
		value := string(filepath.Separator)
		return resolvedText{value: value, displayValue: value}, nil
	case strings.HasPrefix(name, "env:"):
		key := strings.TrimPrefix(name, "env:")
		value := v.env[key]
		if v.redaction.ShouldRedact(key) {
			return resolvedText{value: value, displayValue: RedactedPlaceholder, redacted: true}, nil
		}
		return resolvedText{value: value, displayValue: value}, nil
	case strings.HasPrefix(name, "input:"):
		return v.inputs.Resolve(strings.TrimPrefix(name, "input:"))
	case strings.HasPrefix(name, "command:"):
		return resolvedText{}, fmt.Errorf("unsupported variable: ${%s}", name)
	case strings.HasPrefix(name, "config:"):
		return resolvedText{}, fmt.Errorf("unsupported variable: ${%s}", name)
	case strings.HasPrefix(name, "file"):
		return resolvedText{}, fmt.Errorf("unsupported variable: ${%s}", name)
	default:
		return resolvedText{}, fmt.Errorf("unsupported variable: ${%s}", name)
	}
}

type inputResolver struct {
	inputs         map[string]Input
	values         map[string]string
	env            map[string]string
	redaction      RedactionPolicy
	reader         *bufio.Reader
	writer         io.Writer
	nonInteractive bool
}

func newInputResolver(options ResolveOptions) (*inputResolver, error) {
	items := make(map[string]Input, len(options.Inputs))
	for _, input := range options.Inputs {
		if input.ID == "" {
			return nil, fmt.Errorf("input is missing id")
		}
		items[input.ID] = input
	}
	resolver := &inputResolver{
		inputs:         items,
		values:         cloneMap(options.InputValues),
		env:            envMap(options.Env),
		redaction:      options.Redaction,
		reader:         bufio.NewReader(options.Stdin),
		writer:         options.Stdout,
		nonInteractive: options.NonInteractive,
	}
	return resolver, nil
}

func (r *inputResolver) Resolve(id string) (resolvedText, error) {
	if value, ok := r.values[id]; ok {
		return r.toResolvedText(id, value), nil
	}
	if value, ok := r.env[inputEnvKey(id)]; ok {
		r.values[id] = value
		return r.toResolvedText(id, value), nil
	}

	input, ok := r.inputs[id]
	if !ok {
		return resolvedText{}, fmt.Errorf("unknown input: %s", id)
	}
	if r.nonInteractive {
		return resolvedText{}, fmt.Errorf("input %s requires a value in non-interactive mode", id)
	}

	value, err := r.prompt(input)
	if err != nil {
		return resolvedText{}, err
	}
	r.values[id] = value
	return r.toResolvedText(id, value), nil
}

func (r *inputResolver) toResolvedText(id string, value string) resolvedText {
	if r.redaction.ShouldRedact(id) {
		return resolvedText{value: value, displayValue: RedactedPlaceholder, redacted: true}
	}
	return resolvedText{value: value, displayValue: value}
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
