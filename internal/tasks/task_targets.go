package tasks

type AddTargetDefinition struct {
	CommandName string
	TargetName  string
	FindRoots   func(string) ([]string, error)
	BuildTasks  func(string, string) []Task
}

type taskTargetDefinition struct {
	name              string
	commandName       string
	findRoots         func(string) ([]string, error)
	buildTasks        func(string, string) []Task
	collectCandidates func(string) ([]TaskCandidate, error)
	problemMatchers   func() map[string]matcherConfig
}

func taskTargetDefinitions() []taskTargetDefinition {
	return []taskTargetDefinition{
		{
			name:              "javascript",
			collectCandidates: collectJavaScriptCandidates,
			problemMatchers:   javascriptProblemMatchers,
		},
		{
			name:              "typescript",
			collectCandidates: collectTypeScriptCandidates,
			problemMatchers:   typescriptProblemMatchers,
		},
		{
			name:        "go",
			commandName: "go",
			findRoots:   FindGoModules,
			buildTasks: func(_ string, root string) []Task {
				return NewGoTasks(root)
			},
			collectCandidates: collectGoCandidates,
			problemMatchers:   goProblemMatchers,
		},
		{
			name:        "rust",
			commandName: "rust",
			findRoots:   FindCargoProjects,
			buildTasks: func(_ string, root string) []Task {
				return NewCargoTasks(root)
			},
			collectCandidates: collectRustCandidates,
			problemMatchers:   rustProblemMatchers,
		},
		{
			name:        "swift",
			commandName: "swift",
			findRoots:   FindSwiftPackages,
			buildTasks: func(_ string, root string) []Task {
				return NewSwiftTasks(root)
			},
			collectCandidates: collectSwiftCandidates,
			problemMatchers:   swiftProblemMatchers,
		},
		{
			name:              "java-gradle",
			commandName:       "gradle",
			findRoots:         FindGradleProjects,
			buildTasks:        NewGradleTasks,
			collectCandidates: collectGradleCandidates,
			problemMatchers:   gradleProblemMatchers,
		},
		{
			name:              "java-maven",
			commandName:       "maven",
			findRoots:         FindMavenProjects,
			buildTasks:        NewMavenTasks,
			collectCandidates: collectMavenCandidates,
			problemMatchers:   mavenProblemMatchers,
		},
		{
			name:            "cpp",
			problemMatchers: cppProblemMatchers,
		},
	}
}

func AddTargetDefinitions() map[string]AddTargetDefinition {
	definitions := make(map[string]AddTargetDefinition)
	for _, target := range taskTargetDefinitions() {
		if target.commandName == "" || target.findRoots == nil || target.buildTasks == nil {
			continue
		}
		definitions[target.commandName] = AddTargetDefinition{
			CommandName: target.commandName,
			TargetName:  target.name,
			FindRoots:   target.findRoots,
			BuildTasks:  target.buildTasks,
		}
	}
	return definitions
}

func newTaskCandidate(ecosystem string, label string, task Task, detail string) TaskCandidate {
	return TaskCandidate{
		Ecosystem: ecosystem,
		Label:     label,
		Type:      task.EffectiveType(),
		Detail:    detail,
		Task:      task,
	}
}

func appendRootTaskCandidates(candidates []TaskCandidate, ecosystem string, tasks []Task, detail string) []TaskCandidate {
	for _, task := range tasks {
		candidates = append(candidates, newTaskCandidate(ecosystem, task.EffectiveLabel(), task, detail))
	}
	return candidates
}
