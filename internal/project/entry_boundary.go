package project

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Entry checks identify supported framework registration forms. They do not
// infer whether arbitrary callbacks happen to perform equivalent authorization.
func inspectEntryBoundaries(
	root string,
	metadata []byte,
) []ArchitectureDiagnostic {
	var m Manifest

	_ = toml.Unmarshal(metadata, &m)

	diagnostics := []ArchitectureDiagnostic{}
	definitionPackages := workerDefinitionPackages(root)
	add := func(id, file string, line int, message, repair string) {
		diagnostics = append(
			diagnostics,
			ArchitectureDiagnostic{
				ID:       id,
				File:     file,
				Line:     line,
				Severity: "error",
				Message:  message,
				Repair:   repair,
			},
		)
	}

	for _, job := range m.Jobs {
		if operation, ok := m.Operation(
			job.Operation,
		); !ok ||
			operation.Public {
			add(
				"ARCH213",
				"pluto.toml",
				1,
				"worker entry has no protected independent operation "+
					"reference: "+job.Group+"."+job.Action,
				"Declare the operation once and set the job operation "+
					"reference; persisted actor identifiers do not "+
					"confer authority.",
			)
		}
	}

	for _, agent := range m.Agents {
		if operation, ok := m.Operation(
			agent.Operation,
		); !ok ||
			operation.Public ||
			(operation.Write &&
				m.Boundary.Mode != "candidate") ||

			operation.Permission == "" {
			add(
				"ARCH214",
				"pluto.toml",
				1,
				"agent tool has no supported protected read operation "+
					"reference: "+agent.Group+"."+agent.Action,
				"Declare a protected read operation and migrate tool "+
					"behavior into its private implementation.",
			)
		}
	}

	managed := map[string]bool{}

	if files, err := Managed(m); err == nil {
		for path := range files {
			managed[path] = true
		}
	}

	_ = filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			relative, _ := filepath.Rel(root, path)
			relative = filepath.ToSlash(relative)

			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", "testdata", "dist":
					return filepath.SkipDir
				}

				if relative != "." {
					if _, err := os.Stat(
						filepath.Join(path, "go.mod"),
					); err == nil {
						return filepath.SkipDir
					}
				}

				return nil
			}

			if !strings.HasSuffix(relative, ".go") ||
				strings.HasSuffix(relative, "_test.go") ||
				entry.Type()&os.ModeSymlink != 0 {
				return nil
			}

			set := token.NewFileSet()

			tree, err := parser.ParseFile(set, path, nil, 0)
			if err != nil {
				return nil
			}

			imports := map[string]string{}

			for _, spec := range tree.Imports {
				value, _ := strconv.Unquote(spec.Path.Value)

				name := filepath.Base(value)
				if spec.Name != nil {
					name = spec.Name.Name
				}

				imports[name] = value
				if name == "." &&
					(value == "github.com/plutoframework/pluto-jobs" ||
						value == "github.com/plutoframework/pluto-agent") {
					id := "ARCH211"
					if strings.HasSuffix(value, "pluto-agent") {
						id = "ARCH212"
					}

					add(
						id,
						relative,
						set.Position(spec.Pos()).Line,
						"dot-imported entry capabilities hide their declared "+
							"adapter ownership",
						"Use qualified framework-owned entry adapters; "+
							"dot-imported worker/tool capabilities are "+
							"unsupported.",
					)
				}
			}

			worker, tool := false, false
			allowedWorker := managed[relative] &&
				(strings.HasPrefix(
					relative,
					"internal/jobs/",
				) || strings.HasPrefix(
					relative,
					"internal/background/",
				) || strings.HasPrefix(
					relative,
					"internal/jobcontracts/",
				) ||
					relative == "internal/app/worker_runtime_gen.go" ||
					(m.Boundary.Mode == "candidate" &&
						(relative == "internal/app/candidate_operations_"+
							"gen.go" ||

							relative == "internal/access/caller_gen.go")))
			allowedTool := managed[relative] &&
				strings.HasPrefix(relative, "internal/agents/")

			ast.Inspect(tree, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				if !worker && !allowedWorker && selector.Sel.Name == "Bind" &&
					definitionPackages[filepath.Dir(path)] {
					add(
						"ARCH211",
						relative,
						set.Position(selector.Pos()).Line,
						"Jobs-definition package exposes an unmanaged worker "+
							"Bind path",
						"Bind typed deliveries only inside the declared "+
							"managed worker adapter; arbitrary callback "+
							"bindings cannot replace operation gates.",
					)

					worker = true
				}

				qualifier, ok := selector.X.(*ast.Ident)
				if !ok {
					return true
				}

				imported := imports[qualifier.Name]
				if !worker && !allowedWorker &&
					imported == "github.com/plutoframework/pluto-jobs" &&
					(selector.Sel.Name == "Handler" ||
						selector.Sel.Name == "Job" ||
						selector.Sel.Name == "Delivery") {
					add(
						"ARCH211",
						relative,
						set.Position(selector.Pos()).Line,
						"worker handler binding exists outside a declared "+
							"managed operation adapter",
						"Declare job.operation and move business behavior "+
							"behind the shared operation gate; keep "+
							"payload contracts separate from worker "+
							"handlers.",
					)

					worker = true
				}

				if imported == "github.com/plutoframework/pluto-jobs" &&
					selector.Sel.Name == "NewWorker" &&
					relative != "internal/app/worker_runtime_gen.go" &&
					(m.Boundary.Mode != "candidate" ||
						relative != "internal/app/"+
							"candidate_operations_gen.go" ||

						!managed[relative]) &&

					!worker {
					add(
						"ARCH211",
						relative,
						set.Position(selector.Pos()).Line,
						"raw worker construction bypasses the immutable "+
							"managed dispatch path",
						"Construct workers through the generated app "+
							"wrapper; inject sealed operation "+
							"dependencies, never replacement handler "+
							"maps.",
					)

					worker = true
				}

				if !tool && !allowedTool &&
					imported == "github.com/plutoframework/pluto-agent" &&
					(selector.Sel.Name == "ToolConfig" ||
						selector.Sel.Name == "NewTool") {
					add(
						"ARCH212",
						relative,
						set.Position(selector.Pos()).Line,
						"agent tool binding exists outside a declared "+
							"managed operation adapter",
						"Declare agent.operation and use the generated tool "+
							"adapter; remove authored business Handle/"+
							"Read callbacks.",
					)

					tool = true
				}

				return true
			})

			return nil
		},
	)

	return diagnostics
}

func workerDefinitionPackages(root string) map[string]bool {
	packages := map[string]bool{}
	_ = filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}

			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", "testdata", "dist":
					return filepath.SkipDir
				}

				if path != root {
					if _, err := os.Stat(
						filepath.Join(path, "go.mod"),
					); err == nil {
						return filepath.SkipDir
					}
				}

				return nil
			}

			if !strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return nil
			}

			tree, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				return nil
			}

			qualifiers := map[string]bool{}

			for _, spec := range tree.Imports {
				value, _ := strconv.Unquote(spec.Path.Value)
				if value != "github.com/plutoframework/pluto-jobs" {
					continue
				}

				name := filepath.Base(value)
				if spec.Name != nil {
					name = spec.Name.Name
				}

				qualifiers[name] = true
			}

			ast.Inspect(tree, func(node ast.Node) bool {
				selector, ok := node.(*ast.SelectorExpr)
				if !ok {
					return true
				}

				name, ok := selector.X.(*ast.Ident)
				if ok && qualifiers[name.Name] &&
					(selector.Sel.Name == "Definition" ||
						selector.Sel.Name == "NewDefinition") {
					packages[filepath.Dir(path)] = true
				}

				return true
			})

			return nil
		},
	)

	return packages
}
