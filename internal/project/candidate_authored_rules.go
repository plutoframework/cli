package project

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

// authoredRole is one authored role directory of one declared group.
type authoredRole struct {
	role  string
	group string
}

const (
	roleRules   = "rules"
	roleLogic   = "logic"
	roleAdapter = "adapter"

	candidateFactory = "internal/app/candidate_operations_gen.go"

	candidateAdaptersFile = "internal/app/candidate_adapters_gen.go"
	corePagination        = "github.com/plutoframework/pluto-core/pagination"
	coreState             = "github.com/plutoframework/pluto-core/state"
	pgxPgtype             = "github.com/jackc/pgx/v5/pgtype"
)

// addAuthoredRules registers role directories, capability owners, protected
// types and reserved names for the authored-body seam. It derives everything
// from the canonical manifest, never from file content.
func addAuthoredRules(
	rules *candidateRules,
	module string,
	m Manifest,
) {
	rules.roles = map[string]authoredRole{}

	for _, group := range authoredGroups(m) {
		rules.roles[rolesRulesDir+group] = authoredRole{roleRules, group}
		rules.roles[rolesLogicDir+group] = authoredRole{roleLogic, group}
		rules.roles[rolesAdaptersDir+group] = authoredRole{roleAdapter, group}
	}

	rules.dependencies = nil
	rules.foreignPorts = map[string]map[string]bool{}

	for _, operation := range m.Operations {
		for _, id := range operation.Ports {
			if !operation.isAuthored() || !foreignPort(operation.Group, id) {
				continue
			}

			owner, _, _ := splitQualified(id)

			if rules.foreignPorts[operation.Group] == nil {
				rules.foreignPorts[operation.Group] = map[string]bool{}
			}

			rules.foreignPorts[operation.Group][owner] = true
		}
	}

	if m.Authored != nil {
		for _, dependency := range m.Authored.Dependencies {
			rules.dependencies = append(rules.dependencies, dependency.Module)
		}
	}

	if !hasAuthoredOperations(m) {
		return
	}

	scopeNamespace := module + "/" + scopeDir
	bindOwners := []string{scopetestFile}
	provisionOwners := []string{}

	for _, operation := range m.Operations {
		if operation.Body != BodyAuthored {
			continue
		}

		base := "internal/operations/" + operation.Group + "/" +
			lowerAction(operation.Action)
		gate := base + "_gate_gen.go"
		workflow := base + "_workflow_gen.go"
		body := rolesLogicDir + operation.Group + "/" +
			lowerAction(operation.Action) + ".go"
		logicNamespace := module + "/" + rolesLogicDir + operation.Group

		if operation.Scope == ScopeProvision {
			provisionOwners = append(provisionOwners, workflow)
		} else {
			bindOwners = append(bindOwners, workflow)
		}

		rules.managed[gate] = true
		rules.managed[workflow] = true
		rules.managed[rolesLogicDir+operation.Group+"/contract_gen.go"] = true

		rules.addCapability(logicNamespace, operation.Action, workflow, body)

		rules.protectedTypes[logicNamespace+"."+operation.Action+"Ports"] = true

		lower := strings.ToLower(operation.Action[:1]) + operation.Action[1:]
		for _, suffix := range []string{"Ports", "IntentRequest"} {
			if rules.privateNames["internal/operations/"+operation.Group] ==
				nil {
				rules.privateNames["internal/operations/"+operation.Group] = map[string]string{}
			}

			rules.privateNames["internal/operations/"+operation.Group][lower+
				suffix] = gate
		}
	}

	rules.managed[scopeFile] = true
	rules.managed[scopetestFile] = true

	rules.addCapability(scopeNamespace, "Bind", bindOwners...)
	rules.addCapability(
		scopeNamespace,
		"BindProvisioning",
		provisionOwners...,
	)
	rules.addCapability(
		scopeNamespace,
		"NewDB",
		candidateFactory,
		candidateAdaptersFile,
		scopetestFile,
	)

	queryOwners := []string{}
	for _, set := range m.Queries {
		queryOwners = append(queryOwners, queriesHelperPath(set.Group))
		rules.managed[queriesHelperPath(set.Group)] = true
	}

	rules.addCapability(scopeNamespace, "Queries", queryOwners...)

	for _, name := range []string{
		"Execution", "Tenant", "DB", "Tx", "Provisioning",
	} {
		rules.protectedTypes[scopeNamespace+"."+name] = true
	}

	for _, name := range []string{
		"scopeExecution",
		"scopeGate",
		"scopeResource",
		"scopeTenant",
		"scopeDBPool",
		"scopeTx",
		"scopeProvision",
		"scopeSubject",
	} {
		if rules.globalPrivate[name] == nil {
			rules.globalPrivate[name] = map[string]bool{}
		}

		rules.globalPrivate[name][scopeFile] = true
	}
}

func (r *candidateRules) addCapability(
	namespace, name string,
	owners ...string,
) {
	key := namespace + "." + name
	if r.capabilities[key] == nil {
		r.capabilities[key] = map[string]bool{}
	}

	for _, owner := range owners {
		r.capabilities[key][owner] = true
	}
}

// stdlibImpure lists standard-library packages that reach the OS, network,
// process, filesystem or reflection. Authored roles never import them: I/O
// belongs to generated code and declared adapters.
var stdlibImpure = []string{
	"C", "unsafe", "reflect", "plugin", "os", "net", "syscall", "log",
	"database/sql", "io/fs", "io/ioutil", "path/filepath", "embed", "debug",
	"runtime", "testing", "go", "expvar", "html/template", "crypto/tls",
	"crypto/x509",
}

func stdlibPure(path string) bool {
	for _, impure := range stdlibImpure {
		if path == impure || strings.HasPrefix(path, impure+"/") {
			return false
		}
	}

	return true
}

func candidateStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")

	return !strings.Contains(first, ".")
}

// roleImportAllowed applies the per-role allowlist (ARCH610).
func (r candidateRules) roleImportAllowed(
	role authoredRole,
	module, path string,
) bool {
	if candidateStdlib(path) {
		return stdlibPure(path)
	}

	if path == corePagination || path == coreState {
		return true
	}

	if role.role == roleAdapter && path == pgxPgtype {
		return true
	}

	if path == module || strings.HasPrefix(path, module+"/") {
		return r.roleModuleImportAllowed(role, strings.TrimPrefix(
			path,
			module+"/",
		))
	}

	if strings.HasPrefix(path, "github.com/plutoframework/") ||
		candidateDataPackage(path) {
		return false
	}

	for _, dependency := range r.dependencies {
		if path == dependency || strings.HasPrefix(path, dependency+"/") {
			return true
		}
	}

	return false
}

func (r candidateRules) roleModuleImportAllowed(
	role authoredRole,
	relative string,
) bool {
	own := func(directory string) bool {
		return relative == directory+role.group
	}

	// Another group's ports package and its plain data models are visible to
	// the logic of an operation that declares one of its tx ports.
	foreign := func() bool {
		for _, prefix := range []string{"internal/ports/", "internal/models/"} {
			owner, ok := strings.CutPrefix(relative, prefix)
			if ok && r.foreignPorts[role.group][owner] {
				return true
			}
		}

		return false
	}
	anyRules := strings.HasPrefix(relative, rolesRulesDir) &&
		!strings.Contains(strings.TrimPrefix(relative, rolesRulesDir), "/")

	switch role.role {
	case roleRules:
		return anyRules
	case roleLogic:
		return anyRules || own("internal/models/") ||
			own("internal/ports/") || foreign() || relative == scopeDir ||
			relative == "internal/domain"
	default:
		return own(rolesRulesDir) || own("internal/models/") ||
			own("internal/ports/") || relative == scopeDir ||
			relative == "internal/domain" ||
			own("internal/storage/queries/")
	}
}

// inspectAuthoredRole enforces ARCH610 (imports) and ARCH611 (entry shape) on
// one file of a declared role directory.
func inspectAuthoredRole(
	file *candidateSource,
	rules candidateRules,
	role authoredRole,
	module string,
	emit func(id string, pos token.Pos, message, repair string),
) {
	for _, spec := range file.tree.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil || rules.roleImportAllowed(role, module, path) {
			continue
		}

		emit(
			"ARCH610",
			spec.Pos(),
			"authored "+role.role+" role imports outside its allowlist: "+
				path,
			"Rules stay pure; logic uses rules, models, ports, "+
				"internal/scope and internal/domain; adapters add their "+
				"sqlc package. Declare third-party modules in "+
				"[[authored.dependencies]] with a reason.",
		)
	}

	inspectAuthoredShape(file, role.role+" role", emit)
}

// inspectAuthoredImports applies seam-wide import rules to every unmanaged
// file: the scope test harness is test-only, and a logic package is reachable
// only from generated code.
func inspectAuthoredImports(
	file *candidateSource,
	rules candidateRules,
	module string,
	emit func(id string, pos token.Pos, message, repair string),
) {
	for _, spec := range file.tree.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}

		if path == module+"/"+scopeDir+"/scopetest" {
			emit(
				"ARCH610",
				spec.Pos(),
				"production source imports the test-only scope harness",
				"scopetest mints Executions for tests only; import it from "+
					"_test.go files.",
			)
		}

		prefix := module + "/" + rolesLogicDir
		if strings.HasPrefix(path, prefix) {
			group := strings.TrimPrefix(path, prefix)
			if role, ok := rules.roles[rolesLogicDir+group]; ok &&
				file.directory != rolesLogicDir+role.group {
				emit(
					"ARCH611",
					spec.Pos(),
					"authored logic is imported outside its generated "+
						"workflow: "+path,
					"Only the operation's generated workflow calls a body, "+
						"inside the gate.",
				)
			}
		}
	}
}

// checkAuthoredBodies reports a declared authored operation whose body file
// does not declare the exported function the contract asserts.
func checkAuthoredBodies(
	m Manifest,
	files []*candidateSource,
	add func(id, path string, line int, message, repair string),
) {
	declared := map[string]bool{}

	for _, file := range files {
		for _, declaration := range file.tree.Decls {
			if function, ok := declaration.(*ast.FuncDecl); ok &&
				function.Recv == nil {
				declared[file.path+"#"+function.Name.Name] = true
			}
		}
	}

	for _, operation := range m.Operations {
		if operation.Body != BodyAuthored {
			continue
		}

		path := rolesLogicDir + operation.Group + "/" +
			lowerAction(operation.Action) + ".go"
		if declared[path+"#"+operation.Action] {
			continue
		}

		add(
			"ARCH611",
			path,
			1,
			"authored body "+operation.Action+" is not declared in "+path,
			"Declare func "+operation.Action+" in "+path+" with the shape "+
				"asserted by contract_gen.go; the workflow calls only "+
				"that function.",
		)
	}
}
