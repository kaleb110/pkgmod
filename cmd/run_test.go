package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaleb110/cmd"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func tmpDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "depsyncer-cmd-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(d) })
	return d
}

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}

// chdir changes the working directory to dir for the duration of the test.
// cmd.Run reads package.json relative to cwd.
func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(orig) })
}

// ── package.json existence ────────────────────────────────────────────────────

// TestRun_MissingPackageJSON verifies that running without a package.json in
// cwd produces a clear error before any scanning occurs.
func TestRun_MissingPackageJSON(t *testing.T) {
	dir := tmpDir(t) // intentionally no package.json
	writeFile(t, dir, "app.ts", `import "react"`)
	chdir(t, dir)

	err := cmd.Run([]string{"--src", ".", "--manager", "pnpm"})
	if err == nil {
		t.Fatal("expected error for missing package.json, got nil")
	}
	if !strings.Contains(err.Error(), "package.json") {
		t.Errorf("error should mention package.json, got: %v", err)
	}
}

// ── manager resolution ────────────────────────────────────────────────────────

// TestRun_ManagerFromPackageJSONField verifies that the packageManager field
// takes priority over the --manager flag.  We can't run a real installer in
// tests, so we verify by passing an unsupported --manager flag that would
// normally error — the field should win and suppress that error.
//
// We verify indirectly: if the field is "pnpm@9" and --manager is "cargo"
// (unsupported), resolution should succeed (pnpm wins) and only fail later
// when pnpm is not found — not with "unsupported manager cargo".
func TestRun_ManagerFromPackageJSONField(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"packageManager": "pnpm@9.1.0",
		"dependencies":   {"react": "^18"}
	}`)
	writeFile(t, dir, "app.ts", `import "react"`)
	chdir(t, dir)

	err := cmd.Run([]string{"--src", ".", "--manager", "cargo"})
	// Two acceptable outcomes:
	//  a) nil  — all deps already declared, exits before install
	//  b) non-nil but NOT "unsupported manager cargo" — pnpm was chosen,
	//     then either pnpm was not found on PATH or something else failed
	if err != nil && strings.Contains(err.Error(), "unsupported package manager \"cargo\"") {
		t.Errorf("--manager flag should have been ignored because packageManager field is set; got: %v", err)
	}
}

// TestRun_UnsupportedManagerInField verifies that an unrecognised value in the
// packageManager field (e.g. "yarn-berry@4.0") is rejected immediately with a
// clear error, rather than silently falling back to the --manager flag.
func TestRun_UnsupportedManagerInField(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"packageManager":"yarn-berry@4.0.0"}`)
	writeFile(t, dir, "app.ts", `import "react"`)
	chdir(t, dir)

	err := cmd.Run([]string{"--src", ".", "--manager", "pnpm"})
	if err == nil {
		t.Fatal("expected error for unsupported packageManager field, got nil")
	}
	if !strings.Contains(err.Error(), "yarn-berry") {
		t.Errorf("error should mention the bad manager name, got: %v", err)
	}
}

// TestRun_UnsupportedManagerFlag verifies that an unknown --manager flag is
// rejected when the packageManager field is absent.
func TestRun_UnsupportedManagerFlag(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18"}}`)
	writeFile(t, dir, "app.ts", `import "react"`)
	chdir(t, dir)

	err := cmd.Run([]string{"--src", ".", "--manager", "cargo"})
	if err == nil {
		t.Fatal("expected error for unknown --manager flag, got nil")
	}
}

// TestRun_FlagUsedWhenFieldAbsent verifies that --manager is respected when
// packageManager is absent from package.json.  We use a valid manager that
// may not be installed; we only care that resolution succeeded (error is not
// "unsupported manager").
func TestRun_FlagUsedWhenFieldAbsent(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18"}}`)
	writeFile(t, dir, "app.ts", `import "react"`)
	chdir(t, dir)

	err := cmd.Run([]string{"--src", ".", "--manager", "bun"})
	// Acceptable: nil (all deps present) or a real exec error (bun not on PATH).
	// Not acceptable: "unsupported package manager".
	if err != nil && strings.Contains(err.Error(), "unsupported package manager") {
		t.Errorf("bun should be accepted as a valid manager; got: %v", err)
	}
}

// ── existing behaviour ────────────────────────────────────────────────────────

// TestRun_AllDepsPresent verifies that the installer is never invoked when
// every import is already in package.json.
func TestRun_AllDepsPresent(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"packageManager": "pnpm@9.1.0",
		"dependencies":   {"react": "^18"}
	}`)
	writeFile(t, dir, "app.ts", `import React from "react"`)
	chdir(t, dir)

	if err := cmd.Run([]string{"--src", "."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRun_DevDepNotReinstalled verifies packages in devDependencies are not
// treated as missing.
func TestRun_DevDepNotReinstalled(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{
		"packageManager":  "pnpm@9.1.0",
		"devDependencies": {"typescript": "^5"}
	}`)
	writeFile(t, dir, "app.ts", `import ts from "typescript"`)
	chdir(t, dir)

	if err := cmd.Run([]string{"--src", "."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRun_NoSourceFiles verifies early exit when the source directory has no
// JS/TS files.
func TestRun_NoSourceFiles(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"packageManager":"pnpm@9.1.0"}`)
	chdir(t, dir)

	if err := cmd.Run([]string{"--src", "."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRun_NonExistentSrcDir verifies that a missing --src directory is an error.
func TestRun_NonExistentSrcDir(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"packageManager":"pnpm@9.1.0"}`)
	chdir(t, dir)

	if err := cmd.Run([]string{"--src", "/no/such/dir"}); err == nil {
		t.Error("expected error for missing src dir")
	}
}

// TestRun_BuiltinsNotInstalled verifies Node.js built-ins are never installed.
func TestRun_BuiltinsNotInstalled(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"packageManager":"pnpm@9.1.0"}`)
	writeFile(t, dir, "app.ts", strings.Join([]string{
		`import fs from "fs"`,
		`import path from "path"`,
		`import crypto from "crypto"`,
	}, "\n"))
	chdir(t, dir)

	// All imports are built-ins — should report "no imports found" and exit cleanly.
	if err := cmd.Run([]string{"--src", "."}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRun_PkgJSONReadFromCwd is the regression test for the original bug:
// --src=src should find package.json in cwd (project root), not inside src/.
func TestRun_PkgJSONReadFromCwd(t *testing.T) {
	root := tmpDir(t)
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "package.json", `{
		"packageManager": "pnpm@9.1.0",
		"dependencies":   {"zustand": "^5.0.13"}
	}`)
	writeFile(t, srcDir, "store.ts", `import { create } from "zustand"`)
	chdir(t, root)

	if err := cmd.Run([]string{"--src", "src"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}