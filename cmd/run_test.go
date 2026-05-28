package cmd_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaleb110/cmd"
)

// mockInstaller records Install calls without running any real commands.
// It lives here because it is only needed to test cmd.Run end-to-end.
type mockInstaller struct {
	got []string
	err error // if non-nil, Install returns this error
}

func (m *mockInstaller) Install(pkgs []string) error {
	m.got = append(m.got, pkgs...)
	return m.err
}

// ── helpers ──────────────────────────────────────────────────────────────────

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
// cmd.Run reads package.json relative to cwd, so tests must chdir into the
// temp project root before calling Run.
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

// ── tests ─────────────────────────────────────────────────────────────────────

// TestRun_AllDepsPresent verifies that the installer is never called when every
// imported package is already listed in package.json.
func TestRun_AllDepsPresent(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"dependencies":{"react":"^18"}}`)
	writeFile(t, dir, "app.ts", `import React from "react"`)
	chdir(t, dir)

	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "."}, mi); err != nil {
		t.Fatal(err)
	}
	if len(mi.got) != 0 {
		t.Errorf("expected no installs, got %v", mi.got)
	}
}

// TestRun_DevDepNotReinstalled ensures packages already in devDependencies are
// not treated as missing, even though they are not in dependencies.
func TestRun_DevDepNotReinstalled(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{"devDependencies":{"typescript":"^5"}}`)
	writeFile(t, dir, "app.ts", `import ts from "typescript"`)
	chdir(t, dir)

	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "."}, mi); err != nil {
		t.Fatal(err)
	}
	if len(mi.got) != 0 {
		t.Errorf("typescript is a devDep — should not reinstall; got %v", mi.got)
	}
}

// TestRun_NoSourceFiles verifies early exit when the source directory contains
// no JS/TS files (and therefore no imports to compare against).
func TestRun_NoSourceFiles(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{}`)
	chdir(t, dir)

	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "."}, mi); err != nil {
		t.Fatal(err)
	}
	if len(mi.got) != 0 {
		t.Errorf("expected no installs, got %v", mi.got)
	}
}

// TestRun_NonExistentSrcDir verifies that a missing --src directory is
// reported as an error rather than silently succeeding.
func TestRun_NonExistentSrcDir(t *testing.T) {
	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "/no/such/dir"}, mi); err == nil {
		t.Error("expected error for missing src dir")
	}
}

// TestRun_MalformedPackageJSON verifies that a JSON parse error in package.json
// is surfaced to the caller rather than swallowed.
func TestRun_MalformedPackageJSON(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{not valid json`)
	writeFile(t, dir, "app.ts", `import "some-pkg"`)
	chdir(t, dir)

	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "."}, mi); err == nil {
		t.Error("expected error for malformed package.json")
	}
}

// TestRun_BuiltinsNotInstalled verifies that Node.js built-in modules (fs,
// path, crypto, …) are never forwarded to the installer.
func TestRun_BuiltinsNotInstalled(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "package.json", `{}`)
	writeFile(t, dir, "app.ts", strings.Join([]string{
		`import fs from "fs"`,
		`import path from "path"`,
		`import crypto from "crypto"`,
	}, "\n"))
	chdir(t, dir)

	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "."}, mi); err != nil {
		t.Fatal(err)
	}
	if len(mi.got) != 0 {
		t.Errorf("built-ins should not be installed, got %v", mi.got)
	}
}

// TestRun_PkgJSONReadFromCwd is the regression test for the original bug:
// running with --src=src should find package.json at the project root (cwd),
// not inside the src/ subdirectory.
func TestRun_PkgJSONReadFromCwd(t *testing.T) {
	root := tmpDir(t)
	srcDir := filepath.Join(root, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	// package.json is at the project root, source file is inside src/.
	writeFile(t, root, "package.json", `{"dependencies":{"zustand":"^5.0.13"}}`)
	writeFile(t, srcDir, "store.ts", `import { create } from "zustand"`)
	chdir(t, root)

	mi := &mockInstaller{}
	if err := cmd.Run([]string{"--src", "src"}, mi); err != nil {
		t.Fatal(err)
	}
	if len(mi.got) != 0 {
		t.Errorf("zustand is declared — nothing should install; got %v", mi.got)
	}
}