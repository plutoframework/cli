package project

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

// Authored role directories. Each declared group owns one directory per role;
// the role, not file content, decides which capabilities its files may use.
const (
	rolesRulesDir    = "internal/rules/"
	rolesLogicDir    = "internal/logic/"
	rolesAdaptersDir = "internal/adapters/"
	scopeDir         = "internal/scope"
	scopeFile        = scopeDir + "/scope_gen.go"
	scopetestFile    = scopeDir + "/scopetest/scopetest_gen.go"
	scopeStreamFile  = scopeDir + "/stream_gen.go"

	// tenantIndexFile is the generated enumeration of the tenant root table.
	tenantIndexFile = "internal/storage/tenantindex/index_gen.go"
)

// authoredPort is one declared port an authored operation consumes.
type authoredPort struct {
	Field string

	// Type is the port type in the group's contract: the full interface for
	// an own-group port, the narrow transactional one for another group's.
	Type string

	// Alias is the import alias of the owning ports package.
	Alias string
}

// foreignPortField names a cross-group port in the operation's Ports struct.
func foreignPortField(id string) string {
	return adapterField(Port{ID: id})
}

// authoredOp is the template view of one body = "authored" operation.
type authoredOp struct {
	ID         string
	Action     string
	Resource   string
	Result     string
	Pagination bool

	// Provision marks a scope = "provision" body, which receives a
	// scope.Provisioning instead of a scope.Execution.
	Provision bool

	// Principal is the machine principal of a machine-authenticated body or
	// an enrollment operation.
	Principal string

	// PagedRequest is set when the request model carries a Page the workflow
	// must bind from verified scope: a page read exposed as an endpoint.
	PagedRequest bool
	Ports        []authoredPort

	// Transactional is set when the operation writes through another group's
	// tx methods and therefore receives a scope.DB to open its transaction.
	Transactional bool

	// Stream describes a response = "stream" body: the typed channel it
	// receives and the limits the generated workflow enforces.
	IsStream  bool
	Stream    StreamLimits
	Inbound   bool
	StreamIn  string
	StreamOut string

	// EachTenant marks a scope = "each_tenant" body: it has no request, and
	// receives the scope.Execution of one enumerated tenant.
	EachTenant bool
	Job        tenantJobView
}

// tenantJobView carries the declared limits of one tenant job into its
// generated gate.
type tenantJobView struct {
	Kind        string
	Interval    int64
	Timeout     int64
	Concurrency int
	Attempts    int
}

// Returns is the Go result list of the authored body, gate and workflow.
func (a authoredOp) Returns() string {
	if a.Result == "" {
		return "error"
	}

	return a.Result + ", error"
}

type authoredGroupView struct {
	Ops            []authoredOp
	UsesPorts      bool
	UsesPagination bool

	// UsesModels is false when every operation is a tenant job, which has
	// no request model and may live in a group with no models package.
	UsesModels bool

	// ForeignGroups are the other groups whose ports packages the contract
	// imports for cross-group transactional writes.
	ForeignGroups []string
}

func modelName(id string) string {
	_, name, _ := splitQualified(id)

	return name
}

func authoredView(m Manifest, operation Operation) (authoredOp, error) {
	view := authoredOp{
		ID:        operation.ID(),
		Action:    operation.Action,
		Resource:  operation.Resource,
		Provision: operation.Scope == ScopeProvision,
		Principal: operation.MachinePrincipal(),
	}

	if operation.Scope == ScopeEachTenant {
		view.EachTenant = true
		view.Job = tenantJobView{
			Kind:        operation.JobKind(),
			Interval:    int64(operation.Interval()),
			Timeout:     int64(operation.TenantTimeout()),
			Concurrency: operation.TenantConcurrency(),
			Attempts:    operation.TenantAttempts(),
		}
	}

	switch operation.Response {
	case ResponsePage:
		view.Result = "pagination.Page[models." + modelName(operation.Item) +
			"]"
		view.Pagination = true

		for _, endpoint := range m.Endpoints {
			if endpoint.Operation == operation.ID() {
				view.PagedRequest = true
			}
		}
	case ResponseObject:
		view.Result = "models." + modelName(operation.Output)
	case ResponseStream:
		view.IsStream = true
		view.Stream = operation.StreamSettings()
		view.Inbound = operation.Input != ""
		view.StreamIn = "scope.NoMessage"
		view.StreamOut = "models." + modelName(operation.Item)

		if view.Inbound {
			view.StreamIn = "models." + modelName(operation.Input)
		}
	}

	if operation.Write && operation.Idempotency == "required" &&
		!(operation.isAuthoredWriteBody() && m.hasEndpoint(operation)) {
		return view, fmt.Errorf(
			"operation %s: idempotency = \"required\" needs an HTTP "+
				"endpoint (the generated Idempotency-Key claim); internal "+
				"authored writes use idempotency = \"exempt\" with a "+
				"replay_reason",
			operation.ID(),
		)
	}

	for _, id := range operation.Ports {
		if foreignPort(operation.Group, id) {
			owner, name, _ := splitQualified(id)
			view.Transactional = true
			view.Ports = append(view.Ports, authoredPort{
				Field: foreignPortField(id),
				Type:  owner + "ports." + name + "Tx",
				Alias: owner + "ports",
			})

			continue
		}

		view.Ports = append(view.Ports, authoredPort{
			Field: modelName(id),
			Type:  "ports." + modelName(id),
		})
	}

	return view, nil
}

// authoredGroups lists the groups that own authored role directories: any
// group with an authored operation, port, table or query set.
func authoredGroups(m Manifest) []string {
	seen := map[string]bool{}

	for _, operation := range m.Operations {
		if operation.Body == BodyAuthored {
			seen[operation.Group] = true
		}
	}

	for _, port := range m.Ports {
		group, _, _ := splitQualified(port.ID)
		seen[group] = true
	}

	for _, table := range m.Tables {
		seen[table.Group] = true
	}

	for _, set := range m.Queries {
		seen[set.Group] = true
	}

	groups := make([]string, 0, len(seen))
	for group := range seen {
		groups = append(groups, group)
	}

	sort.Strings(groups)

	return groups
}

func hasAuthoredOperations(m Manifest) bool {
	for _, operation := range m.Operations {
		if operation.Body == BodyAuthored {
			return true
		}
	}

	return false
}

func lowerAction(action string) string {
	return strings.ToLower(action)
}

// candidateAuthoredManaged renders the generated half of the authored-body
// seam: the sealed scope package, its test harness, each group's logic
// contract and every authored operation's gate and workflow. Handlers,
// adapters and composition are generated by their own owners.
func candidateAuthoredManaged(m Manifest) (map[string][]byte, error) {
	files := map[string][]byte{}
	if !hasAuthoredOperations(m) {
		return files, nil
	}

	data := dataFor(m, Endpoint{})

	scopeFiles := map[string]string{
		scopeFile:     "candidate_scope.go",
		scopetestFile: "candidate_scopetest.go",
	}

	if m.hasStreams() {
		scopeFiles[scopeStreamFile] = "candidate_stream_scope.go"
	}

	for path, name := range scopeFiles {
		source, err := render(name, data)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}

		files[path] = source
	}

	groups := map[string]*authoredGroupView{}

	for _, operation := range m.Operations {
		if operation.Body != BodyAuthored {
			continue
		}

		view, err := authoredView(m, operation)
		if err != nil {
			return nil, err
		}

		group := groups[operation.Group]
		if group == nil {
			group = &authoredGroupView{}
			groups[operation.Group] = group
		}

		group.Ops = append(group.Ops, view)
		group.UsesModels = group.UsesModels || !view.EachTenant

		for _, port := range view.Ports {
			group.UsesPorts = group.UsesPorts || port.Alias == ""
		}

		group.addForeign(operation)

		group.UsesPagination = group.UsesPagination || view.Pagination

		opData := dataFor(m, operation.Endpoint())
		opData.Authored = view

		base := "internal/operations/" + operation.Group + "/" +
			lowerAction(operation.Action)

		layout := map[string]string{
			"_gate_gen.go":     "candidate_authored_gate.go",
			"_workflow_gen.go": "candidate_authored_workflow.go",
		}

		if operation.Scope == ScopeProvision {
			layout = map[string]string{
				"_gate_gen.go":     "candidate_provision_gate.go",
				"_workflow_gen.go": "candidate_provision_workflow.go",
			}
		}

		if operation.isMachine() {
			layout = map[string]string{
				"_gate_gen.go":     "candidate_machine_gate.go",
				"_workflow_gen.go": "candidate_machine_workflow.go",
			}
		}

		if view.IsStream {
			layout = map[string]string{
				"_gate_gen.go":     "candidate_stream_gate.go",
				"_workflow_gen.go": "candidate_stream_workflow.go",
			}
		}

		if operation.Scope == ScopeEachTenant {
			layout = map[string]string{
				"_gate_gen.go":     "candidate_tenantjob_gate.go",
				"_workflow_gen.go": "candidate_tenantjob_workflow.go",
			}
		}

		for suffix, name := range layout {
			source, err := render(name, opData)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", base+suffix, err)
			}

			files[base+suffix] = source
		}
	}

	if m.hasEachTenant() {
		source, err := render("candidate_tenant_index.go", data)
		if err != nil {
			return nil, err
		}

		files[tenantIndexFile] = source
	}

	for name, group := range groups {
		groupData := dataFor(m, Endpoint{Group: name})
		groupData.AuthoredGroup = *group

		source, err := render("candidate_logic_contract.go", groupData)
		if err != nil {
			return nil, err
		}

		files[rolesLogicDir+name+"/contract_gen.go"] = source
	}

	return files, nil
}

// candidateAuthoredScaffold renders the authored half, created once for a new
// operation and never regenerated: the logic body stub and, for the group's
// first authored operation, the rules package.
func candidateAuthoredScaffold(
	m Manifest,
	operation Operation,
) (map[string][]byte, error) {
	files := map[string][]byte{}
	if operation.Body != BodyAuthored {
		return files, nil
	}

	view, err := authoredView(m, operation)
	if err != nil {
		return nil, err
	}

	data := dataFor(m, operation.Endpoint())
	data.Authored = view

	source, err := render("candidate_logic_scaffold.go", data)
	if err != nil {
		return nil, err
	}

	files[rolesLogicDir+operation.Group+"/"+
		lowerAction(operation.Action)+".go"] = source

	for _, other := range m.Operations {
		if other.Body != BodyAuthored || other.Group != operation.Group {
			continue
		}

		if other.ID() == operation.ID() {
			rules, err := render("candidate_rules_scaffold.go", data)
			if err != nil {
				return nil, err
			}

			files[rolesRulesDir+operation.Group+"/doc.go"] = rules
		}

		break
	}

	return files, nil
}

// allAuthored reports whether every operation has an authored body, so no
// resource data contract applies.
func allAuthored(operations []Operation) bool {
	for _, operation := range operations {
		if !operation.outsideContracts() {
			return false
		}
	}

	return len(operations) > 0
}

func (g *authoredGroupView) addForeign(operation Operation) {
	for _, id := range operation.Ports {
		if !foreignPort(operation.Group, id) {
			continue
		}

		owner, _, _ := splitQualified(id)
		if !slices.Contains(g.ForeignGroups, owner) {
			g.ForeignGroups = append(g.ForeignGroups, owner)
			sort.Strings(g.ForeignGroups)
		}
	}
}
