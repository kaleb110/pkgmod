// Package installer defines the Installer interface, the Manager enum of
// supported package managers, and a factory function that maps an enum value
// to its concrete implementation.
//
// # Adding a new package manager
//
// 1. Add a constant to the Manager enum and its string name to managerNames.
// 2. Implement the Installer interface in a new file (e.g. yarn.go).
// 3. Add a case for it in New().
//
// Nothing outside this package needs to change — cmd.Run accepts any Installer
// and main.go delegates construction entirely to New().
package installer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Manager is an enum of the package managers pkgmod supports.
// Using a named type instead of a raw string means the compiler rejects
// unknown values at call sites and switch statements are exhaustiveness-checked
// by linters such as exhaustive.
type Manager int

const (
	// ManagerPnpm uses `pnpm add`.
	ManagerPnpm Manager = iota
	// ManagerBun uses `bun add`.
	ManagerBun
	// ManagerNpm uses `npm install`.
	ManagerNpm
	// ManagerYarn uses `yarn add`.
	ManagerYarn

	// managerSentinel is not a real manager; it marks the end of the enum so
	// that New() can detect out-of-range values without a separate list.
	managerSentinel
)

// managerNames maps each Manager constant to the CLI flag string the user
// passes with --manager.  It is the single source of truth for valid names.
var managerNames = map[string]Manager{
	"pnpm": ManagerPnpm,
	"bun":  ManagerBun,
	"npm":  ManagerNpm,
	"yarn": ManagerYarn,
}

// managerStrings is the reverse of managerNames: enum → canonical string.
// Used by Manager.String() so fmt and logs display human-readable names.
var managerStrings = func() map[Manager]string {
	rev := make(map[Manager]string, len(managerNames))
	for name, m := range managerNames {
		rev[m] = name
	}
	return rev
}()

// String returns the canonical lowercase name of the manager (e.g. "pnpm").
// It satisfies fmt.Stringer so Manager prints readably in logs and errors.
func (m Manager) String() string {
	if s, ok := managerStrings[m]; ok {
		return s
	}
	return fmt.Sprintf("Manager(%d)", int(m))
}

// ParseManager converts a user-supplied string (e.g. from the --manager flag)
// into a Manager enum value, returning a descriptive error for unknown names.
//
// This is where unsupported managers are rejected early — before any scanning
// or file I/O takes place — so the user gets immediate feedback.
func ParseManager(s string) (Manager, error) {
	m, ok := managerNames[strings.ToLower(strings.TrimSpace(s))]
	if !ok {
		supported := make([]string, 0, len(managerNames))
		for name := range managerNames {
			supported = append(supported, name)
		}
		return 0, fmt.Errorf(
			"unsupported package manager %q (supported: %s)",
			s, strings.Join(supported, ", "),
		)
	}
	return m, nil
}

// Installer is the single method every package manager implementation must
// satisfy.  Keeping the interface narrow means mocks stay trivial and new
// implementations stay focused.
type Installer interface {
	// Install adds the given packages to the project.  The implementation
	// is responsible for printing progress to the user.
	Install(pkgs []string) error
}

// New returns the Installer for the given Manager.  It returns an error only
// if m is out of range, which cannot happen when m comes from ParseManager.
func New(m Manager) (Installer, error) {
	switch m {
	case ManagerPnpm:
		return &Pnpm{}, nil
	case ManagerBun:
		return &Bun{}, nil
	case ManagerNpm:
		return &Npm{}, nil
	case ManagerYarn:
		return &Yarn{}, nil
	default:
		return nil, fmt.Errorf("no installer registered for manager %d", m)
	}
}

// ── Implementations ───────────────────────────────────────────────────────────

// Pnpm installs packages using `pnpm add`.
type Pnpm struct{}

// Install runs `pnpm add <pkgs...>` and streams output to the terminal.
func (p *Pnpm) Install(pkgs []string) error {
	return run("pnpm", "add", pkgs)
}

// Bun installs packages using `bun add`.
type Bun struct{}

// Install runs `bun add <pkgs...>` and streams output to the terminal.
func (b *Bun) Install(pkgs []string) error {
	return run("bun", "add", pkgs)
}

// Npm installs packages using `npm install`.
type Npm struct{}

// Install runs `npm install <pkgs...>` and streams output to the terminal.
func (n *Npm) Install(pkgs []string) error {
	return run("npm", "install", pkgs)
}

// Yarn installs packages using `yarn add`.
type Yarn struct{}

// Install runs `yarn add <pkgs...>` and streams output to the terminal.
func (y *Yarn) Install(pkgs []string) error {
	return run("yarn", "add", pkgs)
}

// run is the shared helper that executes <binary> <subcommand> <pkgs...> and
// streams stdout/stderr directly to the terminal so the user sees live output.
func run(binary, subcommand string, pkgs []string) error {
	args := append([]string{subcommand}, pkgs...)
	fmt.Printf("Running: %s %s\n", binary, strings.Join(args, " "))
	cmd := exec.Command(binary, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
