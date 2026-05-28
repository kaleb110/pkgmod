// Package cmd wires together the scanner, deps loader, installer, and TUI into
// the top-level CLI command.  It owns flag parsing and program flow but
// delegates every meaningful operation to the purpose-built sub-packages.
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

// Run is the program entry point.  It accepts the raw CLI arguments so that
// tests can drive it without spawning a subprocess.
func Run(args []string) error {
	fs := flag.NewFlagSet("dep-syncer", flag.ContinueOnError)
	src     := fs.String("src",     ".", "Source directory to scan for JS/TS imports")
	// Default is empty string, not "pnpm" — an absent flag means "read from
	// package.json" and we distinguish that from an explicit flag value.
	manager := fs.String("manager", "", "Package manager override (pnpm, bun, npm, yarn)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// ── Step 1: verify package.json exists ───────────────────────────────────
	//
	// A missing package.json is a hard error — there is nowhere to record new
	// dependencies and no baseline to diff against.  We check this first so
	// the user gets a clear message before any scanning happens.

	const pkgJSONPath = "package.json"
	if _, err := os.Stat(pkgJSONPath); os.IsNotExist(err) {
		return fmt.Errorf("package.json not found in the current directory — run dep-syncer from your project root")
	}

	// ── Step 2: load package.json ─────────────────────────────────────────────
	//
	// LoadFile returns both the declared dependency set and the raw
	// packageManager field in a single read so we don't open the file twice.

	pkgFile, err := deps.LoadFile(pkgJSONPath)
	if err != nil {
		return err
	}

	// ── Step 3: resolve which package manager to use ─────────────────────────
	//
	// Priority:
	//   1. packageManager field in package.json  (e.g. "pnpm@9.1.0")
	//   2. --manager flag (must be provided when field is absent)
	//
	// If the field names a manager we don't support, that is an error — we
	// don't silently fall back to the flag, because that could install packages
	// with the wrong manager and corrupt the lockfile.
	//
	// If neither is set, we return a clear error asking the user to specify.

	if pkgFile.PackageManager == "" && *manager == "" {
		return fmt.Errorf(
			"cannot determine package manager: packageManager field is absent from package.json\n" +
			"specify one with --manager (supported: pnpm, bun, npm, yarn)",
		)
	}

	managerName, err := deps.ResolveManager(
		pkgFile.PackageManager,
		*manager,
		func(s string) (string, error) {
			m, parseErr := installer.ParseManager(s)
			if parseErr != nil {
				return "", parseErr
			}
			return m.String(), nil
		},
	)
	if err != nil {
		return err
	}

	m, err := installer.ParseManager(managerName)
	if err != nil {
		return err
	}
	inst, err := installer.New(m)
	if err != nil {
		return err
	}

	// ── Step 4: validate --src ────────────────────────────────────────────────

	if _, err := os.Stat(*src); os.IsNotExist(err) {
		return fmt.Errorf("source directory %q does not exist", *src)
	}

	// ── Step 5: scan source files ─────────────────────────────────────────────

	sc := scanner.New()
	found, err := sc.ScanDir(*src)
	if err != nil {
		return fmt.Errorf("scanning %q: %w", *src, err)
	}

	if len(found) == 0 {
		fmt.Fprintln(os.Stdout, "✅ No imports found in source files.")
		return nil
	}

	// ── Step 6: diff ──────────────────────────────────────────────────────────

	missing := deps.Diff(found, pkgFile.Declared)

	if len(missing) == 0 {
		fmt.Fprintf(os.Stdout, "✨ All %d import(s) are already in package.json.\n", len(found))
		return nil
	}

	// ── Step 7: prompt ────────────────────────────────────────────────────────

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

	// ── Step 8: install ───────────────────────────────────────────────────────

	return inst.Install(selected)
}