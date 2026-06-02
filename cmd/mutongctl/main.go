package main

import (
	"fmt"
	"os"
	"strings"
)

// version is set via -ldflags at build time.
var version = "dev"

var exitCode int

func main() {
	defer func() { os.Exit(exitCode) }()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		exitCode = extractExitCode(err)
	}
}

func extractExitCode(err error) int {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "exit_code=1"):
		return 1
	case strings.Contains(msg, "exit_code=2"):
		return 2
	case strings.Contains(msg, "exit_code=3"):
		return 3
	case strings.Contains(msg, "exit_code=4"):
		return 4
	case strings.Contains(msg, "exit_code=5"):
		return 5
	default:
		return 1
	}
}
