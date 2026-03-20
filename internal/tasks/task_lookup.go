package tasks

import "sort"

type TaskLookupResult struct {
	Task       ResolvedTask
	Label      string
	Candidates []string
}

func (c *Catalog) LookupTask(name string) TaskLookupResult {
	if task, ok := c.Tasks[name]; ok {
		return TaskLookupResult{Task: task, Label: name}
	}

	if name == "build" || name == "test" {
		defaultCandidates := c.groupCandidates(name, true)
		if len(defaultCandidates) == 1 {
			label := defaultCandidates[0]
			return TaskLookupResult{Task: c.Tasks[label], Label: label}
		}
		if len(defaultCandidates) > 1 {
			return TaskLookupResult{Candidates: defaultCandidates}
		}

		groupCandidates := c.groupCandidates(name, false)
		if len(groupCandidates) > 0 {
			return TaskLookupResult{Candidates: groupCandidates}
		}
	}

	actionCandidates := c.actionCandidates(name)
	if len(actionCandidates) == 1 {
		label := actionCandidates[0]
		return TaskLookupResult{Task: c.Tasks[label], Label: label}
	}
	if len(actionCandidates) > 1 {
		return TaskLookupResult{Candidates: actionCandidates}
	}

	return TaskLookupResult{}
}

func (c *Catalog) groupCandidates(kind string, requireDefault bool) []string {
	result := make([]string, 0)
	for _, label := range c.Order {
		task := c.Tasks[label]
		group, ok := ParseTaskGroup(task.Group)
		if !ok || group.Kind != kind {
			continue
		}
		if requireDefault && !group.IsDefault {
			continue
		}
		result = append(result, label)
	}
	return result
}

func (c *Catalog) actionCandidates(action string) []string {
	result := make([]string, 0)
	for _, label := range c.Order {
		if generatedActionAlias(label) == action {
			result = append(result, label)
		}
	}
	sort.Strings(result)
	return result
}

func generatedActionAlias(label string) string {
	parts := splitCanonicalTaskLabel(label)
	if len(parts) < 2 {
		return ""
	}
	if !isGeneratedTaskTool(parts[0]) {
		return ""
	}
	return parts[1]
}

func splitCanonicalTaskLabel(label string) []string {
	parts := make([]string, 0, 4)
	current := ""
	for _, part := range []rune(label) {
		if part == '-' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
			continue
		}
		current += string(part)
	}
	if current != "" {
		parts = append(parts, current)
	}
	return parts
}

func isGeneratedTaskTool(tool string) bool {
	switch tool {
	case "npm", "tsc", "gulp", "grunt", "jake", "go", "cargo", "swift", "gradle", "maven":
		return true
	default:
		return false
	}
}
