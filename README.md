# prv

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="assets/prv-lockup-dark.svg">
    <img src="assets/prv-lockup.svg" alt="prv" width="320">
  </picture>
</p>

The live state of everything in a folder - git or not.

## Key features

- **Git or not.** Every project is shown - git repos and never-`git init`'d folders alike.
- **Read-only & stateless.** No registry, no cache, no config file - inspects real disk every run.
- **Ecosystem tags.** Manifests (`go.mod`, `package.json`, `pyproject.toml`, …) become
  readable `go` / `node` / `python` tags.
- **Single static binary.** One file, no runtime dependencies, cross-platform (Windows, macOS, Linux).
- **Git features.** Branch, dirty/clean state, and ahead/behind counts - from local tracking
  refs unless you opt in to `git fetch`.
- **TODO features.** Detects `TODO` / `TODO.md` / `TODO.txt` and shows open items.

## Overview

`prv` answers one question: **what's the state of everything in this folder right now?**
Run it and get a table of every project with its git branch and dirty/behind state, open
todos, detected stack, and when it was last touched.

It's built for the folder of many projects - `~/work`, `~/personal`, a monorepo boundary -
where you want a live inventory rather than a deep dive into a single repo. Because it's a
*projects directory* viewer (not a git-only multi-repo scanner), non-git projects are
first-class citizens: any directory with a signal (`.git`, a manifest, a TODO file, or an
empty `.prv` marker) becomes a row, and signal-free folders that merely *contain* projects
are expanded into their children.

The whole thing is one static Go binary. It launches instantly, reads real disk fresh
every scan, and never tells you anything it cached.

## Usage

Run the full-screen TUI on the current directory:

```bash
prv
```

Point it at somewhere specific, or get a plain table instead:

```bash
prv ~/work          # TUI on ~/work
prv ls              # fixed-width table on . to stdout
prv ls .. --depth=3 # table, shallower scan
```

Example table output:

```text
my-app       node   ●2   main    ↑2   3    2d ago
legacy-svc   python ✓    dev         ✓    21d ago
proto        go     ●1   feat/x  ↓1   -    5h ago
```

### TUI keys

| Key | Action |
| --- | ------ |
| `j` / `k` (or arrows) | Navigate rows |
| `/` | Filter by name, tags, or git state |
| `s` | Cycle sort (name → activity → open tasks → tags → dirty-first) |
| `r` | Re-scan (manual refresh) |
| `f` | Fetch tracking refs across repos (network) |
| `?` | Help overlay |
| `q` | Quit |

Behind the scenes, `prv` reads git state locally first for speed; value like `↑2` comes
from local tracking refs unless you `--fetch` or press `f`. Stale-but-local values are
visually distinguished (e.g. a `?` suffix) so old data never masquerades as fresh.

### Flags

| Flag | Description |
| ---- | ----------- |
| `--depth=<n>` | Root-relative classification + manifest/TODO search depth (default `4`) |
| `--fetch` | Fetch tracking refs before rendering (network) |
| `--refresh=<duration>` | TUI auto-rescan cadence, e.g. `--refresh=30s` (off by default) |
| `--sort=<field>` | Sort field: `name` \| `activity` \| `todo` \| `tags` \| `git` |
| `--version` | Print the version and exit |

## Installation

`prv` is a single static Go binary - no runtime dependencies.

**Requirements:**

- Go **1.26** or newer. (build-time only - the installed binary has no Go runtime dependency)
- git available on $PATH (used as a subprocess for all git state)

If Go is installed, the one-liner is:

```bash
go install github.com/mo-100/prv/cmd/prv@latest
```

Or build from a checkout:

```bash
go build -o out/prv ./cmd/prv
```

After installing, verify the binary is on your `$PATH` and works:

```bash
prv --version
```

> **`go install` puts the binary in `$GOPATH/bin` (or `$HOME/go/bin` by default).** If `prv: command not found` after installing, that directory likely isn't on your `$PATH` - see [Troubleshooting](#troubleshooting).

## How it decides what's a project

Every directory with a **project signal** becomes a row:

- a `.git` entry,
- a known **manifest** (`go.mod`, `package.json`, `pyproject.toml`, a `Dockerfile`, …),
- a **TODO file** (`TODO`, `TODO.md`, `TODO.txt`), or
- an explicit empty **`.prv` marker** (for write-ups or design folders with no manifest).

Dirs with *no signals of their own* but signalled children are treated as **containers**
and expanded into their children (up to `--depth`). Signal-less folders with no signalled
children, hidden dirs, and skip-listed paths (`node_modules`, `dist`, `build`, …) are
quietly omitted - no noise.

## Troubleshooting

**`prv: command not found` after `go install`**
`go install` places the binary in `$GOPATH/bin` (defaults to `$HOME/go/bin`). Add it to
your shell's `$PATH`, e.g. `export PATH="$HOME/go/bin:$PATH"`.

**A row shows `!` in the Git column**
That project's `.git` directory exists but `git -C <path>` failed to read its state
(corrupted repo, permissions issue, etc.). Run `git -C <path> status` directly to see the
underlying error.

**`↑N` / `↓N` shows a trailing `?`**
That means the ahead/behind count is from local tracking refs only - no fetch has run this
session, so it may be stale. Run with `--fetch` or press `f` in the TUI to refresh it.

## License

MIT
