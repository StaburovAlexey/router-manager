package app

import (
	"vpn-router/internal/cli"
	"vpn-router/internal/config"
	"vpn-router/internal/shell"
)

func Execute(version string) error {
	root := cli.NewRoot(cli.Options{
		Version: version,
		Paths:   config.DefaultPaths(),
		Runner:  shell.RealRunner{},
	})
	return root.Execute()
}
