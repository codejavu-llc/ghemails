package main

import (
	"os"

	"github.com/codejavu-llc/ghemails/internal/app"
)

func main() {
	os.Exit(app.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
