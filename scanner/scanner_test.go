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

// sc is a default scanner with no extra exclusions, used by most tests.
var sc = scanner.New(nil)

// ── ExtractImports ────────────────────────────────────────────────────────────

func TestExtractImports(t *testing.T) {
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

		// ── Node built-ins: modern "node:" prefix form ────────────────────────
		// The "node:" prefix is the canonical approach recommended by Node.js.
		// All of these must be ignored regardless of what follows the colon.
		{"node: fs", `import fs from "node:fs"`, []string{}},
		{"node: path", `import path from "node:path"`, []string{}},
		{"node: crypto", `import { randomBytes } from "node:crypto"`, []string{}},
		{"node: worker_threads", `import { Worker } from "node:worker_threads"`, []string{}},
		{"node: sub-path", `import { pipeline } from "node:stream/promises"`, []string{}},

		// ── Node built-ins: legacy bare form ─────────────────────────────────
		// Older code doesn't use the "node:" prefix — these must still be ignored.
		{"bare fs", `import fs from "fs"`, []string{}},
		{"bare path", `import path from "path"`, []string{}},
		{"bare crypto", `import crypto from "crypto"`, []string{}},

		// Path aliases
		{"@/ alias", `import Button from "@/components/Button"`, []string{}},
		{"~/ alias", `import styles from "~/assets/styles"`, []string{}},

		// Real scoped package must not be confused with @/ alias
		{"real scoped pkg", `import { Q } from "@tanstack/react-query"`, []string{"@tanstack/react-query"}},

		// Comments must be skipped entirely
		{"line comment", `// import { x } from "ghost-pkg"`, []string{}},
		{"jsdoc line", ` * import { x } from "ghost-pkg"`, []string{}},

		// Deduplication
		{"deduplication", strings.Join([]string{
			`import { useState } from "react"`,
			`import { useEffect } from "react"`,
		}, "\n"), []string{"react"}},

		// Multiple packages
		{"multiple packages", strings.Join([]string{
			`import React from "react"`,
			`import { Link } from "react-router-dom"`,
			`import axios from "axios"`,
		}, "\n"), []string{"axios", "react", "react-router-dom"}},

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

// ── --exclude custom prefixes ─────────────────────────────────────────────────

// TestExcludePrefixes verifies that the user-supplied exclusion prefixes are
// applied after the built-in rules, so project-specific aliases are filtered.
func TestExcludePrefixes(t *testing.T) {
	cases := []struct {
		name     string
		prefixes []string
		src      string
		want     []string
	}{
		{
			name:     "virtual: prefix (Vite virtual modules)",
			prefixes: []string{"virtual:"},
			src:      `import "virtual:pwa-register"`,
			want:     []string{},
		},
		{
			name:     "~icons/ prefix (unplugin-icons)",
			prefixes: []string{"~icons/"},
			src:      `import IconMenu from "~icons/mdi/menu"`,
			want:     []string{},
		},
		{
			name:     "multiple prefixes, only matching ones excluded",
			prefixes: []string{"virtual:", "~icons/"},
			src: strings.Join([]string{
				`import "virtual:pwa-register"`,
				`import IconMenu from "~icons/mdi/menu"`,
				`import axios from "axios"`,
			}, "\n"),
			want: []string{"axios"},
		},
		{
			name:     "non-matching prefix does not exclude real package",
			prefixes: []string{"virtual:"},
			src:      `import axios from "axios"`,
			want:     []string{"axios"},
		},
		{
			name:     "empty prefix string is ignored safely",
			prefixes: []string{"", "virtual:"},
			src:      `import "virtual:something"`,
			want:     []string{},
		},
		{
			name:     "nil prefixes behaves same as empty",
			prefixes: nil,
			src:      `import axios from "axios"`,
			want:     []string{"axios"},
		},
		{
			name:     "dollar sign alias (SvelteKit $lib)",
			prefixes: []string{"$"},
			src: strings.Join([]string{
				`import { db } from "$lib/db"`,
				`import axios from "axios"`,
			}, "\n"),
			want: []string{"axios"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := scanner.New(tc.prefixes)
			got := sorted(s.ExtractImports(strings.NewReader(tc.src)))
			want := sorted(tc.want)
			if fmt.Sprint(got) != fmt.Sprint(want) {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// ── ScanDir ───────────────────────────────────────────────────────────────────

func TestScanDir_SkipsNodeModules(t *testing.T) {
	dir := tmpDir(t)
	nm := filepath.Join(dir, "node_modules", "some-pkg")
	os.MkdirAll(nm, 0755)
	writeFile(t, nm, "index.ts", `import "should-be-ignored"`)
	writeFile(t, dir, "app.ts", `import "real-pkg"`)

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

func TestScanDir_Recurses(t *testing.T) {
	dir := tmpDir(t)
	sub := filepath.Join(dir, "src", "components")
	os.MkdirAll(sub, 0755)
	writeFile(t, sub, "Button.tsx", `import "deep-pkg"`)

	found, err := sc.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found["deep-pkg"] {
		t.Error("deep-pkg not found in subdirectory")
	}
}

func TestScanDir_OnlySupportedExtensions(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "style.css", `import "css-pkg"`)
	writeFile(t, dir, "README.md", `import "md-pkg"`)
	writeFile(t, dir, "data.json", `{"import":"json-pkg"}`)
	writeFile(t, dir, "main.ts", `import "ts-pkg"`)

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
		t.Error("ts-pkg not found")
	}
}

func TestScanDir_ExcludePrefixAppliedDuringWalk(t *testing.T) {
	dir := tmpDir(t)
	writeFile(t, dir, "app.ts", strings.Join([]string{
		`import "virtual:pwa-register"`,
		`import axios from "axios"`,
	}, "\n"))

	s := scanner.New([]string{"virtual:"})
	found, err := s.ScanDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if found["virtual:pwa-register"] {
		t.Error("virtual: import should have been excluded")
	}
	if !found["axios"] {
		t.Error("axios should still be found")
	}
}

func TestScanDir_EmptyDirectory(t *testing.T) {
	found, err := sc.ScanDir(tmpDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Errorf("expected empty, got %v", found)
	}
}

func TestScanDir_NonExistentDirectory(t *testing.T) {
	_, err := sc.ScanDir("/does/not/exist")
	if err == nil {
		t.Error("expected error for non-existent directory")
	}
}