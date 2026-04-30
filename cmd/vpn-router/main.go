package main

import (
	"fmt"
	"os"

	"vpn-router/internal/app"
)

var version = "dev"

func main() {
	if err := app.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
