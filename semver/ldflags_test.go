package semver

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Version, Commit and Date are set with -ldflags -X, which addresses a variable
// by its full import path. Go silently ignores -X for a symbol it cannot find,
// so renaming this package produces released binaries that report "dev" with no
// error from the compiler, the linker, GoReleaser or CI.
//
// That happened once: the package moved from internal/version to semver, and the
// v0.1.0 binaries shipped reporting "dev". These tests read the build
// configuration and assert the paths still resolve.

// xFlag captures the package path and variable from an -X argument.
var xFlag = regexp.MustCompile(`-X\s+'?([\w./\-]+)\.(\w+)=`)

func repoRoot(t *testing.T) string {
	t.Helper()
	// This test lives one directory below the module root.
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Skipf("cannot locate the module root: %v", err)
	}
	return root
}

func modulePath(t *testing.T, root string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module "))
		}
	}
	t.Fatal("no module directive in go.mod")
	return ""
}

// exportedVars returns the package-level var names declared in a directory.
func exportedVars(t *testing.T, dir string) map[string]bool {
	t.Helper()
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, dir, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", dir, err)
	}
	out := map[string]bool{}
	for _, pkg := range pkgs {
		for _, file := range pkg.Files {
			for _, decl := range file.Decls {
				gen, ok := decl.(*ast.GenDecl)
				if !ok || gen.Tok != token.VAR {
					continue
				}
				for _, spec := range gen.Specs {
					vs, ok := spec.(*ast.ValueSpec)
					if !ok {
						continue
					}
					for _, name := range vs.Names {
						out[name.Name] = true
					}
				}
			}
		}
	}
	return out
}

// checkLdflags asserts every -X target in a build file names a package that
// exists and a variable that package actually declares.
func checkLdflags(t *testing.T, root, module, file string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, file))
	if err != nil {
		t.Fatalf("read %s: %v", file, err)
	}

	// The Makefile parameterises the path, so expand its variables first.
	body := string(raw)
	body = strings.ReplaceAll(body, "$(MODULE)", module)
	for _, m := range regexp.MustCompile(`(?m)^VPKG\s*:?=\s*(\S+)`).FindAllStringSubmatch(body, -1) {
		body = strings.ReplaceAll(body, "$(VPKG)", m[1])
	}

	matches := xFlag.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatalf("%s declares no -X flags; version injection would be silently absent", file)
	}

	for _, m := range matches {
		pkgPath, varName := m[1], m[2]
		if !strings.HasPrefix(pkgPath, module) {
			t.Errorf("%s: -X targets %q, which is outside module %q", file, pkgPath, module)
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(pkgPath, module), "/")
		dir := filepath.Join(root, rel)
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			t.Errorf("%s: -X targets package %q but %s does not exist. "+
				"The linker ignores this silently and the binary reports \"dev\".",
				file, pkgPath, rel)
			continue
		}
		if vars := exportedVars(t, dir); !vars[varName] {
			t.Errorf("%s: -X sets %s.%s but that package declares no such var",
				file, pkgPath, varName)
		}
	}
}

func TestLdflagsTargetsARealPackage(t *testing.T) {
	root := repoRoot(t)
	module := modulePath(t, root)
	for _, file := range []string{"Makefile", ".goreleaser.yaml"} {
		t.Run(file, func(t *testing.T) {
			checkLdflags(t, root, module, file)
		})
	}
}

func TestBuildFilesInjectAllThreeVariables(t *testing.T) {
	// Version alone is not enough: a binary with no Commit or Date is harder to
	// trace back to a build.
	root := repoRoot(t)
	module := modulePath(t, root)

	for _, file := range []string{"Makefile", ".goreleaser.yaml"} {
		raw, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		body := strings.ReplaceAll(string(raw), "$(MODULE)", module)
		for _, want := range []string{"Version=", "Commit=", "Date="} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not inject %s", file, strings.TrimSuffix(want, "="))
			}
		}
	}
}

func TestInjectableVarsExist(t *testing.T) {
	// The counterpart check: these three must stay package-level vars, because a
	// const or a local cannot be set by the linker.
	root := repoRoot(t)
	vars := exportedVars(t, filepath.Join(root, "semver"))
	for _, name := range []string{"Version", "Commit", "Date"} {
		if !vars[name] {
			t.Errorf("%s must remain a package-level var for -ldflags -X to reach it", name)
		}
	}
}
