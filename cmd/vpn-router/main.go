package main

import (
	"fmt"
	"os"

	"vpn-router/internal/app"
	"vpn-router/internal/ux"
)

var version = "dev"

func main() {
	if err := app.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, ux.FriendlyError(err))
		os.Exit(1)
	}
}
