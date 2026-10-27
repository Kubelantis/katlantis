package events_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/runatlantis/atlantis/testing"
)

// A WorkingDirLocker release that is never called keeps the pull locked on
// that replica until it restarts. Every caller must make sure no return path
// skips it, in one of three ways:
//
//   - `defer unlock()` in the function that took the lock;
//   - append it to a slice that a deferred function releases;
//   - return it, in which case the same rules apply to that function's
//     callers.
func TestWorkingDirLockReleasesAreDeferred(t *testing.T) {
	fset := token.NewFileSet()
	var funcs []*ast.FuncDecl
	err := filepath.WalkDir(filepath.Join(".."), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// The lockers themselves compose releases rather than defer them.
			if d.Name() == "mocks" || d.Name() == "testdata" || d.Name() == "pulllock" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || d.Name() == "working_dir_locker.go" {
			return nil
		}
		f, err := goparser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				funcs = append(funcs, fn)
			}
		}
		return nil
	})
	Ok(t, err)

	// Methods that return a release are lock calls too.
	lockers := map[string]bool{"TryLock": true, "TryLockPull": true}
	for changed := true; changed; {
		changed = false
		for _, fn := range funcs {
			if lockers[fn.Name.Name] {
				continue
			}
			for _, site := range lockSites(fn, lockers) {
				if site.returned {
					lockers[fn.Name.Name] = true
					changed = true
				}
			}
		}
	}

	found := 0
	for _, fn := range funcs {
		for _, site := range lockSites(fn, lockers) {
			found++
			if !site.released {
				t.Errorf("%s: %s is not released with defer in %s", fset.Position(site.pos), site.name, fn.Name.Name)
			}
		}
	}
	// Guards against the check silently matching nothing after a rename.
	Assert(t, found >= 15, "expected at least 15 working dir lock call sites, found %d", found)
}

type lockSite struct {
	pos      token.Pos
	name     string
	released bool
	returned bool
}

// lockSites finds `release, err := x.WorkingDirLocker.TryLock(...)` and
// calls of the methods in lockers, and how fn releases each.
func lockSites(fn *ast.FuncDecl, lockers map[string]bool) []lockSite {
	deferred := map[string]bool{}     // defer name()
	deferredRefs := map[string]bool{} // names a deferred closure uses
	returned := map[string]bool{}     // return name, ...
	appendedTo := map[string]string{} // slice = append(slice, name)
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.DeferStmt:
			if id, ok := n.Call.Fun.(*ast.Ident); ok && len(n.Call.Args) == 0 {
				deferred[id.Name] = true
			}
			if lit, ok := n.Call.Fun.(*ast.FuncLit); ok {
				ast.Inspect(lit.Body, func(m ast.Node) bool {
					if id, ok := m.(*ast.Ident); ok {
						deferredRefs[id.Name] = true
					}
					return true
				})
			}
		case *ast.ReturnStmt:
			if len(n.Results) > 0 {
				if id, ok := n.Results[0].(*ast.Ident); ok {
					returned[id.Name] = true
				}
			}
		case *ast.AssignStmt:
			if len(n.Lhs) == 1 && len(n.Rhs) == 1 {
				slice, ok1 := n.Lhs[0].(*ast.Ident)
				call, ok2 := n.Rhs[0].(*ast.CallExpr)
				if ok1 && ok2 && len(call.Args) == 2 {
					if f, ok := call.Fun.(*ast.Ident); ok && f.Name == "append" {
						if id, ok := call.Args[1].(*ast.Ident); ok {
							appendedTo[id.Name] = slice.Name
						}
					}
				}
			}
		}
		return true
	})

	var sites []lockSite
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		as, ok := n.(*ast.AssignStmt)
		if !ok || len(as.Rhs) != 1 || len(as.Lhs) != 2 {
			return true
		}
		call, ok := as.Rhs[0].(*ast.CallExpr)
		if !ok || !isLockCall(call, lockers) {
			return true
		}
		site := lockSite{pos: as.Pos(), name: "the release"}
		if id, ok := as.Lhs[0].(*ast.Ident); ok && id.Name != "_" {
			site.name = id.Name
			slice, appended := appendedTo[id.Name]
			site.returned = returned[id.Name]
			site.released = deferred[id.Name] || site.returned || (appended && deferredRefs[slice])
		}
		sites = append(sites, site)
		return true
	})
	return sites
}

// isLockCall reports whether call is x.WorkingDirLocker.TryLock(...),
// x.workingDirLocker.TryLockPull(...), or a call of another method in
// lockers, such as one that wraps TryLockPull and returns its release.
func isLockCall(call *ast.CallExpr, lockers map[string]bool) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || !lockers[sel.Sel.Name] {
		return false
	}
	if sel.Sel.Name != "TryLock" && sel.Sel.Name != "TryLockPull" {
		return true
	}
	var recv string
	switch x := sel.X.(type) {
	case *ast.SelectorExpr:
		recv = x.Sel.Name
	case *ast.Ident:
		recv = x.Name
	}
	return strings.EqualFold(recv, "WorkingDirLocker")
}
