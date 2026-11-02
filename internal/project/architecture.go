package project

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

type ArchitectureDiagnostic struct {
	ID         string `json:"id"`
	File       string `json:"file"`
	Line       int    `json:"line"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	Repair     string `json:"repair"`
	SourceRole string `json:"source_role,omitempty"`
	TargetRole string `json:"target_role,omitempty"`
}

type ArchitecturePackage struct {
	Path string `json:"path"`
	Role string `json:"role"`
}

type ArchitectureReport struct {
	Version      int                      `json:"version"`
	Directory    string                   `json:"directory"`
	Packages     []ArchitecturePackage    `json:"packages"`
	Diagnostics  []ArchitectureDiagnostic `json:"diagnostics"`
	Unassessed   []string                 `json:"unassessed"`
	Passed       bool                     `json:"passed"`
	CheckedScope string                   `json:"checked_scope"`
}

type architectureImport struct {
	path, file string
	line       int
}
type architectureSource struct {
	role     string
	imports  []architectureImport
	contract bool
	file     string
}

// InspectArchitecture parses production sources without executing project
// tools,
// resolving dependencies, or modifying even obsolete manifests. It checks
// source
// dependencies, not authorization, business meaning, or runtime effects.
func InspectArchitecture(directory string) ArchitectureReport {
	root, err := filepath.Abs(directory)
	r := ArchitectureReport{
		Version: 1,
		CheckedScope: "passed means only implemented production Go " +
			"dependency, inventory, manifest, managed operation/entry " +
			"ownership, supported candidate contract/source confinement, " +
			"source-derived lifecycle boundary and local invocation " +
			"checks have no diagnostics; unassessed guarantees are never " +
			"implied",
		Directory:   root,
		Packages:    []ArchitecturePackage{},
		Diagnostics: []ArchitectureDiagnostic{},
		Unassessed: []string{
			"semantic business-rule placement, lifecycle commit enforcement, " +
				"principal provenance, resource authorization and " +
				"required CI execution are not assessed; generated " +
				"capability gates do not establish restricted write " +
				"adoption",
			"external dependency internals and frontend source dependencies " +
				"are not traversed",
			"contract classification is a syntactic declaration inventory, " +
				"not proof of purity; build-tag variants are inspected " +
				"together without type checking",
		},
	}

	add := func(id, file string, line int, message, repair string) {
		r.Diagnostics = append(
			r.Diagnostics,
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
	if err != nil {
		add(
			"ARCH001",
			".",
			1,
			err.Error(),
			"Provide an accessible project directory.",
		)

		return r
	}

	manifest, err := os.ReadFile(filepath.Join(root, "pluto.toml"))

	var m Manifest
	if err != nil {
		add(
			"ARCH001",
			"pluto.toml",
			1,
			"manifest unavailable: "+err.Error(),
			"Create or restore the project manifest; inspection does not "+
				"migrate it.",
		)
	} else if err = toml.Unmarshal(manifest, &m); err != nil {
		add(
			"ARCH001",
			"pluto.toml",
			1,
			"manifest cannot be parsed: "+err.Error(),
			"Repair TOML syntax without losing authored metadata.",
		)
	} else if m.SchemaVersion != SchemaVersion {
		add(
			"ARCH002",
			"pluto.toml",
			1,
			fmt.Sprintf(
				"schema %d requires explicit migration to %d; source "+
					"inspection continued",
				m.SchemaVersion,
				SchemaVersion,
			),
			"Review and explicitly migrate metadata and managed contracts "+
				"using the owning CLI version.",
		)
	}

	moduleData, err := os.ReadFile(filepath.Join(root, "go.mod"))
	module := ""

	if err == nil {
		module, _ = boundaryModulePath(moduleData)
	}

	if module == "" {
		add(
			"ARCH003",
			"go.mod",
			1,
			"module path unavailable; local dependency resolution is "+
				"unassessed",
			"Restore a valid module directive.",
		)
	}

	var owned map[string]bool
	if m.Boundary.Mode == "candidate" {
		owned, _ = CandidateManagedPaths(m)
	}

	directories := inspectionDirectories(root, module, owned)

	sources := map[string]*architectureSource{}

	err = filepath.WalkDir(
		root,
		func(path string, e fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}

			rel, _ := filepath.Rel(root, path)
			rel = filepath.ToSlash(rel)

			if e.IsDir() {
				if skipInspectionDirectory(rel, e.Name(), directories) {
					return filepath.SkipDir
				}

				if rel != "." {
					if _, err := os.Stat(
						filepath.Join(path, "go.mod"),
					); err == nil {
						r.Unassessed = append(
							r.Unassessed,
							"nested module excluded: "+rel,
						)

						return filepath.SkipDir
					}
				}

				return nil
			}

			if !strings.HasSuffix(rel, ".go") ||
				strings.HasSuffix(rel, "_test.go") {
				return nil
			}

			if e.Type()&os.ModeSymlink != 0 {
				add(
					"ARCH004",
					rel,
					1,
					"symlink source not inspected",
					"Use a regular source file.",
				)

				return nil
			}

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}

			if strings.Contains(string(data), "//go:build ") ||
				strings.Contains(string(data), "// +build ") {
				r.Unassessed = append(
					r.Unassessed,
					"build constraints not evaluated: "+rel,
				)
			}

			set := token.NewFileSet()

			tree, err := parser.ParseFile(set, path, nil, parser.AllErrors)
			if err != nil {
				add(
					"ARCH004",
					rel,
					1,
					"source parse failed: "+err.Error(),
					"Repair source syntax; other files remain inspected.",
				)

				return nil
			}

			dir := filepath.ToSlash(filepath.Dir(rel))

			s := sources[dir]
			if s == nil {
				s = &architectureSource{
					role:     architectureRole(dir),
					contract: true,
					file:     rel,
				}
				sources[dir] = s
			}

			for _, decl := range tree.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					s.contract = false
				case *ast.GenDecl:
					if d.Tok == token.VAR {
						s.contract = false
					}
				}
			}

			for _, spec := range tree.Imports {
				dep, _ := strconv.Unquote(spec.Path.Value)
				s.imports = append(
					s.imports,
					architectureImport{dep, rel, set.Position(spec.Pos()).Line},
				)
			}

			return nil
		},
	)
	if err != nil {
		add(
			"ARCH004",
			".",
			1,
			"source inventory incomplete: "+err.Error(),
			"Restore source access and rerun inspection.",
		)
	}

	keys := []string{}
	for key := range sources {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	for _, key := range keys {
		s := sources[key]
		if s.role == "unknown" && s.contract {
			s.role = "contract"
		}

		if s.role == "unknown" {
			add(
				"ARCH005",
				s.file,
				1,
				"unclassified production package: "+key,
				"Place the package under a documented responsibility root; "+
					"helpers retain transitive dependency checks.",
			)
		}

		if s.role == "policy" || s.role == "configuration" {
			r.Unassessed = append(

				r.Unassessed,

				"origin dependency restrictions not assessed for shared "+
					s.role+
					" package: "+
					key,
			)
		}

		r.Packages = append(r.Packages, ArchitecturePackage{key, s.role})
	}

	for _, key := range keys {
		origin := sources[key]
		if origin.role == "composition" || origin.role == "unknown" {
			continue
		}

		seen := map[string]bool{}

		var visit func(string, []string)

		visit = func(current string, chain []string) {
			if seen[current] {
				return
			}

			seen[current] = true

			s := sources[current]
			if s == nil {
				return
			}

			for _, dep := range s.imports {
				local := localImportDirectory(module, dep.path)

				role := architectureRole(local)
				if target := sources[local]; target != nil {
					role = target.role
				}

				if architectureForbidden(origin.role, role, dep.path) {
					add(
						"ARCH101",
						dep.file,
						dep.line,
						fmt.Sprintf(
							"%s package %s reaches forbidden %s via %s",
							origin.role,
							key,
							dep.path,
							strings.Join(append(chain, current), " -> "),
						),
						"Move concrete effects to adapters; inject a "+
							"consumer-owned port and keep shared wire "+
							"types in contract-only packages.",
					)

					r.Diagnostics[len(r.Diagnostics)-1].SourceRole = origin.role
					r.Diagnostics[len(r.Diagnostics)-1].TargetRole = role

					continue
				}

				if local != "" {
					if sources[local] == nil {
						add(
							"ARCH006",
							dep.file,
							dep.line,
							"local import cannot be inspected: "+dep.path,
							"Restore the local package or remove the "+
								"unresolved import.",
						)
					} else {
						visit(local, append(chain, current))
					}
				}
			}
		}
		visit(key, nil)
	}

	// A local script is editable evidence, never proof of trusted required CI.
	check, err := os.ReadFile(filepath.Join(root, "scripts/check.sh"))
	if err != nil || !architectureInvocation(string(check)) {
		add(
			"ARCH007",
			"scripts/check.sh",
			1,
			"canonical CLI check invocation is absent or unrecognized",
			"Have the project check script invoke pluto vet or pluto "+
				"architecture check; separately require it in trusted CI.",
		)
	}

	if m.Boundary.Mode == "candidate" {
		r.Unassessed[0] = "supported candidate source ownership and declared " +
			"capability shape are checked; arbitrary business semantics, " +
			"deployed SQL behavior, downstream idempotency, dependency " +
			"runtime correctness and trusted CI execution are not proved " +
			"by source inspection"
	}

	r.Diagnostics = append(
		r.Diagnostics,
		operationBoundaryDiagnostics(root, manifest)...)
	r.Diagnostics = append(
		r.Diagnostics,
		inspectEntryBoundaries(root, manifest)...)
	lifecycleDiagnostics, lifecycleUnassessed := inspectLifecycleBoundaries(
		root,
		module,
	)
	r.Diagnostics = append(r.Diagnostics, lifecycleDiagnostics...)
	r.Unassessed = append(r.Unassessed, lifecycleUnassessed...)
	r.Diagnostics = append(
		r.Diagnostics,
		inspectCandidateModules(root, manifest)...)
	candidateDiagnostics, candidateUnassessed := inspectCandidateBoundaries(
		root,
		module,
		manifest,
	)
	r.Diagnostics = append(r.Diagnostics, candidateDiagnostics...)
	r.Unassessed = append(r.Unassessed, candidateUnassessed...)
	primitiveDiagnostics, primitiveUnassessed := inspectPrimitiveBoundaries(
		root,
		module,
		manifest,
	)
	r.Diagnostics = append(r.Diagnostics, primitiveDiagnostics...)
	r.Unassessed = append(r.Unassessed, primitiveUnassessed...)
	sort.SliceStable(r.Diagnostics, func(i, j int) bool {
		a, b := r.Diagnostics[i], r.Diagnostics[j]
		if a.File != b.File {
			return a.File < b.File
		}

		if a.Line != b.Line {
			return a.Line < b.Line
		}

		if a.ID != b.ID {
			return a.ID < b.ID
		}

		return a.Message < b.Message
	})

	r.Passed = len(r.Diagnostics) == 0

	return r
}

func architectureRole(path string) string {
	for _, item := range []struct {
		root, role string
	}{
		{
			"cmd",
			"composition",
		},
		{
			"internal/app",
			"composition",
		},
		{
			"internal/config",
			"configuration",
		},
		{
			"internal/access",
			"policy",
		},
		{
			"internal/features",
			"domain",
		},
		{
			"internal/pluto",
			"entry",
		},
		{
			"internal/development",
			"composition",
		},
		{
			"internal/handlers",
			"entry",
		},
		{
			"internal/background",
			"entry",
		},
		{
			"internal/jobs",
			"entry",
		},
		{
			"internal/agents",
			"entry",
		},
		{
			"internal/jobcontracts",
			"contract",
		},
		{
			"internal/transport",
			"entry",
		},
		{
			"internal/tools",
			"entry",
		},
		{
			"internal/operations",
			"operations",
		},
		{
			"internal/domain",
			"domain",
		},
		{
			"internal/storage",
			"data",
		},
		{
			"internal/caching",
			"data",
		},
		{
			"internal/auth",
			"infrastructure",
		},
		{
			"internal/infrastructure",
			"infrastructure",
		},
		{
			"internal/paths",
			"contract",
		},
		{
			"internal/models",
			"contract",
		},
		{
			"internal/ports",
			"contract",
		},
		{
			"internal/api",
			"contract",
		},
		{
			"web",
			"presentation",
		},
	} {
		if path == item.root || strings.HasPrefix(path, item.root+"/") {
			return item.role
		}
	}

	return "unknown"
}

func architectureForbidden(source, target, dep string) bool {
	http := dep == "net/http" || strings.HasPrefix(dep, "net/http/") ||
		strings.HasPrefix(dep, "github.com/go-chi/")
	storage := dep == "database/sql" ||
		strings.HasPrefix(dep, "github.com/jackc/") ||
		strings.HasPrefix(dep, "github.com/redis/")
	effects := http || storage || dep == "os" || dep == "os/exec" ||
		dep == "net" ||
		dep == "crypto/rand" ||
		dep == "math/rand" ||
		strings.HasPrefix(dep, "math/rand/")

	switch source {
	case "domain":
		return effects || target == "operations" || target == "entry" ||
			target == "data" ||
			target == "infrastructure" ||
			target == "composition" ||
			target == "presentation"
	case "operations":
		return http || storage || target == "entry" || target == "data" ||
			target == "infrastructure" ||
			target == "composition" ||
			target == "presentation"
	case "data":
		return target == "entry" || target == "operations" ||
			target == "composition" ||
			target == "presentation"
	case "infrastructure":
		return target == "entry" || target == "operations" ||
			target == "data" ||
			target == "composition" ||
			target == "presentation"
	case "entry":
		return storage || target == "data" || target == "composition" ||
			target == "presentation"
	case "contract":
		return http || storage || target == "entry" || target == "operations" ||
			target == "data" ||
			target == "infrastructure" ||
			target == "composition"
	}

	return false
}

// This is evidence of a local invocation only. Arbitrary shell control flow and
// protected CI execution cannot be proved by inspecting an editable script.
func architectureInvocation(script string) bool {
	for _, line := range strings.Split(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "exec" {
			fields = fields[1:]
		}

		if len(fields) < 2 || fields[0] != "pluto" {
			continue
		}

		if fields[1] == "vet" ||
			len(fields) >= 3 && fields[1] == "architecture" &&
				fields[2] == "check" {
			return true
		}
	}

	return false
}
