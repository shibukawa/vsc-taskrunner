package tasks

import "encoding/json"

func builtinProblemMatchers() map[string]matcherConfig {
	matchers := make(map[string]matcherConfig)
	for _, target := range taskTargetDefinitions() {
		if target.problemMatchers == nil {
			continue
		}
		for name, config := range target.problemMatchers() {
			matchers[name] = config
		}
	}
	return matchers
}

func mustMarshal(value any) json.RawMessage {
	data, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return data
}
