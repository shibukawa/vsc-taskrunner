package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tailscale/hujson"
)

func SaveTask(options LoadOptions, task Task) error {
	return SaveTasks(options, []Task{task})
}

func SaveTasks(options LoadOptions, tasks []Task) error {
	prepared := make([]Task, 0, len(tasks))
	seen := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		normalized, err := prepareTaskForSave(task, options.WorkspaceRoot)
		if err != nil {
			return err
		}
		if seen[normalized.EffectiveLabel()] {
			return fmt.Errorf("duplicate task in request: %s", normalized.EffectiveLabel())
		}
		seen[normalized.EffectiveLabel()] = true
		prepared = append(prepared, normalized)
	}

	if err := os.MkdirAll(filepath.Dir(options.Path), 0o755); err != nil {
		return fmt.Errorf("create tasks directory: %w", err)
	}

	if _, err := os.Stat(options.Path); errors.Is(err, os.ErrNotExist) {
		return createTasksFile(options.Path, prepared)
	} else if err != nil {
		return fmt.Errorf("stat tasks file %s: %w", options.Path, err)
	}

	file, err := LoadFile(options)
	if err != nil {
		return err
	}
	for _, existing := range file.Tasks {
		for _, task := range prepared {
			if existing.EffectiveLabel() == task.EffectiveLabel() {
				return fmt.Errorf("task already exists: %s", task.EffectiveLabel())
			}
		}
	}

	content, err := os.ReadFile(options.Path)
	if err != nil {
		return fmt.Errorf("read tasks file %s: %w", options.Path, err)
	}

	root, err := hujson.Parse(content)
	if err != nil {
		return fmt.Errorf("parse tasks file %s: %w", options.Path, err)
	}

	operations := make([]map[string]any, 0, len(prepared))
	for _, task := range prepared {
		operations = append(operations, map[string]any{
			"op":    "add",
			"path":  "/tasks/-",
			"value": task,
		})
	}
	patch, err := json.Marshal(operations)
	if err != nil {
		return fmt.Errorf("encode task patch: %w", err)
	}

	if err := root.Patch(patch); err != nil {
		return fmt.Errorf("append task: %w", err)
	}
	root.Format()

	if err := os.WriteFile(options.Path, root.Pack(), 0o644); err != nil {
		return fmt.Errorf("write tasks file %s: %w", options.Path, err)
	}
	return nil
}

func ValidateTaskForSave(task Task) error {
	switch task.EffectiveType() {
	case "shell", "process":
		if task.EffectiveLabel() == "" {
			return fmt.Errorf("label is required")
		}
		if !task.Command.Set || task.Command.Value == "" {
			return fmt.Errorf("command is required")
		}
		return nil
	case "npm":
		if task.Script == "" {
			return fmt.Errorf("npm task requires script")
		}
		return nil
	case "typescript":
		if task.TSConfig == "" {
			return fmt.Errorf("typescript task requires tsconfig")
		}
		return nil
	case "gulp", "grunt", "jake":
		if task.ProviderTask == "" {
			return fmt.Errorf("%s task requires task", task.EffectiveType())
		}
		return nil
	default:
		return fmt.Errorf("unsupported task type: %s", task.EffectiveType())
	}
}

func prepareTaskForSave(task Task, workspaceRoot string) (Task, error) {
	if task.Label == "" {
		if inferred := inferTaskLabel(task, workspaceRoot); inferred != "" {
			task.Label = inferred
		}
	}
	if err := ValidateTaskForSave(task); err != nil {
		return Task{}, err
	}
	return task, nil
}

func createTasksFile(path string, tasks []Task) error {
	file := File{
		Version: "2.0.0",
		Tasks:   tasks,
	}
	content, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode tasks file: %w", err)
	}
	formatted, err := hujson.Format(content)
	if err != nil {
		return fmt.Errorf("format tasks file: %w", err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("write tasks file %s: %w", path, err)
	}
	return nil
}
