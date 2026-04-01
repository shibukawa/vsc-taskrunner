package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"vsc-taskrunner/internal/tasks"
)

func (a *App) runAdd(args []string) int {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		if exitCode, ok := a.runAddSubcommand(args[0], args[1:]); ok {
			return exitCode
		}
	}

	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "add does not accept positional arguments")
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	task, err := promptForTask(a.stdin, a.stdout)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	if err := tasks.SaveTask(loaderOptions, task); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	fmt.Fprintf(a.stdout, "added task %q to %s\n", task.Label, loaderOptions.Path)
	return 0
}

func (a *App) runAddDetect(args []string) int {
	fs := flag.NewFlagSet("add detect", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var jsonOutput bool
	var save bool
	var addAll bool
	var ecosystems string
	var labels string

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.BoolVar(&jsonOutput, "json", false, "print JSON")
	fs.BoolVar(&save, "save", false, "save detected tasks instead of listing them")
	fs.BoolVar(&addAll, "all", false, "select all matching detected tasks")
	fs.StringVar(&ecosystems, "ecosystem", "", "comma separated ecosystems to filter")
	fs.StringVar(&labels, "label", "", "comma separated task labels to save")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "add detect does not accept positional arguments")
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	candidates, err := tasks.DetectTaskCandidates(loaderOptions.WorkspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	filtered, err := filterCandidates(candidates, parseCSVList(ecosystems), parseCSVList(labels), addAll, save)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if save {
		items := make([]tasks.Task, 0, len(filtered))
		for _, candidate := range filtered {
			items = append(items, candidate.Task)
		}
		warnings, err := saveTasksForAdd(loaderOptions, items)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		result := printAddSummary(a.stdout, loaderOptions.Path, items)
		for _, warning := range warnings {
			fmt.Fprintln(a.stderr, warning)
		}
		return result
	}

	if jsonOutput {
		return writeJSON(a.stdout, filtered)
	}

	for _, candidate := range filtered {
		fmt.Fprintf(a.stdout, "%s\t%s", candidate.Ecosystem, candidate.Label)
		if candidate.Detail != "" {
			fmt.Fprintf(a.stdout, "\t%s", candidate.Detail)
		}
		fmt.Fprintln(a.stdout)
	}
	return 0
}

func (a *App) runAddNPM(args []string) int {
	fs := flag.NewFlagSet("add npm", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var packagePath string
	var taskNames string
	var addAll bool

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.StringVar(&packagePath, "path", "", "package directory relative to workspace root")
	fs.StringVar(&taskNames, "task", "", "comma separated npm script names")
	fs.BoolVar(&addAll, "all", false, "add all detected scripts")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "add npm does not accept positional arguments")
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	packagePath = normalizeCLIPath(packagePath)
	if packagePath == "" {
		packages, err := tasks.FindNPMPackages(loaderOptions.WorkspaceRoot)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		selected, err := chooseSingleRoot(packages, addAll, "package.json", "--path")
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		packagePath = selected
	}

	scripts, err := tasks.NPMScripts(loaderOptions.WorkspaceRoot, packagePath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	selectedScripts, err := chooseNamedItems(parseCSVList(taskNames), scripts, addAll, "npm scripts")
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	items := make([]tasks.Task, 0, len(selectedScripts))
	for _, script := range selectedScripts {
		items = append(items, tasks.NewNPMTask(script, packagePath))
	}
	warnings, err := saveTasksForAdd(loaderOptions, items)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	result := printAddSummary(a.stdout, loaderOptions.Path, items)
	for _, warning := range warnings {
		fmt.Fprintln(a.stderr, warning)
	}
	return result
}

func (a *App) runAddTypeScript(args []string) int {
	fs := flag.NewFlagSet("add typescript", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var tsconfig string
	var taskModes string
	var addAll bool

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.StringVar(&tsconfig, "tsconfig", "", "tsconfig path relative to workspace root")
	fs.StringVar(&taskModes, "task", "", "comma separated task modes: build,watch")
	fs.BoolVar(&addAll, "all", false, "add build and watch tasks for all detected tsconfig files")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "add typescript does not accept positional arguments")
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	configs, err := tasks.FindTypeScriptConfigs(loaderOptions.WorkspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	selectedConfigs := configs
	if tsconfig != "" {
		selectedConfigs = []string{normalizeCLIPath(tsconfig)}
	} else if !addAll {
		selected, err := chooseSingleRoot(configs, false, "tsconfig.json", "--tsconfig")
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		selectedConfigs = []string{selected}
	}

	selectedModes := parseCSVList(taskModes)
	if len(selectedModes) == 0 && !addAll {
		selectedModes = []string{"build"}
	}
	availableModes, err := tasks.AvailableTypeScriptTaskModes(loaderOptions.WorkspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	modes, err := chooseTypeScriptModes(selectedModes, availableModes, addAll)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	items := make([]tasks.Task, 0, len(selectedConfigs)*len(modes))
	for _, config := range selectedConfigs {
		for _, mode := range modes {
			items = append(items, tasks.NewTypeScriptTask(config, mode))
		}
	}
	warnings, err := saveTasksForAdd(loaderOptions, items)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	result := printAddSummary(a.stdout, loaderOptions.Path, items)
	for _, warning := range warnings {
		fmt.Fprintln(a.stderr, warning)
	}
	return result
}

func (a *App) runAddProvider(taskType string, args []string) int {
	fs := flag.NewFlagSet("add "+taskType, flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var providerTask string
	var providerFile string
	var addAll bool

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.StringVar(&providerTask, "task", "", "provider task name")
	fs.StringVar(&providerFile, "file", "", "provider file path relative to workspace root")
	fs.BoolVar(&addAll, "all", false, "add all detected provider tasks")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(a.stderr, "add %s does not accept positional arguments\n", taskType)
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	items, err := resolveProviderTasks(loaderOptions.WorkspaceRoot, taskType, normalizeCLIPath(providerFile), parseCSVList(providerTask), addAll)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	warnings, err := saveTasksForAdd(loaderOptions, items)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	result := printAddSummary(a.stdout, loaderOptions.Path, items)
	for _, warning := range warnings {
		fmt.Fprintln(a.stderr, warning)
	}
	return result
}

func promptForTask(reader io.Reader, writer io.Writer) (tasks.Task, error) {
	input := bufio.NewReader(reader)

	label, err := promptLine(input, writer, "Label", "")
	if err != nil {
		return tasks.Task{}, err
	}
	if label == "" {
		return tasks.Task{}, fmt.Errorf("label is required")
	}

	taskType, err := promptType(input, writer)
	if err != nil {
		return tasks.Task{}, err
	}

	command, err := promptLine(input, writer, "Command", "")
	if err != nil {
		return tasks.Task{}, err
	}
	if command == "" {
		return tasks.Task{}, fmt.Errorf("command is required")
	}

	argsLine, err := promptLine(input, writer, "Args", "")
	if err != nil {
		return tasks.Task{}, err
	}
	args, err := splitCommandLine(argsLine)
	if err != nil {
		return tasks.Task{}, err
	}

	task := tasks.Task{
		Label:   label,
		Type:    taskType,
		Command: tasks.TokenValue{Value: command, Set: true},
		Args:    make([]tasks.TokenValue, 0, len(args)),
	}
	for _, arg := range args {
		task.Args = append(task.Args, tasks.TokenValue{Value: arg, Set: true})
	}

	fmt.Fprintln(writer)
	fmt.Fprintf(writer, "Task summary\n")
	fmt.Fprintf(writer, "  label: %s\n", task.Label)
	fmt.Fprintf(writer, "  type: %s\n", task.Type)
	fmt.Fprintf(writer, "  command: %s\n", task.Command.Value)
	if len(args) > 0 {
		fmt.Fprintf(writer, "  args: %s\n", strings.Join(args, " | "))
	}

	confirmed, err := promptConfirm(input, writer, "Save task", true)
	if err != nil {
		return tasks.Task{}, err
	}
	if !confirmed {
		return tasks.Task{}, fmt.Errorf("task creation cancelled")
	}

	return task, nil
}

func promptType(reader *bufio.Reader, writer io.Writer) (string, error) {
	for {
		value, err := promptLine(reader, writer, "Type [shell/process]", "shell")
		if err != nil {
			return "", err
		}
		switch value {
		case "", "shell":
			return "shell", nil
		case "process":
			return "process", nil
		default:
			fmt.Fprintln(writer, "Type must be 'shell' or 'process'.")
		}
	}
}

func promptConfirm(reader *bufio.Reader, writer io.Writer, label string, defaultValue bool) (bool, error) {
	defaultText := "y"
	if !defaultValue {
		defaultText = "n"
	}
	value, err := promptLine(reader, writer, label+" [y/n]", defaultText)
	if err != nil {
		return false, err
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "y", "yes":
		return true, nil
	case "n", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid confirmation: %s", value)
	}
}

func promptLine(reader *bufio.Reader, writer io.Writer, label string, defaultValue string) (string, error) {
	if defaultValue != "" {
		fmt.Fprintf(writer, "%s [%s]: ", label, defaultValue)
	} else {
		fmt.Fprintf(writer, "%s: ", label)
	}

	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultValue, nil
	}
	return line, nil
}

func splitCommandLine(input string) ([]string, error) {
	var tokens []string
	var current strings.Builder
	var quote rune
	var escaped bool
	started := false

	flush := func() {
		tokens = append(tokens, current.String())
		current.Reset()
		started = false
	}

	for _, r := range input {
		if escaped {
			current.WriteRune(r)
			escaped = false
			started = true
			continue
		}

		if r == '\\' && quote != '\'' {
			escaped = true
			started = true
			continue
		}

		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
			started = true
			continue
		}

		switch r {
		case '\'', '"':
			quote = r
			started = true
		case ' ', '\t', '\n', '\r':
			if started {
				flush()
			}
		default:
			current.WriteRune(r)
			started = true
		}
	}

	if escaped {
		return nil, fmt.Errorf("unterminated escape sequence")
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quoted string")
	}
	if started {
		flush()
	}
	return tokens, nil
}

func parseCSVList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item != "" {
			result = append(result, item)
		}
	}
	return result
}

func normalizeCLIPath(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	if cleaned == "." {
		return ""
	}
	return cleaned
}

func chooseSingleRoot(items []string, addAll bool, itemName string, flagName string) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("no %s detected", itemName)
	}
	if addAll && len(items) > 1 {
		return "", fmt.Errorf("%s cannot be omitted when multiple %s are present; remove --all or use %s", itemName, itemName, flagName)
	}
	if len(items) > 1 {
		return "", fmt.Errorf("multiple %s detected: %s; use %s", itemName, strings.Join(items, ", "), flagName)
	}
	return items[0], nil
}

func chooseNamedItems(selected []string, available []string, addAll bool, itemName string) ([]string, error) {
	if addAll {
		copied := append([]string(nil), available...)
		sort.Strings(copied)
		return copied, nil
	}
	if len(selected) == 0 {
		if len(available) == 1 {
			return []string{available[0]}, nil
		}
		return nil, fmt.Errorf("select %s with --task or use --all (available: %s)", itemName, strings.Join(available, ", "))
	}
	availableSet := make(map[string]bool, len(available))
	for _, item := range available {
		availableSet[item] = true
	}
	result := make([]string, 0, len(selected))
	for _, item := range selected {
		if !availableSet[item] {
			return nil, fmt.Errorf("unknown item %q for %s (available: %s)", item, itemName, strings.Join(available, ", "))
		}
		result = append(result, item)
	}
	return result, nil
}

func chooseTypeScriptModes(selected []string, available []string, addAll bool) ([]string, error) {
	allModes := []string{"build", "watch"}
	validSet := make(map[string]bool, len(allModes))
	for _, mode := range allModes {
		validSet[mode] = true
	}
	for _, mode := range selected {
		if !validSet[mode] {
			return nil, fmt.Errorf("unknown item %q for typescript task modes (available: %s)", mode, strings.Join(allModes, ", "))
		}
	}
	availableSet := make(map[string]bool, len(available))
	for _, mode := range available {
		availableSet[mode] = true
	}
	suppressed := make([]string, 0, len(allModes)-len(available))
	for _, mode := range allModes {
		if !availableSet[mode] {
			suppressed = append(suppressed, mode)
		}
	}
	if addAll {
		if len(available) == 0 {
			return nil, fmt.Errorf("no TypeScript task modes available; workspace-root npm scripts take priority for: %s", strings.Join(suppressed, ", "))
		}
		copied := append([]string(nil), available...)
		sort.Strings(copied)
		return copied, nil
	}
	if len(selected) == 0 {
		return chooseNamedItems(selected, available, addAll, "typescript task modes")
	}
	result := make([]string, 0, len(selected))
	for _, mode := range selected {
		if availableSet[mode] {
			result = append(result, mode)
		}
	}
	if len(result) > 0 {
		return result, nil
	}
	if len(suppressed) > 0 {
		return nil, fmt.Errorf("selected TypeScript task modes are provided by workspace-root npm scripts: %s", strings.Join(selected, ", "))
	}
	return nil, fmt.Errorf("select typescript task modes with --task or use --all")
}

func printAddSummary(writer io.Writer, path string, tasksToReport []tasks.Task) int {
	if len(tasksToReport) == 1 {
		fmt.Fprintf(writer, "added task %q to %s\n", tasksToReport[0].EffectiveLabel(), path)
		return 0
	}
	labels := make([]string, 0, len(tasksToReport))
	for _, task := range tasksToReport {
		labels = append(labels, task.EffectiveLabel())
	}
	sort.Strings(labels)
	fmt.Fprintf(writer, "added %d tasks to %s: %s\n", len(tasksToReport), path, strings.Join(labels, ", "))
	return 0
}

func generatedAction(task tasks.Task) string {
	group, ok := tasks.ParseTaskGroup(task.Group)
	if ok {
		return group.Kind
	}
	if task.EffectiveType() == "typescript" {
		if task.Option == "watch" {
			return "watch"
		}
		return "build"
	}
	return generatedActionLabel(task.EffectiveLabel())
}

func generatedActionLabel(label string) string {
	parts := strings.Split(label, "-")
	if len(parts) < 2 {
		return ""
	}
	switch parts[0] {
	case "npm", "tsc", "gulp", "grunt", "jake", "go", "cargo", "swift", "gradle", "maven":
		return parts[1]
	default:
		return ""
	}
}

func containsString(items []string, target string) bool {
	return slices.Contains(items, target)
}

func filterCandidates(candidates []tasks.TaskCandidate, ecosystems []string, labels []string, addAll bool, requireExplicitSelection bool) ([]tasks.TaskCandidate, error) {
	ecosystemSet := make(map[string]bool, len(ecosystems))
	for _, ecosystem := range ecosystems {
		ecosystemSet[ecosystem] = true
	}
	labelSet := make(map[string]bool, len(labels))
	for _, label := range labels {
		labelSet[label] = true
	}
	filtered := make([]tasks.TaskCandidate, 0, len(candidates))
	for _, candidate := range candidates {
		if len(ecosystemSet) > 0 && !ecosystemSet[candidate.Ecosystem] {
			continue
		}
		if len(labelSet) > 0 && !labelSet[candidate.Label] {
			continue
		}
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("no matching detected tasks")
	}
	if !requireExplicitSelection {
		return filtered, nil
	}
	if addAll || len(labels) > 0 || len(ecosystems) == 1 {
		return filtered, nil
	}
	if len(filtered) == 1 {
		return filtered, nil
	}
	available := make([]string, 0, len(filtered))
	for _, candidate := range filtered {
		available = append(available, candidate.Label)
	}
	return nil, fmt.Errorf("multiple detected tasks matched; use --all or --label (available: %s)", strings.Join(available, ", "))
}

func resolveProviderTasks(workspaceRoot string, taskType string, providerFile string, requestedTasks []string, addAll bool) ([]tasks.Task, error) {
	if providerFile != "" && len(requestedTasks) > 0 {
		items := make([]tasks.Task, 0, len(requestedTasks))
		for _, name := range requestedTasks {
			items = append(items, tasks.NewProviderTask(taskType, name, providerFile))
		}
		return items, nil
	}

	definitions, err := tasks.FindProviderTasks(workspaceRoot, taskType)
	if err != nil {
		return nil, err
	}
	if providerFile != "" {
		filtered := definitions[:0]
		for _, definition := range definitions {
			if definition.File == providerFile {
				filtered = append(filtered, definition)
			}
		}
		definitions = filtered
	}
	if len(definitions) == 0 {
		return nil, fmt.Errorf("no %s tasks detected", taskType)
	}

	selectedDefinitions, err := selectProviderDefinitions(definitions, requestedTasks, addAll, providerFile != "")
	if err != nil {
		return nil, err
	}
	items := make([]tasks.Task, 0, len(selectedDefinitions))
	for _, definition := range selectedDefinitions {
		items = append(items, tasks.NewProviderTask(taskType, definition.Task, definition.File))
	}
	return items, nil
}

func selectProviderDefinitions(definitions []tasks.ProviderTaskDefinition, requestedTasks []string, addAll bool, constrainedByFile bool) ([]tasks.ProviderTaskDefinition, error) {
	if addAll {
		return append([]tasks.ProviderTaskDefinition(nil), definitions...), nil
	}
	if len(requestedTasks) == 0 {
		if len(definitions) == 1 {
			return []tasks.ProviderTaskDefinition{definitions[0]}, nil
		}
		available := make([]string, 0, len(definitions))
		for _, definition := range definitions {
			available = append(available, providerSummary(definition))
		}
		return nil, fmt.Errorf("select provider task with --task or use --all (available: %s)", strings.Join(available, ", "))
	}
	index := make(map[string][]tasks.ProviderTaskDefinition)
	for _, definition := range definitions {
		index[definition.Task] = append(index[definition.Task], definition)
	}
	selected := make([]tasks.ProviderTaskDefinition, 0, len(requestedTasks))
	for _, name := range requestedTasks {
		matches := index[name]
		if len(matches) == 0 {
			return nil, fmt.Errorf("unknown provider task %q", name)
		}
		if len(matches) > 1 && !constrainedByFile {
			return nil, fmt.Errorf("provider task %q is defined in multiple files; use --file", name)
		}
		selected = append(selected, matches[0])
	}
	return selected, nil
}

func providerSummary(definition tasks.ProviderTaskDefinition) string {
	if definition.File == "" {
		return definition.Task
	}
	return definition.Task + "@" + definition.File
}
