package tasks

import "sort"

type TaskLookupResult struct {
	Task       ResolvedTask
	Label      string
	Candidates []string
}

func (c *Catalog) LookupTask(name string) TaskLookupResult {
	selection := lookupTaskSelection(name, c.Order, func(label string) bool {
		_, ok := c.Tasks[label]
		return ok
	}, func(label string) []byte {
		return c.Tasks[label].Group
	})
	if selection.Label == "" {
		return TaskLookupResult{Candidates: selection.Candidates}
	}
	return TaskLookupResult{Task: c.Tasks[selection.Label], Label: selection.Label}
}

func lookupTaskSelection(name string, order []string, hasLabel func(string) bool, groupForLabel func(string) []byte) TaskLookupSelection {
	if hasLabel(name) {
		return TaskLookupSelection{Label: name}
	}

	if name == "build" || name == "test" {
		defaultCandidates := groupCandidates(order, kindMatches(name, true, groupForLabel))
		if len(defaultCandidates) == 1 {
			return TaskLookupSelection{Label: defaultCandidates[0]}
		}
		if len(defaultCandidates) > 1 {
			return TaskLookupSelection{Candidates: defaultCandidates}
		}

		candidates := groupCandidates(order, kindMatches(name, false, groupForLabel))
		if len(candidates) > 0 {
			return TaskLookupSelection{Candidates: candidates}
		}
	}

	actionCandidates := actionCandidates(order, name)
	if len(actionCandidates) == 1 {
		return TaskLookupSelection{Label: actionCandidates[0]}
	}
	if len(actionCandidates) > 1 {
		return TaskLookupSelection{Candidates: actionCandidates}
	}

	return TaskLookupSelection{}
}

func kindMatches(kind string, requireDefault bool, groupForLabel func(string) []byte) func(string) bool {
	return func(label string) bool {
		group, ok := ParseTaskGroup(groupForLabel(label))
		if !ok || group.Kind != kind {
			return false
		}
		if requireDefault && !group.IsDefault {
			return false
		}
		return true
	}
}

func groupCandidates(order []string, match func(string) bool) []string {
	result := make([]string, 0)
	for _, label := range order {
		if match(label) {
			result = append(result, label)
		}
	}
	return result
}

func actionCandidates(order []string, action string) []string {
	result := make([]string, 0)
	for _, label := range order {
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
