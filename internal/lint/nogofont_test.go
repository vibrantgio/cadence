// Package lint holds repo-wide lint tests that enforce architecture
// decisions mechanically.
//
// TestNoGofontImports enforces ADR-003: no library source file may import
// gioui.org/font/gofont. It walks the entire repository from the module
// root, deliberately including any nested modules (e.g. a gallery/ demo
// module): the ADR bans gofont in library source, and the galleries were
// migrated off gofont too, so keeping the whole repo clean is the intent.
// Only testdata/, vendor/, node_modules/ and .git/ are skipped.
//
// The check matches actual parsed import declarations (go/parser in
// ImportsOnly mode), not substrings, so this file's own mention of the
// banned path does not trip the lint.
package lint

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// bannedImport is the import path forbidden by ADR-003. Both the exact
// path and any subpackage of it are rejected.
const bannedImport = "gioui.org/font/gofont"

func TestNoGofontImports(t *testing.T) {
	root, err := repoRoot()
	if err != nil {
		t.Fatalf("locating module root: %v", err)
	}

	var offenders []string
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "testdata", "vendor", "node_modules":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, imp := range f.Imports {
			p, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				continue
			}
			if p == bannedImport || strings.HasPrefix(p, bannedImport+"/") {
				rel, rerr := filepath.Rel(root, path)
				if rerr != nil {
					rel = path
				}
				offenders = append(offenders, rel)
				break
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if len(offenders) > 0 {
		t.Errorf("ADR-003 violation: %s imported by:\n  %s",
			bannedImport, strings.Join(offenders, "\n  "))
	}
}

// repoRoot walks up from the test's working directory (the package
// directory, per `go test` convention) to the nearest ancestor containing
// go.mod — the module root, which for this repo is also the repository
// root.
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
