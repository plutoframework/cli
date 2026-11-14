package migrate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type packageDecls struct {
	name  string
	decls map[string]bool
}

// danglingReferences reports authored Go uses of package-level declarations
// that the plan removes. It parses source only (go/ast, no type checking), so
// it reports selector uses through imports and unresolved same-package names.
func danglingReferences(w *Workspace, module, profile string) error {
	changed := map[string]bool{}

	for name := range w.plan.Writes {
		if path.Ext(name) == ".go" {
			changed[path.Dir(name)] = true
		}
	}

	for name := range w.plan.Deletes {
		if path.Ext(name) == ".go" {
			changed[path.Dir(name)] = true
		}
	}

	if len(changed) == 0 {
		return nil
	}

	sources, err := goSources(w.Root)
	if err != nil {
		return err
	}

	removed := map[string]packageDecls{}

	for directory := range changed {
		before, err := declarations(w, sources, directory, false)
		if err != nil {
			return err
		}

		after, err := declarations(w, sources, directory, true)
		if err != nil {
			return err
		}

		gone := map[string]bool{}

		for name := range before.decls {
			if !after.decls[name] || after.name != before.name {
				gone[name] = true
			}
		}

		if len(gone) > 0 {
			removed[directory] = packageDecls{before.name, gone}
		}
	}

	for _, name := range sources {
		if w.plan.Writes[name] != nil || w.plan.Deletes[name] {
			continue
		}

		source, _, err := w.Read(name)
		if err != nil {
			return err
		}

		inspectReferences(w, name, source, module, profile, removed)
	}

	return nil
}

func inspectReferences(
	w *Workspace,
	name string,
	source []byte,
	module, profile string,
	removed map[string]packageDecls,
) {
	files := token.NewFileSet()

	file, err := parser.ParseFile(files, name, source, 0)
	if err != nil {
		return
	}

	seen := map[string]bool{}
	report := func(position token.Pos, symbol string) {
		line := files.Position(position).Line

		key := symbol + "@" + strconv.Itoa(line)
		if seen[key] {
			return
		}

		seen[key] = true

		w.Manual(Item{
			Path: name,
			Line: line,
			Reason: "uses " + symbol + ", which the " + profile +
				" profile no longer declares",
			Action: "port this use to the " + profile + " API or remove " +
				"it",
		})
	}

	imported := map[string]packageDecls{}

	for _, spec := range file.Imports {
		importPath := strings.Trim(spec.Path.Value, `"`)
		if !strings.HasPrefix(importPath, module+"/") {
			continue
		}

		target, ok := removed[strings.TrimPrefix(importPath, module+"/")]
		if !ok {
			continue
		}

		local := target.name
		if spec.Name != nil {
			local = spec.Name.Name
		}

		imported[local] = target
	}

	ast.Inspect(file, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		ident, ok := selector.X.(*ast.Ident)
		if !ok || ident.Obj != nil {
			return true
		}

		if target, ok := imported[ident.Name]; ok &&
			target.decls[selector.Sel.Name] {
			report(selector.Pos(), ident.Name+"."+selector.Sel.Name)
		}

		return true
	})

	local, ok := removed[path.Dir(name)]
	if !ok || local.name != file.Name.Name {
		return
	}

	for _, ident := range file.Unresolved {
		if local.decls[ident.Name] {
			report(ident.Pos(), local.name+"."+ident.Name)
		}
	}
}

// declarations collects package-level names of non-test files in a directory,
// either as they are on disk or as they will be after the plan.
func declarations(
	w *Workspace,
	sources []string,
	directory string,
	planned bool,
) (packageDecls, error) {
	names := map[string]bool{}

	for _, name := range sources {
		if path.Dir(name) == directory {
			names[name] = true
		}
	}

	if planned {
		for name := range w.plan.Writes {
			if path.Dir(name) == directory && path.Ext(name) == ".go" {
				names[name] = true
			}
		}
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}

	sort.Strings(ordered)

	result := packageDecls{decls: map[string]bool{}}

	for _, name := range ordered {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}

		var (
			source []byte
			exists bool
			err    error
		)

		if planned {
			source, exists, err = w.Read(name)
		} else {
			source, exists, err = w.Original(name)
		}

		if err != nil {
			return result, err
		}

		if !exists {
			continue
		}

		file, err := parser.ParseFile(
			token.NewFileSet(),
			name,
			source,
			parser.SkipObjectResolution,
		)
		if err != nil {
			continue
		}

		result.name = file.Name.Name

		for _, decl := range file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				if decl.Recv == nil {
					result.decls[decl.Name.Name] = true
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						result.decls[spec.Name.Name] = true
					case *ast.ValueSpec:
						for _, ident := range spec.Names {
							result.decls[ident.Name] = true
						}
					}
				}
			}
		}
	}

	return result, nil
}

// goSources lists Go files under root, skipping hidden, vendor and
// node_modules directories. Symlinked entries are not followed.
func goSources(root string) ([]string, error) {
	result := []string{}

	err := filepath.WalkDir(
		root,
		func(current string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				base := entry.Name()
				if current != root && (strings.HasPrefix(base, ".") ||
					base == "vendor" || base == "node_modules") {
					return filepath.SkipDir
				}

				return nil
			}

			if entry.Type()&os.ModeSymlink != 0 ||
				filepath.Ext(current) != ".go" {
				return nil
			}

			relative, err := filepath.Rel(root, current)
			if err != nil {
				return err
			}

			result = append(result, filepath.ToSlash(relative))

			return nil
		},
	)

	sort.Strings(result)

	return result, err
}
