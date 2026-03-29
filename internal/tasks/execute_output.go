package tasks

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

type OutputMode string

const (
	OutputModeQuiet   OutputMode = "quiet"
	OutputModeDefault OutputMode = "default"
)

type ColorMode string

const (
	ColorModeAuto   ColorMode = "auto"
	ColorModeNever  ColorMode = "never"
	ColorModeAlways ColorMode = "always"
)

type RunnerOptions struct {
	OutputMode       OutputMode
	ColorMode        ColorMode
	Input            io.Reader
	InputFile        *os.File
	OutputFile       *os.File
	MetaFile         *os.File
	EventHandler     func(TaskEvent)
	TaskOutputWriter func(ResolvedTask) io.Writer
}

func normalizeRunnerOptions(options RunnerOptions) RunnerOptions {
	if options.OutputMode == "" {
		options.OutputMode = OutputModeQuiet
	}
	if options.ColorMode == "" {
		options.ColorMode = ColorModeAuto
	}
	return options
}

type TaskEventType string

const (
	TaskEventStart  TaskEventType = "task-start"
	TaskEventLine   TaskEventType = "task-line"
	TaskEventFinish TaskEventType = "task-finish"
	TaskEventSkip   TaskEventType = "task-skip"
)

type TaskEvent struct {
	Type         TaskEventType `json:"type"`
	TaskLabel    string        `json:"taskLabel"`
	DependsOn    []string      `json:"dependsOn,omitempty"`
	DependsOrder string        `json:"dependsOrder,omitempty"`
	Status       string        `json:"status,omitempty"`
	Line         string        `json:"line,omitempty"`
	ExitCode     int           `json:"exitCode,omitempty"`
	StartTime    time.Time     `json:"startTime,omitempty"`
	EndTime      time.Time     `json:"endTime,omitempty"`
}

func (r *Runner) printTaskStart(task ResolvedTask) {
	if r.options.OutputMode != OutputModeDefault {
		return
	}
	if r.stderr == nil {
		return
	}
	fmt.Fprintf(r.stderr, "%s %s\n", r.decorate("🚀 run", ansiBlue), task.Label)
	fmt.Fprintf(r.stderr, "   $ %s\n", DisplayTaskCommand(task))
	fmt.Fprintf(r.stderr, "   @ %s\n", task.Options.CWD)
}

func (r *Runner) printTaskFinish(label string, exitCode int, wallTime time.Duration, userTime time.Duration, systemTime time.Duration) {
	if r.options.OutputMode != OutputModeDefault {
		return
	}
	if r.stderr == nil {
		return
	}
	status := r.decorate("✅ OK", ansiGreen)
	if exitCode != 0 {
		status = r.decorate("❌ NG", ansiRed)
	}
	message := fmt.Sprintf("%s %s  wall=%s user=%s sys=%s", status, label, formatDuration(wallTime), formatDuration(userTime), formatDuration(systemTime))
	if exitCode != 0 {
		message = fmt.Sprintf("%s exit=%d", message, exitCode)
	}
	fmt.Fprintln(r.stderr, message)
}

func (r *Runner) decorate(text string, code string) string {
	if !r.metaColorEnabled() {
		return text
	}
	return code + text + ansiReset
}

func (r *Runner) metaColorEnabled() bool {
	switch r.options.ColorMode {
	case ColorModeNever:
		return false
	case ColorModeAlways:
		return true
	default:
		return isTerminalFile(r.options.MetaFile)
	}
}

func (r *Runner) shouldUsePTY() bool {
	if r.options.OutputFile == nil {
		return false
	}
	if r.options.ColorMode == ColorModeNever {
		return false
	}
	if r.options.ColorMode == ColorModeAlways {
		return true
	}
	return isTerminalFile(r.options.OutputFile)
}

func isTerminalFile(file *os.File) bool {
	return file != nil && term.IsTerminal(int(file.Fd()))
}

func colorizedEnv(base map[string]string, mode ColorMode) map[string]string {
	if len(base) == 0 && mode == ColorModeAuto {
		return nil
	}
	result := make(map[string]string, len(base)+4)
	for key, value := range base {
		result[key] = value
	}
	switch mode {
	case ColorModeNever:
		result["NO_COLOR"] = "1"
		result["CLICOLOR"] = "0"
		result["CLICOLOR_FORCE"] = "0"
		result["FORCE_COLOR"] = "0"
	case ColorModeAlways:
		result["NO_COLOR"] = ""
		result["CLICOLOR"] = "1"
		result["CLICOLOR_FORCE"] = "1"
		result["FORCE_COLOR"] = "1"
		if _, ok := result["TERM"]; !ok || strings.TrimSpace(result["TERM"]) == "" || result["TERM"] == "dumb" {
			result["TERM"] = "xterm-256color"
		}
	}
	return result
}

func DisplayTaskCommand(task ResolvedTask) string {
	if task.Type == "shell" {
		return renderShellCommandForDisplay(task)
	}
	parts := make([]string, 0, len(task.Args)+1)
	command := task.DisplayCommand
	if command == "" {
		command = task.Command
	}
	parts = append(parts, quoteDisplayPart(command))
	args := task.DisplayArgs
	if len(args) == 0 {
		args = task.Args
	}
	for _, arg := range args {
		parts = append(parts, quoteDisplayPart(arg))
	}
	return strings.Join(parts, " ")
}

func quoteDisplayPart(value string) string {
	if value == "" {
		return `""`
	}
	if strings.ContainsAny(value, " \t\n\"'") {
		return strconv.Quote(value)
	}
	return value
}

func processTimes(start time.Time, state *os.ProcessState) (time.Duration, time.Duration, time.Duration) {
	wallTime := time.Since(start)
	if state == nil {
		return wallTime, 0, 0
	}
	return wallTime, state.UserTime(), state.SystemTime()
}

func durationMilliseconds(duration time.Duration) int64 {
	if duration <= 0 {
		return 0
	}
	return duration.Milliseconds()
}

func formatDuration(duration time.Duration) string {
	if duration <= 0 {
		return "0ms"
	}
	if duration < time.Millisecond {
		return duration.Round(time.Microsecond).String()
	}
	return duration.Round(time.Millisecond).String()
}

const (
	ansiReset = "\x1b[0m"
	ansiRed   = "\x1b[31m"
	ansiGreen = "\x1b[32m"
	ansiBlue  = "\x1b[34m"
)
