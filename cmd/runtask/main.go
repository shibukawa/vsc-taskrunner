package main

import (
	"os"

	"vsc-taskrunner/internal/cli"
)

func main() {
	app := cli.NewApp(os.Stdin, os.Stdout, os.Stderr)
	os.Exit(app.Run(os.Args[1:]))
}
