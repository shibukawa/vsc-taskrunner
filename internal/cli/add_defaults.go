package cli

import (
	"fmt"
	"os"

	"vsc-taskrunner/internal/tasks"
)

func saveTasksForAdd(options tasks.LoadOptions, items []tasks.Task) ([]string, error) {
	warnings, err := assignDefaultTaskGroups(options, items)
	if err != nil {
		return nil, err
	}
	if err := tasks.SaveTasks(options, items); err != nil {
		return nil, err
	}
	return warnings, nil
}

func assignDefaultTaskGroups(options tasks.LoadOptions, items []tasks.Task) ([]string, error) {
	existingDefaults, err := existingDefaultTaskGroups(options)
	if err != nil {
		return nil, err
	}

	assignedInBatch := map[string]bool{}
	warned := map[string]bool{}
	warnings := make([]string, 0, 2)

	for index := range items {
		group, ok := tasks.ParseTaskGroup(items[index].Group)
		if !ok {
			continue
		}
		if group.Kind != "build" && group.Kind != "test" {
			continue
		}
		if existingDefaults[group.Kind] {
			items[index].Group = tasks.MustTaskGroup(group.Kind, false)
			if !warned[group.Kind] {
				warnings = append(warnings, fmt.Sprintf("A default %s task already exists, so the generated %s task was saved without isDefault: true.", group.Kind, group.Kind))
				warned[group.Kind] = true
			}
			continue
		}
		if assignedInBatch[group.Kind] {
			items[index].Group = tasks.MustTaskGroup(group.Kind, false)
			continue
		}
		items[index].Group = tasks.MustTaskGroup(group.Kind, true)
		assignedInBatch[group.Kind] = true
	}

	return warnings, nil
}

func existingDefaultTaskGroups(options tasks.LoadOptions) (map[string]bool, error) {
	result := map[string]bool{}
	if _, err := os.Stat(options.Path); os.IsNotExist(err) {
		return result, nil
	} else if err != nil {
		return nil, fmt.Errorf("stat tasks file %s: %w", options.Path, err)
	}

	file, err := tasks.LoadFile(options)
	if err != nil {
		return nil, err
	}
	for _, task := range file.Tasks {
		group, ok := tasks.ParseTaskGroup(task.Group)
		if ok && group.IsDefault {
			result[group.Kind] = true
		}
	}
	return result, nil
}

func printAddWarnings(writer *os.File, warnings []string) {
	for _, warning := range warnings {
		fmt.Fprintln(writer, warning)
	}
}
