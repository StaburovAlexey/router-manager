package main

import (
	"fmt"
	"os"

	"router-manager/internal/app"
	"router-manager/internal/ux"
)

var version = "dev"

func main() {
	if err := app.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, ux.FriendlyError(err))
		os.Exit(1)
	}
}
