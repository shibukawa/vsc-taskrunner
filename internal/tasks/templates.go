package tasks

import (
	"sort"
)

type TaskCandidate struct {
	Ecosystem string `json:"ecosystem"`
	Label     string `json:"label"`
	Type      string `json:"type"`
	Detail    string `json:"detail,omitempty"`
	Task      Task   `json:"task"`
}

func DetectTaskCandidates(workspaceRoot string) ([]TaskCandidate, error) {
	candidates := make([]TaskCandidate, 0)
	for _, target := range taskTargetDefinitions() {
		if target.collectCandidates == nil {
			continue
		}
		items, err := target.collectCandidates(workspaceRoot)
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, items...)
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Ecosystem != candidates[j].Ecosystem {
			return candidates[i].Ecosystem < candidates[j].Ecosystem
		}
		if candidates[i].Label != candidates[j].Label {
			return candidates[i].Label < candidates[j].Label
		}
		return candidates[i].Detail < candidates[j].Detail
	})

	return candidates, nil
}
