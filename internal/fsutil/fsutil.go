// Package fsutil holds the filesystem-name predicates shared by the scanner
// and its engines: which names count as hidden, and which directories are
// never descended into. It is neutral ground — every engine package (project,
// scan, manifest, activity, todolist) imports this instead of importing each
// other, so the package graph stays acyclic. One fact, one home (AGENTS.md #6).
package fsutil

import "strings"

// SkipDirs are build-artifact / dependency directories never descended into
// during the non-git activity walk and never expanded as containers. Shared
// so the classifier and the activity walker agree.
var SkipDirs = map[string]bool{
	"node_modules": true,
	".git":         true,
	"dist":         true,
	"build":        true,
	"target":       true,
	".venv":        true,
	"bin":          true,
	"obj":          true,
	".next":        true,
	".cache":       true,
}

// IsHidden reports whether name begins with "." (POSIX-hidden, Windows junction
// paths included). A hidden name is never shown and never expanded or descended
// into, and a project signal (e.g. `.prv`) does NOT override hidden/skip —
// hidden/skip is checked before signals, so a signal inside a hidden dir never
// surfaces it.
func IsHidden(name string) bool { return strings.HasPrefix(name, ".") }

// IsSkipDir reports whether name matches the shared skip list.
func IsSkipDir(name string) bool { return SkipDirs[name] }
