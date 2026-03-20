package tasks

import "encoding/json"

type TaskGroup struct {
	Kind      string `json:"kind"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

func ParseTaskGroup(raw json.RawMessage) (TaskGroup, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return TaskGroup{}, false
	}

	var kind string
	if err := json.Unmarshal(raw, &kind); err == nil {
		if kind == "" {
			return TaskGroup{}, false
		}
		return TaskGroup{Kind: kind}, true
	}

	var group TaskGroup
	if err := json.Unmarshal(raw, &group); err != nil || group.Kind == "" {
		return TaskGroup{}, false
	}
	return group, true
}

func MustTaskGroup(kind string, isDefault bool) json.RawMessage {
	if kind == "" {
		return nil
	}
	if !isDefault {
		encoded, err := json.Marshal(kind)
		if err != nil {
			panic(err)
		}
		return json.RawMessage(encoded)
	}
	encoded, err := json.Marshal(TaskGroup{Kind: kind, IsDefault: true})
	if err != nil {
		panic(err)
	}
	return json.RawMessage(encoded)
}
