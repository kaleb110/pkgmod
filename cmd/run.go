// Package cmd wires together the scanner, deps loader, and installer into the
// top-level CLI command.  It owns flag parsing and program flow but delegates
// every meaningful operation to the purpose-built sub-packages, keeping this
// file thin and easy to read end-to-end.
package cmd

import (
	"flag"
	"fmt"
	"os"

	"github.com/kaleb110/deps"
	"github.com/kaleb110/installer"
	"github.com/kaleb110/scanner"
	"github.com/kaleb110/tui"
)

// Run is the program entry point.  It accepts the raw CLI arguments and a
// concrete Installer so that tests can inject a mock without touching the
// filesystem or spawning processes.
func Run(args []string, inst installer.Installer) error {
	fs := flag.NewFlagSet("dep-syncer", flag.ContinueOnError)
	src := fs.String("src", ".", "Source directory to scan for JS/TS imports")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if _, err := os.Stat(*src); os.IsNotExist(err) {
		return fmt.Errorf("source directory %q does not exist", *src)
	}

	// ── Step 1: scan source files ────────────────────────────────────────────
	//
	// Walk *src and collect every third-party package name referenced in
	// import/require statements.  node_modules is always skipped.

	sc := scanner.New()
	found, err := sc.ScanDir(*src)
	if err != nil {
		return fmt.Errorf("scanning %q: %w", *src, err)
	}

	if len(found) == 0 {
		fmt.Fprintln(os.Stdout, "✅ No imports found in source files.")
		return nil
	}

	// ── Step 2: load declared dependencies ──────────────────────────────────
	//
	// package.json is read from the current working directory (the project
	// root), not from --src.  --src is a source subdirectory; package.json
	// lives one level above it.

	declared, err := deps.Load("package.json")
	if err != nil {
		return err
	}

	// ── Step 3: diff ─────────────────────────────────────────────────────────
	//
	// Anything imported but not listed in any of the four dependency maps is
	// a candidate for installation.

	missing := deps.Diff(found, declared)

	if len(missing) == 0 {
		fmt.Fprintln(os.Stdout, "✨ All imports are already in package.json.")
		return nil
	}

	// ── Step 4: let the user choose what to install ──────────────────────────

	fmt.Fprintf(os.Stdout, "Found %d import(s) not in package.json:\n", len(missing))
	for _, p := range missing {
		fmt.Fprintf(os.Stdout, "  • %s\n", p)
	}

	selected, err := tui.PromptUser(missing)
	if err != nil {
		return err
	}

	if len(selected) == 0 {
		fmt.Fprintln(os.Stdout, "Nothing selected.")
		return nil
	}

	// ── Step 5: install ──────────────────────────────────────────────────────

	return inst.Install(selected)
}