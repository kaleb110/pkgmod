# pkgmod — Architecture & Maintenance Roadmap

---

## 1. Current Architecture

### Package map and dependency graph

```
main.go
  └── cmd.Run(args)
        ├── deps.LoadFile()        reads package.json
        ├── deps.ResolveManager()  picks pnpm / bun / npm / yarn
        ├── installer.ParseManager()
        ├── installer.New()        constructs the concrete installer
        ├── scanner.New(prefixes).ScanDir()   walks source files
        ├── deps.Diff()            pure set subtraction
        └── tui.PromptUser()       interactive picker
              └── installer.Install()
```

Dependency arrows (who imports whom):

```
cmd  →  deps, installer, scanner, tui
tui  →  (bubbletea, lipgloss only — no internal packages)
deps →  (stdlib only)
scanner → (stdlib only)
installer → (stdlib only)
main → cmd
```

**The key invariant:** `deps`, `scanner`, `installer`, and `tui` never import
each other. Only `cmd` knows about all of them. Breaking this rule creates
circular imports and tight coupling that makes individual packages untestable
in isolation.

### What each package owns and what it must never do

| Package | Owns | Must never |
|---|---|---|
| `main` | Wire `cmd.Run`, call `os.Exit` | Contain logic |
| `cmd` | Flag parsing, orchestration, program flow | Touch the filesystem directly beyond `os.Stat` |
| `scanner` | Walk dirs, extract import strings, apply exclusions | Know about package.json or installers |
| `deps` | Parse package.json, diff sets, resolve manager | Spawn processes or render UI |
| `installer` | Enum, factory, spawn package manager processes | Know about source files or package.json layout |
| `tui` | Render interactive picker, return user selection | Know about packages, managers, or files |

---

## 2. How to Add New Features

### A. New package manager (e.g. Yarn Berry, Bun workspaces)

**Only touch: `installer/installer.go`**

1. Add a constant to the enum:
   ```go
   const (
       ManagerPnpm Manager = iota
       ManagerBun
       ManagerNpm
       ManagerYarn
       ManagerYarnBerry   // ← add here
       managerSentinel
   )
   ```

2. Add its name to `managerNames`:
   ```go
   var managerNames = map[string]Manager{
       "pnpm":        ManagerPnpm,
       "bun":         ManagerBun,
       "npm":         ManagerNpm,
       "yarn":        ManagerYarn,
       "yarn-berry":  ManagerYarnBerry,  // ← add here
   }
   ```

3. Implement the struct (new file `installer/yarn_berry.go`):
   ```go
   type YarnBerry struct{}

   func (y *YarnBerry) Install(pkgs []string) error {
       return run("yarn", "add", pkgs)
   }
   ```

4. Add a case to `New()`:
   ```go
   case ManagerYarnBerry:
       return &YarnBerry{}, nil
   ```

5. Add test cases to `installer/installer_test.go`:
   ```go
   {"yarn-berry", installer.ManagerYarnBerry},
   ```

Nothing else changes. `cmd`, `deps`, `scanner`, `tui`, and `main` are
untouched because they never name a concrete manager.

---

### B. New language or file type (e.g. Python, Go imports)

**Only touch: `scanner/scanner.go` and `scanner/scanner_test.go`**

The scanner is the only package that knows about file extensions and import
syntax. Add the extension to `supportedExts` and update `importRe` if the
import syntax differs.

For a language whose import syntax is incompatible with the current regex
(e.g. Python's `import x` or `from x import y`), the cleanest approach is
a `Parser` interface:

```go
// In scanner/scanner.go
type Parser interface {
    // Matches reports whether this parser handles the given file path.
    Matches(path string) bool
    // Extract returns third-party package names from reader content.
    Extract(r io.Reader) []string
}
```

Register parsers in `Scanner` and dispatch in `ScanDir`. Each language gets
its own file (`scanner/python.go`, `scanner/golang.go`) without touching the
others.

---

### C. New output format (e.g. `--json`, `--dry-run`)

**Only touch: `cmd/run.go`**

Add the flag, then branch after the `deps.Diff` call. The packages beneath
are unaffected — they return plain Go values (`[]string`, `map[string]bool`)
that you can serialize however you like.

```go
jsonOut := fs.Bool("json", false, "Print missing packages as JSON and exit")

// After deps.Diff():
if *jsonOut {
    enc := json.NewEncoder(os.Stdout)
    return enc.Encode(missing)
}
```

For `--dry-run`: skip `tui.PromptUser` and `inst.Install` entirely — print
what would be installed and return nil.

---

### D. Configuration file (e.g. `.depsyncer.yaml`)

**Add a new package: `config/config.go`**

Keep flag parsing in `cmd` but let a config file provide defaults. The load
order should be: config file → environment variables → CLI flags (flags win).

```go
// config/config.go
type Config struct {
    Src            string   `yaml:"src"`
    Manager        string   `yaml:"manager"`
    ExcludePrefixes []string `yaml:"exclude"`
}

func Load(path string) (*Config, error) { ... }
```

`cmd/run.go` calls `config.Load(".depsyncer.yaml")` before flag parsing and
uses config values as defaults for any flag not explicitly set.

Do not merge config loading into `deps` — `deps` is package.json only. A
separate `config` package keeps the two concerns independent.

---

### E. Monorepo / workspace support

**Only touch: `cmd/run.go`, potentially add `workspace/workspace.go`**

Workspace support means running the scan-diff-install loop for multiple
`package.json` files. The cleanest approach:

1. Add `--workspace` flag to `cmd`.
2. Extract a `workspace` package that discovers workspace roots (reads
   `pnpm-workspace.yaml`, `package.json#workspaces`, etc.) and returns a
   `[]string` of directories.
3. `cmd` iterates the list and calls `deps.LoadFile` + `deps.Diff` for each.
4. Present all missing packages in a single TUI session grouped by workspace,
   or per-workspace sessions — the `tui` package needs no changes because it
   only receives `[]string`.

---

## 3. Maintenance Rules

### The one-change-per-package rule

Every task should touch **at most one internal package** plus its test file.
If you find yourself editing `scanner.go` and `deps/loader.go` for the same
feature, the feature is in the wrong place or the package boundary is wrong.
Stop and redesign before writing code.

### Keep pure functions pure

`deps.Diff`, `deps.ResolveManager`, and `deps.normalizeKey` have no I/O.
Keep them that way. A pure function is tested with a struct literal and two
lines; a function that reads files needs temp dirs and cleanup. The ratio of
coverage to test complexity is far higher for pure functions.

If a future change requires `Diff` to consult a lockfile, create a new
function rather than adding I/O to the existing one.

### The `Installer` interface is a stability boundary

External code (CI scripts, editor plugins) may construct their own
`Installer` implementations. Treat the interface as public API — never add
a second method without a major version bump and a `//nolint:exhaustruct`
escape for existing implementations.

If you need to pass more context to installers (e.g. `--dev`, `--exact`),
add it to the struct, not to the interface:

```go
type Pnpm struct {
    Dev   bool
    Exact bool
}
```

### Error messages are user interface

Every error that surfaces to the user should answer three questions:
- What went wrong?
- Where did it go wrong? (file path, flag name)
- What should the user do about it?

```go
// Bad
return fmt.Errorf("parse error")

// Good
return fmt.Errorf("parsing %s: unexpected token at line 42 — check for trailing commas", path)
```

Wrap underlying errors with `%w` so callers can use `errors.Is` and
`errors.As`. Never swallow an error with `_ = err`.

### Test file placement and naming

| What | Where | Convention |
|---|---|---|
| Unit tests for a package | Same package, `_test.go` suffix | `package foo_test` (black-box) |
| Shared test helpers | `testutil/testutil.go` if used across packages | Never import `testutil` from non-test code |
| Integration tests | `cmd/run_test.go` using real temp dirs | `chdir` + `t.Cleanup` pattern |
| No real processes | `mockInstaller` in `cmd/run_test.go` | Never call `pnpm`/`bun` in tests |

Tests must never rely on the current working directory being anything
specific — always `chdir` explicitly and restore with `t.Cleanup`.

---

## 4. Planned Feature Backlog

Ordered by impact vs effort. Items at the top are safe to pick up without
risk to the existing architecture.

### Near term (low risk, high value)

**`--dev` flag — install as devDependency**
- Touch: `installer/installer.go` (add `Dev bool` field to each struct,
  pass `--save-dev` / `-D` to the subprocess)
- Touch: `cmd/run.go` (add `--dev` flag, set field before `inst.Install`)

**`--exact` flag — pin exact versions**
- Same pattern as `--dev`. Each manager has its own flag name (`--save-exact`
  for npm, `--exact` for pnpm).

**`--check` / CI mode — exit non-zero if anything is missing, no TUI**
- Touch: `cmd/run.go` only. After `deps.Diff`, if `--check` is set: print
  missing list and `return fmt.Errorf(...)`. Skip `tui.PromptUser` entirely.

**Shell completion (`pkgmod completion bash|zsh|fish`)**
- Add `completion` subcommand in `cmd`. The flag and manager enum values are
  the only completable tokens; source them from `installer.managerNames`.

### Medium term (moderate scope)

**`.depsyncer.yaml` config file**
- New `config` package. Does not change any existing package.
- Unblocks team-wide defaults (e.g. always exclude `virtual:` for Vite teams).

**`--update` mode — update existing deps to latest**
- `installer` gets an `Update(pkgs []string) error` method on the interface.
- `deps.Diff` gets a counterpart `Outdated(declared, latest map[string]string)`
  that compares version strings.
- `scanner` is untouched.

**Workspace / monorepo support**
- New `workspace` package for discovery.
- `cmd` iterates workspaces, calls the existing pipeline per workspace.

### Longer term (requires design work)

**Python support (`requirements.txt`, `pyproject.toml`)**
- Introduce the `Parser` interface in `scanner`.
- New `installer/pip.go`. Existing JS/TS parsers are unaffected.
- `deps` needs a `LoadRequirements(path)` alongside `LoadFile`.

**Lock file awareness — don't prompt for packages already in lockfile**
- New `lockfile` package that parses `pnpm-lock.yaml`, `yarn.lock`,
  `package-lock.json`, `bun.lockb`.
- `cmd` calls `lockfile.Load()` and passes the result to an extended
  `deps.Diff` that accepts a third set.

**Plugin system — let users register custom scanners**
- Loaded from `.depsyncer.yaml` as Go plugin `.so` files or as external
  binaries following a line-delimited stdout protocol.
- The `scanner.Parser` interface (from the language-support item above) is
  the natural extension point.

---

## 5. Git Workflow

```
main          ← always releasable; tagged with vX.Y.Z
  └── feat/bun-workspaces
  └── fix/manager-resolution
  └── chore/update-bubbletea
```

**Commit message convention:**

```
<package>: <what changed and why>

scanner: replace nodeBuiltins map with node: prefix check

The static allowlist required manual updates for every new Node version.
The "node:" prefix is the approach recommended by Node.js itself and
covers all future built-ins automatically.
```

Prefix with the package name that changed. If more than one package changed,
the commit is probably doing too much.

**Before merging any PR:**
- `go test ./...` passes
- `go vet ./...` is clean
- No new exported symbol lacks a doc comment
- Error messages follow the three-question rule above

---

## 6. Versioning Policy

Follow semver strictly because the `Installer` interface and `scanner.New`
signature are consumed by tests and potentially by external code.

| Change | Version bump |
|---|---|
| New flag, new manager, new exclusion rule | Patch (0.0.X) |
| New method on `Installer` interface | Major (X.0.0) |
| Changed signature of `scanner.New` | Major (X.0.0) |
| New package added (`config`, `workspace`) | Minor (0.X.0) |
| Breaking change to `deps.Diff` or `deps.ResolveManager` | Major (X.0.0) |

The `Installer` interface and the two exported functions in `deps`
(`LoadFile`, `Diff`, `ResolveManager`) are the public API surface.
Everything else is internal and can change freely between minor versions.