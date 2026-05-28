// Package installer defines the Installer interface and provides concrete
// implementations for each supported package manager.
//
// # Adding a new package manager
//
// Create a new file in this package (e.g. npm.go) and implement the Installer
// interface:
//
//	type Npm struct{}
//
//	func (n *Npm) Install(pkgs []string) error {
//	    cmd := exec.Command("npm", append([]string{"install"}, pkgs...)...)
//	    cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
//	    return cmd.Run()
//	}
//
// Then wire it in main.go behind a --manager flag.  No other files need to
// change — cmd.Run accepts any Installer.
package installer

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Installer is the single method that every package manager implementation
// must satisfy.  Accepting an interface in cmd.Run means tests can inject a
// mockInstaller without spawning real processes.
type Installer interface {
	// Install adds the given packages to the project.  The implementation is
	// responsible for printing progress to the user.
	Install(pkgs []string) error
}

// Pnpm installs packages using the pnpm package manager.
// It delegates directly to the pnpm binary found on PATH.
type Pnpm struct{}

// Install runs `pnpm add <pkgs...>` and streams stdout/stderr to the terminal.
func (p *Pnpm) Install(pkgs []string) error {
	fmt.Printf("Running: pnpm add %s\n", strings.Join(pkgs, " "))
	cmd := exec.Command("pnpm", append([]string{"add"}, pkgs...)...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}