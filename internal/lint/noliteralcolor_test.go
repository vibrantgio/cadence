package lint

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// exception exempts one file, or one directory subtree, from the literal
// colour ban. Paths are repo-relative and slash-separated; every entry
// must say why the colours there are legitimate.
type exception struct {
	path   string
	reason string
}

// exceptions lists the deliberate literal-colour sites in this repo.
//
// alert/alert.go and toast/toast.go were listed here until F4.6, each
// carrying a byte-identical copy of the same four Tailwind values for the
// success and warning accents. spectrum/tokens now derives both roles —
// hue-fixed ramps and pins, like error — so both entries are gone and the
// only exception left is a deliberate alpha composite.
var exceptions = []exception{
	{
		path: "modal/modal.go",
		// The scrim is black at 50% alpha in both themes by material
		// convention: it dims the scene by reducing luminance, so it is
		// theme-independent by design rather than a palette colour.
		reason: "theme-independent black scrim, alpha-composited dimmer",
	},
}

// TestNoLiteralColors enforces the design-token rule: library source must
// not hard-code colour values. Every colour a component paints comes from
// spectrum/tokens (and, once D1.1 lands, spectrum/color) — a hex literal in
// component code silently forks the palette.
//
// Like TestNoGofontImports, the check walks the entire repository from the
// module root, including nested modules, skipping only .git/, testdata/,
// vendor/ and node_modules/. Files are fully parsed (go/parser, not
// ImportsOnly) and matched on the AST, so this file's own prose does not
// trip the lint. It flags every composite literal of type <pkg>.NRGBA —
// where <pkg> is the local name of an import of image/color, aliased or
// dot-imported — whose elements are all basic literals, e.g.
// color.NRGBA{R: 0x15, G: 0x80, B: 0x3d, A: 0xff}, including such
// literals written element-wise inside a []<pkg>.NRGBA literal.
//
// Three shapes are deliberately not flagged:
//
//   - _test.go files, wholesale. Golden tests and fixtures paint specific
//     throwaway colours by nature; listing every fixture would bloat the
//     allow-list without adding protection, since test colours never ship.
//   - The empty literal color.NRGBA{}. The zero value expresses "unset"
//     or "fully transparent", not a colour choice.
//   - Literals with at least one non-literal element (variable, named
//     constant, call, field access). Those construct or derive a colour
//     at runtime — blends, lerps, tints of token colours — rather than
//     hard-coding one.
//
// Any remaining hard-coded colour in library source must be allow-listed
// in exceptions below with a reason, or migrated to a token.
//
// spectrum is exempt by not carrying this test: spectrum/tokens (and
// spectrum/color once D1.1 creates it) is where literal colours
// legitimately live. If this lint ever lands in spectrum, those two
// packages are its allow-list.
func TestNoLiteralColors(t *testing.T) {
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
		if !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, rerr := filepath.Rel(root, path)
		if rerr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if allowed(rel) {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		names, dot := colorImportNames(f)
		if len(names) == 0 && !dot {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			switch {
			case isNRGBA(cl.Type, names, dot):
				if len(cl.Elts) > 0 && allBasicLits(cl) {
					offenders = append(offenders,
						fmt.Sprintf("%s:%d", rel, fset.Position(cl.Pos()).Line))
				}
			case isNRGBASlice(cl.Type, names, dot):
				for _, e := range cl.Elts {
					inner, ok := e.(*ast.CompositeLit)
					if ok && inner.Type == nil && len(inner.Elts) > 0 && allBasicLits(inner) {
						offenders = append(offenders,
							fmt.Sprintf("%s:%d", rel, fset.Position(inner.Pos()).Line))
					}
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}

	if len(offenders) > 0 {
		t.Errorf("hard-coded colour literals in library source "+
			"(design colours come from spectrum tokens; "+
			"fix, or allow-list with a reason in internal/lint/noliteralcolor_test.go):\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// allowed reports whether the repo-relative slash path is covered by an
// exceptions entry, either exactly (file) or as a subtree (directory).
func allowed(rel string) bool {
	for _, e := range exceptions {
		if rel == e.path || strings.HasPrefix(rel, e.path+"/") {
			return true
		}
	}
	return false
}

// colorImportNames returns the local names under which the file imports
// image/color, and whether it dot-imports it.
func colorImportNames(f *ast.File) (names map[string]bool, dot bool) {
	names = map[string]bool{}
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != "image/color" {
			continue
		}
		switch {
		case imp.Name == nil:
			names["color"] = true
		case imp.Name.Name == ".":
			dot = true
		case imp.Name.Name == "_":
			// blank import: no reachable name
		default:
			names[imp.Name.Name] = true
		}
	}
	return names, dot
}

// isNRGBA reports whether the type expression denotes image/color's NRGBA
// under one of the file's local import names.
func isNRGBA(expr ast.Expr, names map[string]bool, dot bool) bool {
	switch t := expr.(type) {
	case *ast.SelectorExpr:
		id, ok := t.X.(*ast.Ident)
		return ok && names[id.Name] && t.Sel.Name == "NRGBA"
	case *ast.Ident:
		return dot && t.Name == "NRGBA"
	}
	return false
}

// isNRGBASlice reports whether the type expression is a slice or array of
// image/color's NRGBA, whose element literals may omit their type.
func isNRGBASlice(expr ast.Expr, names map[string]bool, dot bool) bool {
	arr, ok := expr.(*ast.ArrayType)
	return ok && isNRGBA(arr.Elt, names, dot)
}

// allBasicLits reports whether every element of the composite literal is a
// plain basic literal (optionally behind a field key) — i.e. the colour is
// written out in full rather than computed.
func allBasicLits(cl *ast.CompositeLit) bool {
	for _, e := range cl.Elts {
		if kv, ok := e.(*ast.KeyValueExpr); ok {
			e = kv.Value
		}
		if _, ok := e.(*ast.BasicLit); !ok {
			return false
		}
	}
	return true
}
