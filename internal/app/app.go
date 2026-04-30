package app

import (
	"router-manager/internal/cli"
	"router-manager/internal/config"
	"router-manager/internal/shell"
)

func Execute(version string) error {
	root := cli.NewRoot(cli.Options{
		Version: version,
		Paths:   config.DefaultPaths(),
		Runner:  shell.RealRunner{},
	})
	return root.Execute()
}
