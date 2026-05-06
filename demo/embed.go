// Package demo bundles small, license-clean fixture corpora that ship
// inside the segspec binary. They power the `segspec analyze --demo <name>`
// flag, which lets first-time users reproduce a realistic dependency graph
// without first finding a public repo to point segspec at.
//
// Why an embed.FS plus an extract-to-tempdir adapter: the existing walker
// (internal/walker) operates on filesystem paths via os.ReadFile and
// helm-template subprocess invocation. Refactoring every parser to take
// fs.FS would be a wide blast radius for a demo feature. Materializing
// the embedded fixture into a temp directory on demand is ~10 LOC and
// keeps the walker untouched.
package demo

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// corpus holds every demo fixture. The `all:` prefix is required so dotfiles
// like .env get embedded (the default embed pattern skips them).
//
//go:embed all:sentry-mini all:microservices-demo
var corpus embed.FS

// Demo describes a single bundled fixture. Description is shown by
// `--demo list`; Root is the directory inside the embedded FS that gets
// extracted to a temp dir when the user picks it.
type Demo struct {
	Name        string
	Description string
	Root        string
}

// Catalog returns the list of available demos in stable presentation
// order. New demos go here; the rest of the wiring is automatic.
func Catalog() []Demo {
	return []Demo{
		{
			Name:        "sentry-mini",
			Description: "Synthesized Sentry-style multi-service stack (~17 services, Compose + Helm + .env)",
			Root:        "sentry-mini",
		},
		{
			Name:        "microservices-demo",
			Description: "Adapted Google Cloud microservices-demo (6 services, gRPC mesh, Apache 2.0)",
			Root:        "microservices-demo",
		},
	}
}

// ErrUnknownDemo is returned by Resolve when the requested name does not
// match any entry in Catalog(). The error message lists valid names so
// the CLI can surface it directly without re-formatting.
var ErrUnknownDemo = errors.New("unknown demo")

// Resolve looks up a demo by name. The returned error wraps ErrUnknownDemo
// for the unknown-name case; callers can use errors.Is to distinguish.
func Resolve(name string) (Demo, error) {
	for _, d := range Catalog() {
		if d.Name == name {
			return d, nil
		}
	}
	return Demo{}, fmt.Errorf("%w: %q (valid: %s)", ErrUnknownDemo, name, strings.Join(names(), ", "))
}

func names() []string {
	out := make([]string, 0, len(Catalog()))
	for _, d := range Catalog() {
		out = append(out, d.Name)
	}
	sort.Strings(out)
	return out
}

// Materialize extracts the embedded fixture rooted at d.Root into a fresh
// temp directory and returns its absolute path. The caller is responsible
// for removing it (typically via defer os.RemoveAll). Files are written
// with mode 0o644 and directories with 0o755 — these fixtures are
// read-only data, no need to preserve special bits.
func (d Demo) Materialize() (string, error) {
	tmp, err := os.MkdirTemp("", "segspec-demo-"+d.Name+"-*")
	if err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	if err := copyEmbeddedTree(corpus, d.Root, tmp); err != nil {
		os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

// copyEmbeddedTree walks src inside the embed.FS and mirrors every file
// into dst on the real filesystem.
func copyEmbeddedTree(src fs.FS, srcRoot, dst string) error {
	return fs.WalkDir(src, srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(srcRoot, p)
		if relErr != nil {
			return relErr
		}
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		data, readErr := fs.ReadFile(src, p)
		if readErr != nil {
			return readErr
		}
		return os.WriteFile(out, data, 0o644)
	})
}
