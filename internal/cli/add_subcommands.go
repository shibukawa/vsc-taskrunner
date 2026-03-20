package cli

import "vsc-taskrunner/internal/tasks"

type addSubcommandHandler func(*App, []string) int

func addSubcommandHandlers() map[string]addSubcommandHandler {
	handlers := map[string]addSubcommandHandler{
		"detect": func(app *App, args []string) int {
			return app.runAddDetect(args)
		},
		"npm": func(app *App, args []string) int {
			return app.runAddNPM(args)
		},
		"typescript": func(app *App, args []string) int {
			return app.runAddTypeScript(args)
		},
		"gulp": func(app *App, args []string) int {
			return app.runAddProvider("gulp", args)
		},
		"grunt": func(app *App, args []string) int {
			return app.runAddProvider("grunt", args)
		},
		"jake": func(app *App, args []string) int {
			return app.runAddProvider("jake", args)
		},
	}
	for name, definition := range tasks.AddTargetDefinitions() {
		commandName := name
		def := definition
		handlers[commandName] = func(app *App, args []string) int {
			return app.runAddTarget(def.TargetName, commandName, args, def.FindRoots, def.BuildTasks)
		}
	}
	return handlers
}

func (a *App) runAddSubcommand(name string, args []string) (int, bool) {
	handler, ok := addSubcommandHandlers()[name]
	if !ok {
		return 0, false
	}
	return handler(a, args), true
}
