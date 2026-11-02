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

type candidateSource struct {
	path      string
	directory string
	namespace string
	imports   map[string]string
	tree      *ast.File
}
type candidateType struct {
	expression ast.Expr
	file       *candidateSource
}
type candidateDeclaration struct {
	Boundary struct {
		Mode string `toml:"mode"`
	} `toml:"boundary"`
	Resources  []ResourceContract  `toml:"resources"`
	Operations []Operation         `toml:"operations"`
	Contracts  []OperationContract `toml:"operation_contracts"`
	Effects    []EffectContract    `toml:"effects"`
	Jobs       []Job               `toml:"jobs"`
	Agents     []Agent             `toml:"agents"`
	Endpoints  []Endpoint          `toml:"endpoints"`
}

type candidateRules struct {
	globalPrivate      map[string]map[string]bool
	transportNamespace string
	managed            map[string]bool
	capabilities       map[string]map[string]bool
	protectedTypes     map[string]bool
	privateNames       map[string]map[string]string
	domainFiles        map[string]bool
}

// inspectCandidateBoundaries checks the closed, declared candidate source
// shape.
// It must run alongside exact managed-content and ordinary dependency checks.
// It does not establish external dependency integrity or arbitrary SQL meaning.
func inspectCandidateBoundaries(
	root, module string,
	metadata []byte,
) ([]ArchitectureDiagnostic, []string) {
	diagnostics := []ArchitectureDiagnostic{}
	add := func(id, path string, line int, message, repair string) {
		diagnostics = append(
			diagnostics,
			ArchitectureDiagnostic{
				ID:       id,
				File:     path,
				Line:     line,
				Severity: "error",
				Message:  message,
				Repair:   repair,
			},
		)
	}

	var declaration candidateDeclaration
	if err := toml.Unmarshal(metadata, &declaration); err != nil {
		add(
			"ARCH601",
			"pluto.toml",
			1,
			"candidate boundary metadata cannot be assessed: "+err.Error(),
			"Repair the declaration before relying on a supported authority "+
				"boundary.",
		)

		return diagnostics, nil
	}

	if declaration.Boundary.Mode != "candidate" {
		if declaration.Boundary.Mode != "" &&
			declaration.Boundary.Mode != "published" {
			add(
				"ARCH601",
				"pluto.toml",
				1,
				"unsupported boundary mode",
				"Select published compatibility or the explicit candidate "+
					"profile; do not silently invent a release contract.",
			)
		}

		for _, operation := range declaration.Operations {
			if !operation.Public {
				add(
					"ARCH401",
					"pluto.toml",
					1,
					"published operation lacks the required opaque identity, "+
						"resource and data contract: "+operation.ID(),
					"Preserve authored source and explicitly migrate to "+
						"supported candidate dependencies/contracts, or "+
						"wait for their release.",
				)
			}
		}

		return diagnostics, nil
	}

	if module == "" {
		add(
			"ARCH601",
			"pluto.toml",
			1,
			"candidate inspection requires the canonical module identity",
			"Repair project.module before generating and checking candidate "+
				"boundaries.",
		)
	}

	if len(declaration.Operations) > 0 || len(declaration.Resources) > 0 ||
		len(declaration.Contracts) > 0 ||
		len(declaration.Effects) > 0 {
		if err := ValidateContracts(
			declaration.Resources,
			declaration.Contracts,
			declaration.Effects,
		); err != nil {
			add(
				"ARCH601",
				"pluto.toml",
				1,
				err.Error(),
				"Declare every resource, permission, policy, bounded query, "+
					"replay obligation and effect before generation.",
			)
		}
	}

	contracts := map[string]OperationContract{}
	for _, contract := range declaration.Contracts {
		contracts[contract.ID] = contract
	}

	operations := map[string]Operation{}

	for _, operation := range declaration.Operations {
		if !validOperationID(operation.ID()) {
			add(

				"ARCH601",

				"pluto.toml",

				1,

				"invalid independent candidate operation identity: "+
					operation.ID(),

				"Repair group/action metadata before deriving owned source.",
			)
		}

		if _, duplicate := operations[operation.ID()]; duplicate {
			add(
				"ARCH601",
				"pluto.toml",
				1,
				"duplicate independent candidate operation: "+operation.ID(),
				"Declare each use case once and reference its shared "+
					"boundary from entries.",
			)
		}

		operations[operation.ID()] = operation
		contract, ok := contracts[operation.ID()]

		write := contract.Kind == "create" || contract.Kind == "transition" ||
			contract.Kind == "effect"
		if !ok || operation.Public || contract.Public != operation.Public ||
			contract.Permission != operation.Permission ||
			write != operation.Write ||
			contract.Idempotency != operation.Idempotency ||
			contract.ExemptionReason != operation.ReplayReason {
			add(
				"ARCH601",
				"pluto.toml",
				1,
				"operation authority contradicts or lacks its resource "+
					"contract: "+operation.ID(),
				"Match the independent operation's permission, write/public "+
					"mode and replay semantics to its declared resource "+
					"behavior.",
			)
		}
	}

	for _, contract := range declaration.Contracts {
		if _, ok := operations[contract.ID]; !ok {
			add(

				"ARCH601",

				"pluto.toml",

				1,

				"resource contract lacks its independent operation: "+
					contract.ID,

				"Declare the owning operation; an unrelated declaration "+
					"cannot satisfy this contract.",
			)
		}
	}

	for _, diagnostic := range diagnostics {
		if diagnostic.ID == "ARCH601" {
			return diagnostics, nil
		}
	}

	rules := candidateSourceRules(module, declaration)

	var canonical Manifest
	if err := toml.Unmarshal(metadata, &canonical); err != nil {
		add(
			"ARCH601",
			"pluto.toml",
			1,
			"canonical candidate ownership cannot be decoded: "+err.Error(),
			"Repair canonical metadata before inspecting source capabilities.",
		)

		return diagnostics, nil
	}

	canonical.Project.Module = module
	if err := ValidateManifestContracts(canonical); err != nil {
		add(
			"ARCH601",
			"pluto.toml",
			1,
			"candidate authority metadata is unsupported: "+err.Error(),
			"Repair profile, roles, entry authority and source paths before "+
				"deriving managed ownership.",
		)

		return diagnostics, nil
	}

	owned, err := CandidateManagedPaths(canonical)
	if err != nil {
		add(
			"ARCH601",
			"pluto.toml",
			1,
			"canonical candidate ownership cannot be derived: "+err.Error(),
			"Extend or repair the framework contract; filenames alone never "+
				"establish managed authority.",
		)

		return diagnostics, nil
	}

	rules.managed = owned
	managedDirectories := map[string]bool{}

	for path := range owned {
		if !strings.HasSuffix(path, ".go") {
			continue
		}

		for directory := filepath.ToSlash(
			filepath.Dir(
				path,
			),
		); directory != "."; directory = filepath.ToSlash(
			filepath.Dir(
				directory,
			),
		) {
			managedDirectories[directory] = true
		}
	}

	set := token.NewFileSet()
	files := []*candidateSource{}
	packages := map[string]string{}

	walkErr := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			relative, _ := filepath.Rel(root, path)

			relative = filepath.ToSlash(relative)
			if entry.Type()&fs.ModeSymlink != 0 {
				add(
					"ARCH605",
					"pluto.toml",
					1,
					"candidate source contains a filesystem alias: "+relative,
					"Replace symlinks with explicitly owned source; hidden "+
						"local build inputs cannot establish inspected "+
						"capability ownership.",
				)

				return nil
			}

			switch filepath.Ext(path) {
			case ".s", ".S", ".c", ".h", ".cc", ".cpp", ".syso":
				add(
					"ARCH605",
					"pluto.toml",
					1,
					"native application build input escapes candidate Go "+
						"ownership: "+relative,
					"Remove native build inputs or add an explicitly "+
						"reviewed framework contract; package-private "+
						"authority cannot be claimed for unchecked "+
						"native code.",
				)

				return nil
			}

			if entry.IsDir() {
				switch entry.Name() {
				case ".git", "node_modules", "vendor", "testdata", "dist":

					// A declared package may use a conventional ignored
					// directory name.
					// Its entire directory must be inspected, including
					// unmanaged siblings.
					if !managedDirectories[relative] {
						return filepath.SkipDir
					}
				}

				if path != root {
					if _, err := os.Stat(
						filepath.Join(path, "go.mod"),
					); err == nil {
						add(
							"ARCH608",
							filepath.ToSlash(filepath.Join(relative, "go.mod")),
							1,
							"nested module can replace source claimed by the "+
								"candidate owner",
							"Keep application source in the canonical "+
								"module. External dependencies require "+
								"explicit declared identity and must not "+
								"shadow managed application packages.",
						)

						return filepath.SkipDir
					}
				}

				return nil
			}

			if entry.Type()&fs.ModeSymlink != 0 ||
				!strings.HasSuffix(path, ".go") ||
				strings.HasSuffix(path, "_test.go") {
				return nil
			}

			tree, err := parser.ParseFile(set, path, nil, parser.ParseComments)
			if err != nil {
				return nil
			} // The canonical inventory reports parser failures.

			relative, err = filepath.Rel(root, path)
			if err != nil {
				return err
			}

			relative = filepath.ToSlash(relative)
			directory := filepath.ToSlash(filepath.Dir(relative))

			namespace := module
			if directory != "." {
				namespace += "/" + directory
			}

			file := &candidateSource{
				path:      relative,
				directory: directory,
				namespace: namespace,
				tree:      tree,
				imports:   map[string]string{},
			}
			files = append(files, file)
			packages[namespace] = tree.Name.Name

			return nil
		},
	)
	if walkErr != nil {
		add(
			"ARCH601",
			".",
			1,
			"candidate source inventory incomplete: "+walkErr.Error(),
			"Restore source access and rerun the canonical inspection.",
		)
	}

	types := map[string]candidateType{}
	functions := map[string]candidateType{}

	for _, file := range files {
		for _, spec := range file.tree.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				continue
			}

			name := candidateImportName(path, packages)
			if spec.Name != nil {
				name = spec.Name.Name
			}

			file.imports[name] = path
		}

		for _, node := range file.tree.Decls {
			switch declaration := node.(type) {
			case *ast.GenDecl:
				for _, item := range declaration.Specs {
					if spec, ok := item.(*ast.TypeSpec); ok {
						types[file.namespace+
							"."+
							spec.Name.Name] = candidateType{
							spec.Type,

							file,
						}
					}
				}
			case *ast.FuncDecl:
				if declaration.Recv == nil && declaration.Type.Results != nil &&
					len(declaration.Type.Results.List) > 0 {
					functionName := file.namespace + "." + declaration.Name.Name
					functions[functionName] = candidateType{
						declaration.Type.Results.List[0].Type,
						file,
					}
				}
			}
		}
	}

	for _, file := range files {
		emit := func(id string, pos token.Pos, message, repair string) {
			add(id, file.path, set.Position(pos).Line, message, repair)
		}

		for _, spec := range file.tree.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			if path == "unsafe" {
				emit(
					"ARCH605",
					spec.Pos(),
					"unsafe application code escapes the candidate authority "+
						"boundary",
					"Remove local unsafe code; private capability guarantees "+
						"cannot be claimed for it.",
				)
			}

			if !rules.managed[file.path] && candidateDataPackage(path) {
				emit(
					"ARCH608",
					spec.Pos(),
					"raw data capability exists outside an approved managed "+
						"adapter",
					"Use the declared resource data contract, connection or "+
						"authentication bootstrap. An independent "+
						"database/store client can bypass the required "+
						"gate.",
				)
			}

			// This reviewed OpenAPI generator is protected by canonical byte
			// integrity.
			// No other managed filename grants reflection authority.
			if path == "reflect" &&
				(file.path != "internal/api/schema.go" ||
					!rules.managed[file.path]) {
				emit(
					"ARCH605",
					spec.Pos(),
					"authored reflection is unsupported by the scoped-record "+
						"candidate profile",
					"Remove reflection or add a reviewed framework-owned "+
						"capability. Dynamic reflective calls can reach "+
						"unsafe memory APIs without importing unsafe.",
				)
			}

			if spec.Name != nil && spec.Name.Name == "." &&
				(path == module || strings.HasPrefix(
					path,
					module+"/",
				) || candidateCriticalPackage(
					path,
				)) {
				emit(
					"ARCH606",
					spec.Pos(),
					"dot import hides a restricted candidate capability",
					"Use explicit qualified imports so capability ownership "+
						"remains inspectable.",
				)
			}
		}

		for _, group := range file.tree.Comments {
			for _, comment := range group.List {
				if strings.HasPrefix(comment.Text, "//go:linkname") {
					emit(
						"ARCH605",
						comment.Pos(),
						"go:linkname escapes candidate package privacy",
						"Remove application linkname directives; external "+
							"dependency internals are a separate trust "+
							"boundary.",
					)
				}
			}
		}

		ast.Inspect(file.tree, func(node ast.Node) bool {
			if name, ok := node.(*ast.Ident); ok {
				if owners := rules.globalPrivate[name.Name]; owners != nil &&
					!owners[file.path] {
					emit(
						"ARCH604",
						name.Pos(),
						"reserved private candidate field escapes its exact "+
							"managed owner: "+name.Name,
						"Use the managed getters, caller adapter and runtime "+
							"factory; do not capture or reassign private "+
							"resource or authority storage.",
					)
				}
			}

			return true
		})

		if rules.managed[file.path] {
			continue
		}

		emit(
			"ARCH602",
			file.tree.Pos(),
			"production source is outside the candidate profile's exact "+
				"managed ownership",
			"Declare supported behavior in metadata. Unsupported hooks, "+
				"initializers, ports or adapters require a framework "+
				"contract extension; preserve authored code during "+
				"explicit migration.",
		)
		inspectCandidateFile(file, rules, types, functions, emit)
	}

	return diagnostics, []string{
		"candidate source checks compose with exact managed integrity; " +
			"external dependency internals and malicious configured " +
			"identity/policy providers are not verified",
	}
}

func candidateImportName(path string, packages map[string]string) string {
	if name := packages[path]; name != "" {
		return name
	}

	switch path {
	case "github.com/plutoframework/pluto-auth":
		return "auth"
	case "github.com/plutoframework/pluto-rbac":
		return "rbac"
	case "github.com/plutoframework/pluto-rbac/identity":
		return "identitygate"
	case "github.com/plutoframework/pluto-jobs":
		return "jobs"
	case "github.com/plutoframework/pluto-agent":
		return "agent"
	}

	return filepath.Base(path)
}

func candidateDataPackage(path string) bool {
	return path == "database/sql" || strings.HasPrefix(path, "database/sql/") ||
		strings.HasPrefix(
			path,
			"github.com/jackc/",
		) || strings.HasPrefix(path, "github.com/redis/") ||
		strings.HasPrefix(
			path,
			"github.com/plutoframework/pluto-core/stores/",
		) ||
		strings.HasPrefix(
			path,
			"github.com/plutoframework/pluto-jobs/stores/",
		) ||
		strings.HasPrefix(
			path,
			"github.com/plutoframework/pluto-auth/stores/",
		) ||
		path == "github.com/plutoframework/pluto-core/cache" ||
		strings.HasPrefix(path, "github.com/plutoframework/pluto-core/cache/")
}

func candidateCriticalPackage(path string) bool {
	return path == "github.com/plutoframework/pluto-auth" ||
		path == "github.com/plutoframework/pluto-rbac" ||
		path == "github.com/plutoframework/pluto-rbac/identity" ||
		path == lifecycleStatePackage
}
