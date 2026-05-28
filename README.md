## pkgmod

a golang cli app to auto download uninstalled dependancies for typescript project.
made with `go` standard library with `bubbletea` as a cli library.

## install

```bash
go install github.com/kaleb110/pkgmod@latest
```

## usage

```bash
$ pkgmod # runs on the current dir
$ pkgmod -src=src # runs on src dir
$ pkgmod -src=src -manager=pnpm # manager flag sets your package manager
```

## supported managers

- npm, pnpm, bun, yarn
- add `packageManager` to your `package.json` to enable auto detection 

## features

- auto download uninstalled packages
- support for pupular package managers
- ESM + CJS support
- sync with package.json