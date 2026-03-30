package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	gitutil "vsc-taskrunner/internal/git"
	"vsc-taskrunner/internal/tasks"
	"vsc-taskrunner/internal/uiconfig"
	"golang.org/x/term"
)

func (a *App) runUIInit(args []string) int {
	fs := flag.NewFlagSet("ui init", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	var repoPath string
	var configPath string
	var write bool

	fs.StringVar(&repoPath, "repo", "", "git repository root")
	fs.StringVar(&configPath, "config", "", "path to runtask-ui.yaml")
	fs.BoolVar(&write, "write", true, "write generated config")

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintln(a.stderr, "ui init does not accept positional arguments")
		return 2
	}

	wd, err := a.wd()
	if err != nil {
		fmt.Fprintln(a.stderr, fmt.Errorf("resolve working directory: %w", err))
		return 1
	}
	repoRoot, err := resolveUIInitRepoRoot(wd, repoPath)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if configPath == "" {
		configPath = filepath.Join(repoRoot, "runtask-ui.yaml")
	}

	prompt := uiInitPrompter{
		reader:     bufio.NewReader(a.stdin),
		writer:     a.stdout,
		inputFile:  readerFile(a.stdin),
		outputFile: writerFile(a.stdout),
	}
	input, err := prompt.collect(repoRoot)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}

	cfg := input.Build()
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(a.stderr, fmt.Errorf("generated config is invalid: %w", err))
		return 1
	}
	content, err := uiconfig.MarshalGeneratedConfig(cfg)
	if err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	content = append(content, '\n')

	if !write {
		_, _ = a.stdout.Write(content)
		return 0
	}
	if _, err := os.Stat(configPath); err == nil {
		overwrite, err := promptConfirm(prompt.reader, a.stdout, "Overwrite existing config", false)
		if err != nil {
			fmt.Fprintln(a.stderr, err)
			return 1
		}
		if !overwrite {
			fmt.Fprintln(a.stdout, "ui init cancelled")
			return 1
		}
	} else if !os.IsNotExist(err) {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	if err := os.WriteFile(configPath, content, 0o644); err != nil {
		fmt.Fprintln(a.stderr, err)
		return 1
	}
	fmt.Fprintf(a.stdout, "wrote %s\n", configPath)
	return 0
}

type uiInitPrompter struct {
	reader     *bufio.Reader
	writer     io.Writer
	inputFile  *os.File
	outputFile *os.File
}

func (p uiInitPrompter) collect(repoRoot string) (uiconfig.GeneratedConfig, error) {
	branchOptions, branchDefaults, currentBranch, err := uiInitBranchChoices(repoRoot)
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	taskOptions, err := uiInitTaskChoices(repoRoot)
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}

	cfg := uiconfig.GeneratedConfig{
		RepositorySource: repoRoot,
		Port:             uiconfig.DefaultConfig().Server.Port,
		Storage: uiconfig.GeneratedStorage{
			Backend:    uiconfig.DefaultConfig().Storage.Backend,
			HistoryDir: uiconfig.DefaultConfig().Storage.HistoryDir,
		},
		MetricsEnabled:  uiconfig.DefaultConfig().Metrics.Enabled,
		MaxParallelRuns: uiconfig.DefaultConfig().Execution.MaxParallelRuns,
	}

	repositorySource, err := promptLine(p.reader, p.writer, "Repository source", cfg.RepositorySource)
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	cfg.RepositorySource = repositorySource

	if len(branchOptions) > 0 {
		selectedBranches, err := promptBranches(p.reader, p.writer, p.inputFile, p.outputFile, branchOptions, branchDefaults)
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		if len(selectedBranches) > 0 {
			defaultBranch, err := promptSelectedBranch(p.reader, p.writer, selectedBranches, currentBranch)
			if err != nil {
				return uiconfig.GeneratedConfig{}, err
			}
			cfg.Branches = reorderSelectedFirst(selectedBranches, defaultBranch)
		}
	}

	portText, err := promptLine(p.reader, p.writer, "Server port", strconv.Itoa(cfg.Port))
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return uiconfig.GeneratedConfig{}, fmt.Errorf("invalid port %q", portText)
	}
	cfg.Port = port

	selectedTasks, err := promptTasks(p.reader, p.writer, p.inputFile, p.outputFile, taskOptions, taskOptions, false)
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	cfg.Tasks = make([]uiconfig.GeneratedTask, 0, len(selectedTasks))
	for _, label := range selectedTasks {
		artifactPath, err := promptLine(p.reader, p.writer, fmt.Sprintf("Artifact path for %s (optional)", label), "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Tasks = append(cfg.Tasks, uiconfig.GeneratedTask{
			Label:        label,
			ArtifactPath: artifactPath,
		})
	}

	authMode, err := promptEnumChoice(p.reader, p.writer, "Auth mode [noauth/oidc]", "noauth", []string{"noauth", "oidc"})
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	if authMode == "noauth" {
		cfg.Auth.NoAuth = true
	} else {
		cfg.Auth.OIDCIssuer, err = promptRequiredLine(p.reader, p.writer, "OIDC issuer", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Auth.OIDCClientID, err = promptRequiredLine(p.reader, p.writer, "OIDC client ID", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Auth.OIDCClientSecret, err = promptRequiredLine(p.reader, p.writer, "OIDC client secret", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
	}

	storageBackend, err := promptEnumChoice(p.reader, p.writer, "Storage backend [local/object]", cfg.Storage.Backend, []string{"local", "object"})
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	cfg.Storage.Backend = storageBackend
	if storageBackend == "object" {
		cfg.Storage.ObjectEndpoint, err = promptRequiredLine(p.reader, p.writer, "Object storage endpoint", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Storage.ObjectBucket, err = promptRequiredLine(p.reader, p.writer, "Object storage bucket", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Storage.ObjectRegion, err = promptLine(p.reader, p.writer, "Object storage region", "us-east-1")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Storage.ObjectAccessKey, err = promptRequiredLine(p.reader, p.writer, "Object storage access key", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Storage.ObjectSecretKey, err = promptRequiredLine(p.reader, p.writer, "Object storage secret key", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Storage.ObjectPrefix, err = promptLine(p.reader, p.writer, "Object storage prefix", "")
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		forcePathStyle, err := promptConfirm(p.reader, p.writer, "Force path style", false)
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
		cfg.Storage.ObjectForcePathStyle = forcePathStyle
	} else {
		cfg.Storage.HistoryDir, err = promptLine(p.reader, p.writer, "History dir", cfg.Storage.HistoryDir)
		if err != nil {
			return uiconfig.GeneratedConfig{}, err
		}
	}

	metricsEnabled, err := promptConfirm(p.reader, p.writer, "Enable metrics", cfg.MetricsEnabled)
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	cfg.MetricsEnabled = metricsEnabled

	parallelText, err := promptLine(p.reader, p.writer, "Max parallel runs", strconv.Itoa(cfg.MaxParallelRuns))
	if err != nil {
		return uiconfig.GeneratedConfig{}, err
	}
	maxParallelRuns, err := strconv.Atoi(parallelText)
	if err != nil {
		return uiconfig.GeneratedConfig{}, fmt.Errorf("invalid max parallel runs %q", parallelText)
	}
	cfg.MaxParallelRuns = maxParallelRuns

	return cfg, nil
}

func resolveUIInitRepoRoot(wd string, repoPath string) (string, error) {
	target := wd
	if repoPath != "" {
		target = repoPath
	}
	repoRoot, err := gitutil.FindRepoRoot(target)
	if err != nil {
		return "", fmt.Errorf("resolve git repository root: %w", err)
	}
	return repoRoot, nil
}

func uiInitBranchChoices(repoRoot string) ([]string, []string, string, error) {
	branches, err := gitutil.ListBranches(repoRoot)
	if err != nil {
		return nil, nil, "", err
	}
	items := make([]string, 0, len(branches))
	defaults := make([]string, 0, len(branches))
	for _, branch := range branches {
		if branch.IsRemote {
			continue
		}
		items = append(items, branch.ShortName)
		if !strings.Contains(branch.ShortName, "/") {
			defaults = append(defaults, branch.ShortName)
		}
	}
	sort.Strings(items)
	sort.Strings(defaults)
	currentBranch, err := gitutil.CurrentBranch(repoRoot)
	if err != nil {
		return nil, nil, "", err
	}
	return items, defaults, currentBranch, nil
}

func uiInitTaskChoices(repoRoot string) ([]string, error) {
	options := tasks.ResolveLoadOptions("", repoRoot)
	file, err := tasks.LoadFile(options)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	labels := make([]string, 0, len(file.Tasks))
	seen := make(map[string]bool, len(file.Tasks))
	for _, task := range file.Tasks {
		label := task.EffectiveLabel()
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	sort.Strings(labels)
	return labels, nil
}

func promptRequiredLine(reader *bufio.Reader, writer io.Writer, label string, defaultValue string) (string, error) {
	value, err := promptLine(reader, writer, label, defaultValue)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return value, nil
}

func promptChoice(reader *bufio.Reader, writer io.Writer, label string, defaultValue string, allowed []string) (string, error) {
	value, err := promptLine(reader, writer, label, defaultValue)
	if err != nil {
		return "", err
	}
	value = strings.TrimSpace(value)
	for _, item := range allowed {
		if value == item {
			return value, nil
		}
	}
	return "", fmt.Errorf("invalid value %q (allowed: %s)", value, strings.Join(allowed, ", "))
}

func promptEnumChoice(reader *bufio.Reader, writer io.Writer, label string, defaultValue string, allowed []string) (string, error) {
	value, err := promptLine(reader, writer, label, defaultValue)
	if err != nil {
		return "", err
	}
	value = strings.ToLower(strings.TrimSpace(value))
	for _, item := range allowed {
		if value == item {
			return value, nil
		}
	}
	return "", fmt.Errorf("invalid value %q (allowed: %s)", value, strings.Join(allowed, ", "))
}

func promptBranches(reader *bufio.Reader, writer io.Writer, inputFile *os.File, outputFile *os.File, available []string, defaults []string) ([]string, error) {
	if canUseInteractiveTaskPicker(inputFile, outputFile) && len(available) > 1 {
		selected, err := promptMultiSelect(inputFile, outputFile, "Select branches with ↑/↓ and Space. Press Enter to confirm.", available, defaults)
		if err != nil {
			return nil, err
		}
		return selected, nil
	}
	defaultValue := strings.Join(defaults, ",")
	if len(available) > 0 {
		fmt.Fprintf(writer, "Available branches: %s\n", strings.Join(available, ", "))
	}
	value, err := promptLine(reader, writer, "Branches (comma separated labels)", defaultValue)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "-" {
		return nil, nil
	}
	selected := parseCSVList(value)
	for _, item := range selected {
		if !containsString(available, item) {
			return nil, fmt.Errorf("unknown branch %q", item)
		}
	}
	return selected, nil
}

func promptSelectedBranch(reader *bufio.Reader, writer io.Writer, selected []string, currentBranch string) (string, error) {
	if len(selected) == 0 {
		return "", nil
	}
	defaultValue := selected[0]
	if currentBranch != "" && containsString(selected, currentBranch) {
		defaultValue = currentBranch
	}
	return promptChoice(reader, writer, "Default branch", defaultValue, selected)
}

func promptTasks(reader *bufio.Reader, writer io.Writer, inputFile *os.File, outputFile *os.File, available []string, defaults []string, allowEmpty bool) ([]string, error) {
	if canUseInteractiveTaskPicker(inputFile, outputFile) && len(available) > 1 {
		selected, err := promptTaskMultiSelect(inputFile, outputFile, available, defaults)
		if err != nil {
			return nil, err
		}
		if !allowEmpty && len(selected) == 0 {
			return nil, fmt.Errorf("at least one task is required")
		}
		return selected, nil
	}
	if len(available) == 0 {
		value, err := promptRequiredLine(reader, writer, "Tasks (comma separated labels)", "")
		if err != nil {
			return nil, err
		}
		selected := parseCSVList(value)
		if len(selected) == 0 {
			return nil, fmt.Errorf("at least one task is required")
		}
		return selected, nil
	}
	fmt.Fprintf(writer, "Available tasks: %s\n", strings.Join(available, ", "))
	defaultValue := strings.Join(defaults, ",")
	value, err := promptLine(reader, writer, "Tasks (comma separated labels)", defaultValue)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(value) == "-" {
		if allowEmpty {
			return nil, nil
		}
		return nil, fmt.Errorf("at least one task is required")
	}
	selected := parseCSVList(value)
	if !allowEmpty && len(selected) == 0 {
		return nil, fmt.Errorf("at least one task is required")
	}
	for _, item := range selected {
		if !containsString(available, item) {
			return nil, fmt.Errorf("unknown task %q", item)
		}
	}
	return selected, nil
}

func canUseInteractiveTaskPicker(inputFile *os.File, outputFile *os.File) bool {
	return inputFile != nil &&
		outputFile != nil &&
		term.IsTerminal(int(inputFile.Fd())) &&
		term.IsTerminal(int(outputFile.Fd()))
}

func promptTaskMultiSelect(inputFile *os.File, outputFile *os.File, available []string, defaults []string) ([]string, error) {
	return promptMultiSelect(inputFile, outputFile, "Select tasks with ↑/↓ and Space. Press Enter to confirm.", available, defaults)
}

func promptMultiSelect(inputFile *os.File, outputFile *os.File, title string, available []string, defaults []string) ([]string, error) {
	state, err := term.MakeRaw(int(inputFile.Fd()))
	if err != nil {
		return nil, fmt.Errorf("enable interactive selection: %w", err)
	}
	defer func() {
		_ = term.Restore(int(inputFile.Fd()), state)
	}()

	selected := make([]bool, len(available))
	for index, item := range available {
		selected[index] = containsString(defaults, item)
	}
	cursor := 0
	renderedLines := 0

	redraw := func() error {
		if renderedLines > 0 {
			fmt.Fprintf(outputFile, "\x1b[%dA", renderedLines)
		}
		lines := []string{
			title,
		}
		for index, item := range available {
			marker := "[x]"
			if !selected[index] {
				marker = "[ ]"
			}
			prefix := "  "
			if index == cursor {
				prefix = "> "
			}
			lines = append(lines, fmt.Sprintf("%s%s %s", prefix, marker, item))
		}
		for _, line := range lines {
			fmt.Fprintf(outputFile, "\x1b[2K\r%s\n", line)
		}
		renderedLines = len(lines)
		return nil
	}

	if err := redraw(); err != nil {
		return nil, err
	}

	buffer := make([]byte, 3)
	for {
		n, err := inputFile.Read(buffer[:1])
		if err != nil {
			return nil, err
		}
		if n != 1 {
			continue
		}
		switch buffer[0] {
		case '\r', '\n':
			fmt.Fprint(outputFile, "\n")
			items := make([]string, 0, len(available))
			for index, item := range available {
				if selected[index] {
					items = append(items, item)
				}
			}
			return items, nil
		case ' ':
			selected[cursor] = !selected[cursor]
		case 'k':
			if cursor > 0 {
				cursor--
			}
		case 'j':
			if cursor < len(available)-1 {
				cursor++
			}
		case 0x03:
			return nil, fmt.Errorf("task selection cancelled")
		case 0x1b:
			if _, err := io.ReadFull(inputFile, buffer[1:3]); err != nil {
				return nil, err
			}
			if buffer[1] != '[' {
				break
			}
			switch buffer[2] {
			case 'A':
				if cursor > 0 {
					cursor--
				}
			case 'B':
				if cursor < len(available)-1 {
					cursor++
				}
			}
		}
		if err := redraw(); err != nil {
			return nil, err
		}
	}
}

func reorderSelectedFirst(selected []string, first string) []string {
	if first == "" || !containsString(selected, first) {
		return append([]string(nil), selected...)
	}
	items := make([]string, 0, len(selected))
	items = append(items, first)
	for _, item := range selected {
		if item != first {
			items = append(items, item)
		}
	}
	return items
}
