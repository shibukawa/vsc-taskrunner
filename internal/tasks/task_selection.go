package tasks

import (
	"fmt"
	"sort"
)

type TaskLookupSelection struct {
	Label      string
	Candidates []string
}

type TaskDefinitionCatalog struct {
	WorkspaceRoot string
	TaskFilePath  string
	Tasks         map[string]Task
	Order         []string
}

func BuildTaskDefinitionCatalog(file *File, workspaceRoot string, taskFilePath string) *TaskDefinitionCatalog {
	defaults := TaskDefaults{
		Options:      file.Options,
		Presentation: file.Presentation,
		RunOptions:   file.RunOptions,
	}

	catalog := &TaskDefinitionCatalog{
		WorkspaceRoot: workspaceRoot,
		TaskFilePath:  taskFilePath,
		Tasks:         make(map[string]Task, len(file.Tasks)),
		Order:         make([]string, 0, len(file.Tasks)),
	}

	for _, task := range file.Tasks {
		merged := mergeTaskDefaults(task, &defaults)
		merged = mergeTaskDefaults(merged, selectDefaults(task.Windows, task.OSX, task.Linux))
		label := merged.Label
		if label == "" {
			label = inferTaskLabel(merged, workspaceRoot)
			merged.Label = label
		}
		catalog.Tasks[label] = merged
		catalog.Order = append(catalog.Order, label)
	}

	sort.Strings(catalog.Order)
	return catalog
}

func ResolveTaskSelection(catalog *TaskDefinitionCatalog, label string, options ResolveOptions) (*Catalog, error) {
	inputResolver, err := newInputResolver(options)
	if err != nil {
		return nil, err
	}

	resolvedCatalog := &Catalog{
		WorkspaceRoot: options.WorkspaceRoot,
		TaskFilePath:  options.TaskFilePath,
		Tasks:         make(map[string]ResolvedTask),
		Order:         make([]string, 0),
	}
	resolved := make(map[string]bool)
	visiting := make(map[string]bool)

	var resolveLabel func(string) error
	resolveLabel = func(name string) error {
		if resolved[name] {
			return nil
		}
		if visiting[name] {
			return nil
		}

		task, ok := catalog.Tasks[name]
		if !ok {
			return nil
		}

		visiting[name] = true
		for _, dep := range task.Dependencies.Labels() {
			if _, ok := catalog.Tasks[dep]; !ok {
				return fmt.Errorf("task %s depends on unknown task %s", task.Label, dep)
			}
			if err := resolveLabel(dep); err != nil {
				return err
			}
		}

		item, err := resolveTask(task, options, inputResolver)
		if err != nil {
			return fmt.Errorf("resolve task %s: %w", task.Label, err)
		}
		resolvedCatalog.Tasks[item.Label] = item
		resolvedCatalog.Order = append(resolvedCatalog.Order, item.Label)
		resolved[item.Label] = true
		delete(visiting, name)
		return nil
	}

	if _, ok := catalog.Tasks[label]; !ok {
		return resolvedCatalog, nil
	}
	if err := resolveLabel(label); err != nil {
		return nil, err
	}
	sort.Strings(resolvedCatalog.Order)
	return resolvedCatalog, nil
}

func ResolveTaskSelectionLabels(catalog *TaskDefinitionCatalog, label string, options ResolveOptions) ([]string, error) {
	resolvedCatalog, err := ResolveTaskSelection(catalog, label, options)
	if err != nil {
		return nil, err
	}
	if resolvedCatalog == nil || len(resolvedCatalog.Tasks) == 0 {
		return nil, nil
	}

	labels := make([]string, 0, len(resolvedCatalog.Tasks))
	for taskLabel := range resolvedCatalog.Tasks {
		labels = append(labels, taskLabel)
	}
	sort.Strings(labels)
	return labels, nil
}

func (c *TaskDefinitionCatalog) LookupTask(name string) TaskLookupSelection {
	return lookupTaskSelection(name, c.Order, func(label string) bool {
		_, ok := c.Tasks[label]
		return ok
	}, func(label string) []byte {
		return c.Tasks[label].Group
	})
}
