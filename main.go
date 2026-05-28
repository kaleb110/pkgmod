// pkgmod scans a JavaScript/TypeScript source tree, compares the imports
// it finds against the project's package.json, and installs anything missing.
//
// Usage:
//
//	pkgmod --src=./src
//	pkgmod --src=./src --manager=bun   # override when packageManager field is absent
//
// The package manager is resolved in this order:
//  1. packageManager field in package.json (e.g. "pnpm@9.1.0") — if present and supported
//  2. --manager flag — used when the field is absent
//
// package.json is always read from the current working directory (project root).
package main

import (
	"fmt"
	"os"

	"github.com/kaleb110/cmd"
)

func main() {
	if err := cmd.Run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}