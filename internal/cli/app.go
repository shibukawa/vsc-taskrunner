package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"vsc-taskrunner/internal/tasks"
)

type App struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
	env    []string
	wd     func() (string, error)
}

func NewApp(stdin io.Reader, stdout io.Writer, stderr io.Writer) *App {
	return &App{
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
		env:    os.Environ(),
		wd:     os.Getwd,
	}
}

func (a *App) Run(args []string) int {
	if len(args) == 0 {
		a.printUsage()
		return 2
	}

	switch args[0] {
	case "list":
		return a.runList(args[1:])
	case "add":
		return a.runAdd(args[1:])
	case "run":
		return a.runTask(args[1:])
	case "help", "-h", "--help":
		a.printUsage()
		return 0
	default:
		return a.runTask(args)
	}
}

func (a *App) printUsage() {
	fmt.Fprintln(a.stderr, "Usage:")
	fmt.Fprintln(a.stderr, "  runtask list [--tasks path] [--workspace path] [--json]")
	fmt.Fprintln(a.stderr, "  runtask add [--tasks path] [--workspace path]")
	fmt.Fprintln(a.stderr, "  runtask add detect [--workspace path] [--json] [--save] [--ecosystem name[,name]] [--label label[,label]] [--all]")
	fmt.Fprintln(a.stderr, "  runtask add npm [--workspace path] [--path dir] [--task name[,name]] [--all]")
	fmt.Fprintln(a.stderr, "  runtask add typescript [--workspace path] [--tsconfig path] [--task build,watch] [--all]")
	fmt.Fprintln(a.stderr, "  runtask add <gulp|grunt|jake> [--task name[,name]] [--file path] [--all]")
	fmt.Fprintln(a.stderr, "  runtask add <go|rust|swift|gradle|maven> [--workspace path] [--path dir] [--task build,test[,bench,cover,lint]] [--all]")
	fmt.Fprintln(a.stderr, "    target aliases: gradle -> java-gradle, maven -> java-maven")
	fmt.Fprintln(a.stderr, "  runtask <task-name> [--tasks path] [--workspace path] [--input key=value] [--json] [--dry-run]")
	fmt.Fprintln(a.stderr, "  runtask run <task-name> [--tasks path] [--workspace path] [--input key=value] [--json] [--dry-run]")
}

func (a *App) runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var jsonOutput bool

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.BoolVar(&jsonOutput, "json", false, "print JSON")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	file, err := tasks.LoadFile(loaderOptions)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	type listedTask struct {
		Label         string   `json:"label"`
		Type          string   `json:"type"`
		Group         string   `json:"group,omitempty"`
		DependsOn     []string `json:"dependsOn,omitempty"`
		Hidden        bool     `json:"hidden,omitempty"`
		Background    bool     `json:"background,omitempty"`
		HasMatcher    bool     `json:"hasProblemMatcher,omitempty"`
		SourceTaskID  string   `json:"sourceTaskId,omitempty"`
		WorkspaceRoot string   `json:"workspaceRoot,omitempty"`
		TaskFilePath  string   `json:"taskFilePath,omitempty"`
	}

	items := make([]listedTask, 0, len(file.Tasks))
	for _, task := range file.Tasks {
		items = append(items, listedTask{
			Label:         task.Label,
			Type:          task.EffectiveType(),
			Group:         taskGroupName(task.Group),
			DependsOn:     task.Dependencies.Labels(),
			Hidden:        task.Hide,
			Background:    task.IsBackground,
			HasMatcher:    len(task.ProblemMatcher) > 0,
			SourceTaskID:  task.Identifier,
			WorkspaceRoot: loaderOptions.WorkspaceRoot,
			TaskFilePath:  loaderOptions.Path,
		})
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].Label < items[j].Label
	})

	if jsonOutput {
		return writeJSON(a.stdout, items)
	}

	for _, item := range items {
		fmt.Fprintf(a.stdout, "%s\t%s", item.Label, item.Type)
		if item.Group != "" {
			fmt.Fprintf(a.stdout, "\tgroup=%s", item.Group)
		}
		if len(item.DependsOn) > 0 {
			fmt.Fprintf(a.stdout, "\tdependsOn=%s", strings.Join(item.DependsOn, ","))
		}
		if item.Hidden {
			fmt.Fprint(a.stdout, "\thidden")
		}
		if item.Background {
			fmt.Fprint(a.stdout, "\tbackground")
		}
		fmt.Fprintln(a.stdout)
	}

	return 0
}

func (a *App) runTask(args []string) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var jsonOutput bool
	var dryRun bool
	var inputAssignments multiFlag

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.BoolVar(&jsonOutput, "json", false, "print JSON")
	fs.BoolVar(&dryRun, "dry-run", false, "resolve but do not execute")
	fs.Var(&inputAssignments, "input", "input value in key=value form; repeatable")

	flagArgs, taskName, err := splitRunArgs(args)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 2
	}

	if err := fs.Parse(flagArgs); err != nil {
		return 2
	}

	if taskName == "" || fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "run requires exactly one task label")
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	file, err := tasks.LoadFile(loaderOptions)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	resolverOptions := tasks.ResolveOptions{
		WorkspaceRoot:  loaderOptions.WorkspaceRoot,
		TaskFilePath:   loaderOptions.Path,
		InputValues:    inputAssignments.Map(),
		Env:            a.env,
		Stdin:          a.stdin,
		Stdout:         a.stdout,
		NonInteractive: false,
	}
	if jsonOutput {
		resolverOptions.Stdout = io.Discard
	}

	catalog, err := tasks.ResolveFile(file, resolverOptions)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	lookup := catalog.LookupTask(taskName)
	if lookup.Label == "" {
		if len(lookup.Candidates) > 0 {
			if taskName == "build" || taskName == "test" {
				fmt.Fprintf(a.stderr, "no default %s task is configured\n", taskName)
				fmt.Fprintf(a.stderr, "available %s tasks: %s\n", taskName, strings.Join(lookup.Candidates, ", "))
			} else {
				fmt.Fprintf(a.stderr, "task selector is ambiguous: %s\n", taskName)
				fmt.Fprintf(a.stderr, "matching tasks: %s\n", strings.Join(lookup.Candidates, ", "))
			}
			return 1
		}
		fmt.Fprintf(a.stderr, "task not found: %s\n", taskName)
		fmt.Fprintln(a.stderr, "use 'runtask add' to create a new task")
		return 1
	}
	resolvedTask := lookup.Task
	resolvedLabel := lookup.Label

	if dryRun {
		if jsonOutput {
			return writeJSON(a.stdout, resolvedTask)
		}
		printDryRun(a.stdout, resolvedTask, catalog)
		return 0
	}

	stdout := a.stdout
	stderr := a.stderr
	if jsonOutput {
		stdout = a.stderr
		stderr = a.stderr
	}

	runner := tasks.NewRunner(catalog, stdout, stderr)
	result, err := runner.Run(resolvedLabel)
	if err != nil && !errors.Is(err, tasks.ErrTaskFailed) {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	if jsonOutput {
		return writeJSON(a.stdout, result)
	}

	printProblems(a.stderr, result.Problems)

	if err != nil {
		fmt.Fprintf(a.stderr, "task failed: %s\n", resolvedLabel)
	}

	return result.ExitCode
}

func splitRunArgs(args []string) ([]string, string, error) {
	flagArgs := make([]string, 0, len(args))
	taskName := ""
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if arg == "--" {
			remaining := args[index+1:]
			if len(remaining) != 1 || taskName != "" {
				return nil, "", fmt.Errorf("run requires exactly one task label")
			}
			return flagArgs, remaining[0], nil
		}
		if strings.HasPrefix(arg, "-") {
			flagArgs = append(flagArgs, arg)
			if runFlagNeedsValue(arg) {
				if index+1 >= len(args) {
					return nil, "", fmt.Errorf("flag needs an argument: %s", arg)
				}
				index++
				flagArgs = append(flagArgs, args[index])
			}
			continue
		}
		if taskName != "" {
			return nil, "", fmt.Errorf("run requires exactly one task label")
		}
		taskName = arg
	}
	return flagArgs, taskName, nil
}

func runFlagNeedsValue(arg string) bool {
	if strings.Contains(arg, "=") {
		return false
	}
	switch arg {
	case "--tasks", "--workspace", "--input":
		return true
	default:
		return false
	}
}

func (a *App) makeLoaderOptions(tasksPath string, workspaceRoot string) (tasks.LoadOptions, error) {
	var err error
	if workspaceRoot == "" {
		workspaceRoot, err = a.wd()
		if err != nil {
			return tasks.LoadOptions{}, fmt.Errorf("resolve workspace root: %w", err)
		}
	}

	return tasks.ResolveLoadOptions(tasksPath, workspaceRoot), nil
}

type multiFlag []string

func (m *multiFlag) String() string {
	return strings.Join(*m, ",")
}

func (m *multiFlag) Set(value string) error {
	if !strings.Contains(value, "=") {
		return fmt.Errorf("input must be key=value: %s", value)
	}
	*m = append(*m, value)
	return nil
}

func (m multiFlag) Map() map[string]string {
	result := make(map[string]string, len(m))
	for _, item := range m {
		parts := strings.SplitN(item, "=", 2)
		result[parts[0]] = parts[1]
	}
	return result
}

func writeJSON(writer io.Writer, value any) int {
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return 1
	}
	return 0
}

func printDryRun(writer io.Writer, task tasks.ResolvedTask, catalog *tasks.Catalog) {
	seen := make(map[string]bool)
	printDryRunTask(writer, task, catalog, seen, 0)
}

func printDryRunTask(writer io.Writer, task tasks.ResolvedTask, catalog *tasks.Catalog, seen map[string]bool, depth int) {
	indent := strings.Repeat("  ", depth)
	if seen[task.Label] {
		fmt.Fprintf(writer, "%s- %s (already shown)\n", indent, task.Label)
		return
	}
	seen[task.Label] = true

	fmt.Fprintf(writer, "%s- %s\n", indent, task.Label)
	fmt.Fprintf(writer, "%s  type: %s\n", indent, task.Type)
	fmt.Fprintf(writer, "%s  cwd: %s\n", indent, task.Options.CWD)
	fmt.Fprintf(writer, "%s  command: %s\n", indent, task.Command)
	if len(task.Args) > 0 {
		fmt.Fprintf(writer, "%s  args: %s\n", indent, strings.Join(task.Args, " | "))
	}
	if group := taskGroupName(task.Group); group != "" {
		fmt.Fprintf(writer, "%s  group: %s\n", indent, group)
	}
	if len(task.DependsOn) > 0 {
		fmt.Fprintf(writer, "%s  dependsOn: %s\n", indent, strings.Join(task.DependsOn, ", "))
	}
	if len(task.Options.Env) > 0 {
		keys := make([]string, 0, len(task.Options.Env))
		for key := range task.Options.Env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		fmt.Fprintf(writer, "%s  env: %s\n", indent, strings.Join(keys, ", "))
	}

	for _, dep := range task.DependsOn {
		if child, ok := catalog.Tasks[dep]; ok {
			printDryRunTask(writer, child, catalog, seen, depth+1)
		}
	}
}

func printProblems(writer io.Writer, problems []tasks.Problem) {
	for _, problem := range problems {
		location := problem.File
		if problem.Line > 0 {
			location = fmt.Sprintf("%s:%d", location, problem.Line)
			if problem.Column > 0 {
				location = fmt.Sprintf("%s:%d", location, problem.Column)
			}
		}
		if location == "" {
			location = "<unknown>"
		}
		fmt.Fprintf(writer, "problem [%s] %s: %s\n", problem.Severity, location, problem.Message)
	}
}

func taskGroupName(raw json.RawMessage) string {
	group, ok := tasks.ParseTaskGroup(raw)
	if ok {
		return group.Kind
	}
	return ""
}
