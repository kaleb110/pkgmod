package scanner_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/kaleb110/pkgmod/scanner"
)

func sorted(s []string) []string {
	out := make([]string, len(s))
	copy(out, s)
	sort.Strings(out)
	return out
}

func tmpDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("", "depsyncer-scanner-*")
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

// ── ExtractImports ────────────────────────────────────────────────────────────

// TestExtractImports covers every supported import syntax as well as the cases
// that must be silently ignored (relative paths, built-ins, comments).
func TestExtractImports(t *testing.T) {
	sc := scanner.New()

	cases := []struct {
		name string
		src  string
		want []string
	}{
		// Static import variants
		{"default import", `import React from "react"`, []string{"react"}},
		{"named import", `import { useState } from 'react'`, []string{"react"}},
		{"namespace import", `import * as _ from "lodash"`, []string{"lodash"}},
		{"side-effect import", `import "reflect-metadata"`, []string{"reflect-metadata"}},

		// CommonJS and dynamic
		{"require call", `const x = require("axios")`, []string{"axios"}},
		{"dynamic import", `const m = await import("some-pkg")`, []string{"some-pkg"}},

		// Re-exports
		{"named re-export", `export { foo } from "foo-lib"`, []string{"foo-lib"}},
		{"star re-export", `export * from "@utils/core"`, []string{"@utils/core"}},

		// Sub-paths: only the root package name should be returned
		{"sub-path import", `import { format } from "date-fns/format"`, []string{"date-fns"}},
		{"scoped sub-path", `import { Q } from "@tanstack/react-query/core"`, []string{"@tanstack/react-query"}},

		// Ignored specifiers
		{"relative ignored", `import { x } from "./local"`, []string{}},
		{"parent relative ignored", `import { x } from "../lib"`, []string{}},
		{"node builtin ignored", `import fs from "fs"`, []string{}},

		// Path aliases — configured in tsconfig/vite, never installable
		{"@/ alias ignored", `import Button from "@/components/Button"`, []string{}},
		{"@/types alias ignored", `import type { Foo } from "@/types"`, []string{}},
		{"@/utils alias ignored", `import { helper } from "@/utils/format"`, []string{}},
		{"~/ alias ignored", `import styles from "~/assets/styles"`, []string{}},
		{"real scoped pkg not confused with alias", `import { Q } from "@tanstack/react-query"`, []string{"@tanstack/react-query"}},

		// Comments must be skipped entirely
		{"line comment skipped", `// import { x } from "ghost-pkg"`, []string{}},
		{"jsdoc line skipped", ` * import { x } from "ghost-pkg"`, []string{}},

		// Deduplication: same package imported twice in one file → one entry
		{"deduplication", strings.Join([]string{
			`import { useState } from "react"`,
			`import { useEffect } from "react"`,
		}, "\n"), []string{"react"}},

		// Multiple distinct packages in one file
		{"multiple packages", strings.Join([]string{
			`import React from "react"`,
			`import { Link } from "react-router-dom"`,
			`import axios from "axios"`,
		}, "\n"), []string{"axios", "react", "react-router-dom"}},

		// Quote style should not matter
		{"single-quoted specifier", `import a from 'pkg-a'`, []string{"pkg-a"}},

		// Edge cases
		{"empty file", "", []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sorted(sc.ExtractImports(strings.NewReader(tc.src)))
			want := sorted(tc.want)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// ── ScanDir ───────────────────────────────────────────────────────────────────

// TestScanDir_SkipsNodeModules ensures vendored code is never scanned.
func TestScanDir_SkipsNodeModules(t *testing.T) {
	dir := tmpDir(t)
	nm := filepath.Join(dir, "node_modules", "some-pkg")
	os.MkdirAll(nm, 0755)
	writeFile(t, nm, "index.ts", `import "should-be-ignored"`)
	writeFile(t, dir, "app.ts", `import "real-pkg"`)

	sc := scanner.New()
	found, err := sc.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if found["should-be-ignored"] {
		t.Error("scanned node_modules — should be skipped")
	}
	if !found["real-pkg"] {
		t.Error("real-pkg not found")
	}
}

// TestScanDir_Recurses verifies that packages in deeply nested subdirectories
// are discovered.
func TestScanDir_Recurses(t *testing.T) {
	dir := tmpDir(t)
	sub := filepath.Join(dir, "src", "components")
	os.MkdirAll(sub, 0755)
	writeFile(t, sub, "Button.tsx", `import "deep-pkg"`)

	sc := scanner.New()
	found, err := sc.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found["deep-pkg"] {
		t.Error("deep-pkg not found in subdirectory")
	}
}

// TestScanDir_OnlySupportedExtensions confirms that CSS, Markdown, JSON, and
// other non-JS/TS files are not scanned.
func TestScanDir_OnlySupportedExtensions(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "style.css", `import "css-pkg"`)
	writeFile(t, dir, "README.md", `import "md-pkg"`)
	writeFile(t, dir, "data.json", `{"import":"json-pkg"}`)
	writeFile(t, dir, "main.ts", `import "ts-pkg"`)

	sc := scanner.New()
	found, err := sc.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"css-pkg", "md-pkg", "json-pkg"} {
		if found[bad] {
			t.Errorf("%s should not be scanned from a non-JS/TS file", bad)
		}
	}
	if !found["ts-pkg"] {
		t.Error("ts-pkg should have been found in main.ts")
	}
}

// TestScanDir_EmptyDirectory verifies that scanning an empty directory returns
// an empty result without error.
func TestScanDir_EmptyDirectory(t *testing.T) {
	sc := scanner.New()
	found, err := sc.ScanDir(tmpDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("expected empty result, got %v", found)
	}
}

// TestScanDir_NonExistentDirectory verifies that a missing directory produces
// an error rather than a silent empty result.
func TestScanDir_NonExistentDirectory(t *testing.T) {
	sc := scanner.New()
	_, err := sc.ScanDir("/does/not/exist")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}
