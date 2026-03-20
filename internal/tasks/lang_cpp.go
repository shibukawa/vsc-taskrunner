package tasks

func cppProblemMatchers() map[string]matcherConfig {
	return map[string]matcherConfig{
		"$msCompile": {
			Owner:        "msCompile",
			Source:       "cpp",
			Severity:     "error",
			FileLocation: mustMarshal("absolute"),
			Pattern: mustMarshal(patternConfig{
				Regexp:   `^\s*(?:\s*\d+>)?(\S.*?)(?:\((\d+|\d+,\d+|\d+,\d+,\d+,\d+)\))?\s*:\s+(?:(\S+)\s+)?((?:fatal +)?error|warning|info)\s+(\w+\d+)?\s*:\s*(.*)$`,
				File:     1,
				Location: 2,
				Severity: 4,
				Code:     5,
				Message:  6,
			}),
		},
	}
}
