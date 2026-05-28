package main

import (
	"fmt"
	"io"
	"os"
)

const version = "0.1.0-w0"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) != 1 {
		printUsage(stderr)
		return 2
	}

	switch args[0] {
	case "--version":
		fmt.Fprintln(stdout, version)
		return 0
	case "--health":
		fmt.Fprintln(stdout, `{"status":"ready"}`)
		return 0
	default:
		printUsage(stderr)
		return 2
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: qiankun-mcpd [--version|--health]")
}
