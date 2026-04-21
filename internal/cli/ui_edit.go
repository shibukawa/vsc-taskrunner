package cli

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"vsc-taskrunner/internal/uiconfig"
)

func (a *App) runUIEdit(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(a.stderr, "ui edit requires a target: task or branch")
		return 2
	}
	switch args[0] {
	case "task":
		return a.runUIEditTask(args[1:])
	case "branch":
		return a.runUIEditBranch(args[1:])
	default:
		fmt.Fprintln(a.stderr, "ui edit requires a target: task or branch")
		return 2
	}
}

func (a *App) runUIEditTask(args []string) int {
	fs := flag.NewFlagSet("ui edit task", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var repoPath string
	var configPath string

	fs.StringVar(&repoPath, "repo", "", "git repository root")
	fs.StringVar(&configPath, "config", "", "path to runtask-ui.yaml")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "ui edit task does not accept positional arguments")
		return 2
	}

	repoRoot, cfgPath, cfg, prompt, ok := a.loadEditableUIConfig(repoPath, configPath)
	if !ok {
		return 1
	}
	taskOptions, _, err := uiInitTaskChoices(repoRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	currentTasks := taskPatterns(cfg.Tasks)
	if len(currentTasks) > 0 {
		fmt.Fprintf(a.stdout, "Configured tasks: %s\n", strings.Join(currentTasks, ", "))
	} else {
		fmt.Fprintln(a.stdout, "Configured tasks: (none)")
	}
	taskOptions = mergeUniqueStrings(taskOptions, currentTasks)
	selectedTasks, err := promptTasks(prompt.reader, a.stdout, prompt.inputFile, prompt.outputFile, taskOptions, currentTasks, true)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	selectedTasks = reorderSelectedFirst(selectedTasks, firstSelectedCurrentTask(selectedTasks, currentTasks))
	previousTasks := append([]string(nil), currentTasks...)
	existingTaskConfigs := make(map[string]uiconfig.TaskUIConfig, len(cfg.Tasks))
	for _, spec := range cfg.Tasks {
		existingTaskConfigs[spec.Pattern] = spec.Config
	}
	cfg.Tasks = nil
	for _, label := range selectedTasks {
		taskCfg := existingTaskConfigs[label]
		artifactPath := ""
		if len(taskCfg.Artifacts) > 0 {
			artifactPath = taskCfg.Artifacts[0].Path
		}
		artifactPath, err = promptLine(prompt.reader, a.stdout, fmt.Sprintf("Artifact path for %s (optional)", label), artifactPath)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		if artifactPath == "" {
			taskCfg.Artifacts = nil
		} else {
			taskCfg.Artifacts = []uiconfig.ArtifactRuleConfig{{Path: artifactPath}}
		}
		cfg.SetTaskConfig(label, taskCfg)
	}

	return a.saveEditedUIConfig(cfgPath, cfg, summarizeTaskEdit(previousTasks, selectedTasks), prompt.reader)
}

func (a *App) runUIEditBranch(args []string) int {
	fs := flag.NewFlagSet("ui edit branch", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var repoPath string
	var configPath string

	fs.StringVar(&repoPath, "repo", "", "git repository root")
	fs.StringVar(&configPath, "config", "", "path to runtask-ui.yaml")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "ui edit branch does not accept positional arguments")
		return 2
	}

	repoRoot, cfgPath, cfg, prompt, ok := a.loadEditableUIConfig(repoPath, configPath)
	if !ok {
		return 1
	}
	branchOptions, _, currentBranch, err := uiInitBranchChoices(repoRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	branchOptions = mergeUniqueStrings(branchOptions, cfg.Branches)

	if len(cfg.Branches) > 0 {
		fmt.Fprintf(a.stdout, "Configured branches: %s\n", strings.Join(cfg.Branches, ", "))
	} else {
		fmt.Fprintln(a.stdout, "Configured branches: (none; top-level local branches are allowed by default)")
	}
	previousBranches := append([]string(nil), cfg.Branches...)
	selectedBranches, err := promptBranches(prompt.reader, a.stdout, prompt.inputFile, prompt.outputFile, branchOptions, cfg.Branches)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	defaultBranch := ""
	if len(selectedBranches) > 0 {
		defaultBranch, err = promptSelectedBranch(prompt.reader, a.stdout, selectedBranches, currentBranch)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
	}
	cfg.Branches = reorderSelectedFirst(selectedBranches, defaultBranch)
	if len(cfg.Branches) == 0 {
		fmt.Fprintln(a.stdout, "No explicit branches selected. The default rule will allow only top-level local branches.")
	}

	return a.saveEditedUIConfig(cfgPath, cfg, summarizeBranchEdit(previousBranches, cfg.Branches), prompt.reader)
}

type uiPrompt struct {
	reader     *bufio.Reader
	inputFile  *os.File
	outputFile *os.File
}

func (a *App) loadEditableUIConfig(repoPath string, configPath string) (string, string, *uiconfig.UIConfig, uiPrompt, bool) {
	wd, err := a.wd()
	if err != nil {
		fmt.Fprintln(a.stderr, fmt.Errorf("resolve working directory: %w", err))
		return "", "", nil, uiPrompt{}, false
	}
	repoRoot, err := resolveUIInitRepoRoot(wd, repoPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return "", "", nil, uiPrompt{}, false
	}
	if configPath == "" {
		configPath = filepath.Join(repoRoot, "runtask-ui.yaml")
	}
	cfg, err := uiconfig.LoadConfig(configPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return "", "", nil, uiPrompt{}, false
	}
	return repoRoot, configPath, cfg, uiPrompt{
		reader:     bufio.NewReader(a.stdin),
		inputFile:  readerFile(a.stdin),
		outputFile: writerFile(a.stdout),
	}, true
}

func (a *App) saveEditedUIConfig(configPath string, cfg *uiconfig.UIConfig, summary string, reader *bufio.Reader) int {
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(a.stderr, fmt.Errorf("edited config is invalid: %w", err))
		return 1
	}
	content, err := uiconfig.MarshalConfig(cfg)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	content = append(content, '\n')

	fmt.Fprintf(a.stdout, "Planned changes: %s\n", summary)
	save, err := promptConfirm(reader, a.stdout, "Save changes", true)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if !save {
		fmt.Fprintln(a.stdout, "ui edit cancelled")
		return 1
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	fmt.Fprintf(a.stdout, "updated %s\n", configPath)
	return 0
}

func summarizeBranchEdit(previous []string, current []string) string {
	if len(current) == 0 {
		return "clear explicit branches and return to top-level local branch defaults"
	}
	if strings.Join(previous, ",") == strings.Join(current, ",") {
		return fmt.Sprintf("keep branches as %s", strings.Join(current, ", "))
	}
	return fmt.Sprintf("set branches to %s", strings.Join(current, ", "))
}

func summarizeTaskEdit(previous []string, current []string) string {
	if len(current) == 0 {
		return "clear all explicit tasks"
	}
	if strings.Join(previous, ",") == strings.Join(current, ",") {
		return fmt.Sprintf("keep tasks as %s", strings.Join(current, ", "))
	}
	return fmt.Sprintf("set tasks to %s", strings.Join(current, ", "))
}

func firstSelectedCurrentTask(selected []string, current []string) string {
	for _, item := range current {
		if containsString(selected, item) {
			return item
		}
	}
	if len(selected) == 0 {
		return ""
	}
	return selected[0]
}

func mergeUniqueStrings(left []string, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	items := make([]string, 0, len(left)+len(right))
	for _, item := range left {
		if seen[item] {
			continue
		}
		seen[item] = true
		items = append(items, item)
	}
	for _, item := range right {
		if seen[item] {
			continue
		}
		seen[item] = true
		items = append(items, item)
	}
	return items
}

func taskPatterns(specs uiconfig.AllowedTaskSpecs) []string {
	items := make([]string, 0, len(specs))
	for _, spec := range specs {
		items = append(items, spec.Pattern)
	}
	return items
}
