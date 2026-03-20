package cli

import (
	"flag"
	"fmt"

	"vsc-taskrunner/internal/tasks"
)

func (a *App) runAddTarget(
	targetName string,
	commandName string,
	args []string,
	findRoots func(string) ([]string, error),
	buildTasks func(string, string) []tasks.Task,
) int {
	fs := flag.NewFlagSet("add "+commandName, flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var tasksPath string
	var workspaceRoot string
	var rootPath string
	var taskNames string
	var addAll bool

	fs.StringVar(&tasksPath, "tasks", "", "path to tasks.json")
	fs.StringVar(&workspaceRoot, "workspace", "", "workspace root")
	fs.StringVar(&rootPath, "path", "", "project root relative to workspace root")
	fs.StringVar(&taskNames, "task", "", "comma separated task names")
	fs.BoolVar(&addAll, "all", false, "add tasks for all detected project roots")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(a.stderr, "add %s does not accept positional arguments\n", commandName)
		return 2
	}

	loaderOptions, err := a.makeLoaderOptions(tasksPath, workspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	roots, err := findRoots(loaderOptions.WorkspaceRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	selectedRoots := roots
	if rootPath != "" {
		selectedRoots = []string{normalizeCLIPath(rootPath)}
	} else if !addAll {
		selected, err := chooseSingleRoot(roots, false, targetName+" project", "--path")
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		selectedRoots = []string{selected}
	}

	selectedActions := parseCSVList(taskNames)
	if len(selectedActions) == 0 && !addAll {
		selectedActions = []string{"build", "test"}
	}
	actions, err := chooseNamedItems(selectedActions, []string{"build", "test"}, addAll, targetName+" task names")
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	items := make([]tasks.Task, 0, len(selectedRoots)*len(actions))
	for _, root := range selectedRoots {
		for _, task := range buildTasks(loaderOptions.WorkspaceRoot, root) {
			if containsString(actions, generatedAction(task)) {
				items = append(items, task)
			}
		}
	}
	if len(items) == 0 {
		fmt.Fprintf(a.stderr, "no %s tasks selected\n", targetName)
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
