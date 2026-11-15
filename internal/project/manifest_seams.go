package project

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

// Authored-code seam declarations. They exist only in the candidate profile
// and describe the typed surface that generated code exposes to authored role
// packages. This file validates declarations; it generates nothing.

const (
	BodyAuthored  = "authored"
	BodyGenerated = "generated"
	BodySystem    = "system"

	ResponsePage   = "page"
	ResponseObject = "object"
	ResponseStream = "stream"
)

// Table declares an authored-data table the framework checks: tenant column,
// append-only triggers and forced row-level security.
type Table struct {
	Group        string `toml:"group"`
	Name         string `toml:"name"`
	TenantColumn string `toml:"tenant_column"`
	AppendOnly   bool   `toml:"append_only,omitempty"`
	RowSecurity  bool   `toml:"row_security"`

	// TenantRoot marks the table whose rows are the tenants themselves: its
	// tenant_column is its own identifier. At most one table is the root.
	TenantRoot bool `toml:"tenant_root,omitempty"`
}

func (t Table) ID() string {
	return t.Group + "." + t.Name
}

// QuerySet declares one authored sqlc query set for a group.
type QuerySet struct {
	Group  string   `toml:"group"`
	Files  []string `toml:"files"`
	Tables []string `toml:"tables"`
}

// Port declares an interface from authored logic to an authored adapter.
type Port struct {
	ID      string   `toml:"id"`
	Adapter string   `toml:"adapter"`
	Methods []string `toml:"methods"`

	// TxMethods names the methods another group's operation may call inside
	// its own transaction; each takes scope.Tx. TxCallers names the only
	// operations allowed to do so (see manifest_txwrite.go).
	TxMethods []string `toml:"tx_methods,omitempty"`
	TxCallers []string `toml:"tx_callers,omitempty"`
}

// Model declares typed fields that generate structs and OpenAPI schemas.
type Model struct {
	ID     string   `toml:"id"`
	Fields []string `toml:"fields"`
}

// OperationQuery declares one typed query parameter of a read operation.
type OperationQuery struct {
	Name  string `toml:"name"`
	Type  string `toml:"type"`
	Max   int    `toml:"max,omitempty"`
	After string `toml:"after,omitempty"`
}

// Authored holds declarations that widen what authored packages may import.
type Authored struct {
	Dependencies []AuthoredDependency `toml:"dependencies,omitempty"`
}

// AuthoredDependency admits one third-party module to authored roles.
type AuthoredDependency struct {
	Module string `toml:"module"`
	Reason string `toml:"reason"`
}

// Memberships binds tenant membership to an app-owned declared table, for
// example users(org_id, id, role).
type Memberships struct {
	Table         string `toml:"table"`
	TenantColumn  string `toml:"tenant_column"`
	SubjectColumn string `toml:"subject_column"`
	RoleColumn    string `toml:"role_column"`

	// ActiveColumn restricts membership to active rows so suspended or
	// deprovisioned subjects lose their role on the next request. With
	// ActiveValues the column is compared as text against that set; without
	// it the column must be boolean and true.
	ActiveColumn string   `toml:"active_column,omitempty"`
	ActiveValues []string `toml:"active_values,omitempty"`
}

var activeValue = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

var (
	snakeName   = regexp.MustCompile(`^[a-z][a-z0-9]*(?:_[a-z0-9]+)*$`)
	exportName  = regexp.MustCompile(`^[A-Z][a-zA-Z0-9]*$`)
	resourceRef = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`)
	modelField  = regexp.MustCompile(
		`^([a-z][a-z0-9]*(?:_[a-z0-9]+)*):([a-z0-9]+)$`,
	)
)

var modelFieldTypes = map[string]bool{
	"int64":  true,
	"string": true,
	"json":   true,
	"hex":    true,
	"time":   true,
	"bool":   true,
}

var queryParamTypes = map[string]bool{
	"string": true,
	"int64":  true,
	"bool":   true,
	"time":   true,
}

const (
	maxQueryString = 4096
	maxQueryInt    = 1000
)

func (m Manifest) hasSeamDeclarations() bool {
	if len(m.Tables)+len(m.Queries)+len(m.Ports)+len(m.Models)+
		len(m.Lookups) != 0 || len(m.Principals) != 0 ||
		m.Authored != nil || m.Memberships != nil ||
		m.Database.RuntimeRole != "" {
		return true
	}

	for _, operation := range m.Operations {
		if operation.usesSeams() {
			return true
		}
	}

	for _, entry := range m.Endpoints {
		if entry.Transport != "" || len(entry.Origins) != 0 {
			return true
		}
	}

	return false
}

func (o Operation) usesSeams() bool {
	return o.Body != "" || o.Resource != "" || o.ResourcePolicy != "" ||
		len(o.Ports) != 0 || o.Response != "" || o.Item != "" ||
		o.Output != "" || o.Input != "" || len(o.Query) != 0 ||
		o.Scope != "" || o.Lookup != "" || o.RateLimit != "" ||
		o.Auth != "" || o.Principal != "" || o.Stream != nil
}

// validateSeams checks every seam declaration. Published projects may declare
// none of them.
func validateSeams(m Manifest) error {
	if m.Boundary.Mode != "candidate" {
		if m.hasSeamDeclarations() {
			return fmt.Errorf(
				"authored-code seam declarations require the candidate " +
					"profile",
			)
		}

		return nil
	}

	tables, err := validateTables(m.Tables)
	if err != nil {
		return err
	}

	if err := validateQuerySets(m.Queries, tables); err != nil {
		return err
	}

	ports, err := validatePorts(m.Ports)
	if err != nil {
		return err
	}

	models, err := validateModels(m.Models)
	if err != nil {
		return err
	}

	if err := validateAuthored(m.Authored); err != nil {
		return err
	}

	if err := rejectWebSocketDependencies(m.Authored); err != nil {
		return err
	}

	if err := validateMemberships(m.Memberships, tables); err != nil {
		return err
	}

	if err := validateTxCallers(m.Ports, m.Operations); err != nil {
		return err
	}

	if err := validateMachineSystem(m); err != nil {
		return err
	}

	if err := validatePrincipals(m, tables); err != nil {
		return err
	}

	for _, operation := range m.Operations {
		if err := operation.validateSeams(m, ports, models); err != nil {
			return fmt.Errorf("operation %s: %w", operation.ID(), err)
		}
	}

	if err := validateStreams(m); err != nil {
		return err
	}

	return validateSystem(m, tables, models)
}

func validateTables(tables []Table) (map[string]Table, error) {
	seen := map[string]Table{}

	for _, table := range tables {
		if !packageName.MatchString(table.Group) ||
			!snakeName.MatchString(table.Name) {
			return nil, fmt.Errorf(
				"table %q requires a lowercase group and snake_case name",
				table.ID(),
			)
		}

		if !snakeName.MatchString(table.TenantColumn) {
			return nil, fmt.Errorf(
				"table %s requires a snake_case tenant_column",
				table.ID(),
			)
		}

		if !table.RowSecurity {
			return nil, fmt.Errorf(
				"table %s requires row_security = true; forced row-level "+
					"security is mandatory",
				table.ID(),
			)
		}

		if _, dup := seen[table.ID()]; dup {
			return nil, fmt.Errorf("duplicate table %s", table.ID())
		}

		seen[table.ID()] = table
	}

	return seen, validateTenantRoots(tables)
}

func validateQuerySets(sets []QuerySet, tables map[string]Table) error {
	seen := map[string]bool{}

	for _, set := range sets {
		if !packageName.MatchString(set.Group) {
			return fmt.Errorf("query set group %q is invalid", set.Group)
		}

		if seen[set.Group] {
			return fmt.Errorf("duplicate query set for group %s", set.Group)
		}

		seen[set.Group] = true

		if len(set.Files) == 0 || len(set.Tables) == 0 {
			return fmt.Errorf(
				"query set %s requires files and tables",
				set.Group,
			)
		}

		for _, pattern := range set.Files {
			if err := validateQueryGlob(set.Group, pattern); err != nil {
				return err
			}
		}

		listed := map[string]bool{}

		for _, name := range set.Tables {
			if _, ok := tables[set.Group+"."+name]; !ok {
				return fmt.Errorf(
					"query set %s references undeclared table %s",
					set.Group,
					name,
				)
			}

			if listed[name] {
				return fmt.Errorf(
					"query set %s lists table %s twice",
					set.Group,
					name,
				)
			}

			listed[name] = true
		}
	}

	return nil
}

func validateQueryGlob(group, pattern string) error {
	prefix := "db/queries/" + group + "/"
	clean := path.Clean(pattern)

	if clean != pattern || !strings.HasPrefix(pattern, prefix) ||
		!strings.HasSuffix(pattern, ".sql") ||
		strings.Contains(pattern, "..") || strings.Contains(pattern, `\`) {
		return fmt.Errorf(
			"query files %q must be clean .sql paths under %s",
			pattern,
			prefix,
		)
	}

	if _, err := path.Match(pattern, "x.sql"); err != nil {
		return fmt.Errorf("query files %q: %w", pattern, err)
	}

	return nil
}

func validatePorts(ports []Port) (map[string]Port, error) {
	seen := map[string]Port{}

	for _, port := range ports {
		group, name, ok := splitQualified(port.ID)
		if !ok || !packageName.MatchString(group) ||
			!exportName.MatchString(name) {
			return nil, fmt.Errorf(
				"port id %q must be <group>.<ExportedName>",
				port.ID,
			)
		}

		if _, dup := seen[port.ID]; dup {
			return nil, fmt.Errorf("duplicate port %s", port.ID)
		}

		adapterGroup, adapter, ok := splitQualified(port.Adapter)
		if !ok || adapterGroup != group || !exportName.MatchString(adapter) {
			return nil, fmt.Errorf(
				"port %s adapter must be %s.<ExportedName>",
				port.ID,
				group,
			)
		}

		if len(port.Methods) == 0 {
			return nil, fmt.Errorf("port %s requires methods", port.ID)
		}

		methods := map[string]bool{}

		for _, method := range port.Methods {
			if !validPortMethod(method) || methods[portMethodName(method)] {
				return nil, fmt.Errorf(
					"port %s has invalid or duplicate method %q",
					port.ID,
					method,
				)
			}

			methods[portMethodName(method)] = true
		}

		if err := validateTxMethods(port); err != nil {
			return nil, err
		}

		seen[port.ID] = port
	}

	return seen, nil
}

func validateModels(models []Model) (map[string]Model, error) {
	seen := map[string]Model{}

	for _, model := range models {
		group, name, ok := splitQualified(model.ID)
		if !ok || !packageName.MatchString(group) ||
			!exportName.MatchString(name) {
			return nil, fmt.Errorf(
				"model id %q must be <group>.<ExportedName>",
				model.ID,
			)
		}

		if _, dup := seen[model.ID]; dup {
			return nil, fmt.Errorf("duplicate model %s", model.ID)
		}

		if len(model.Fields) == 0 {
			return nil, fmt.Errorf("model %s requires fields", model.ID)
		}

		fields := map[string]bool{}

		for _, field := range model.Fields {
			match := modelField.FindStringSubmatch(field)
			if match == nil || !modelFieldTypes[match[2]] {
				return nil, fmt.Errorf(
					"model %s field %q must be name:type with type int64, "+
						"string, json, hex, time or bool",
					model.ID,
					field,
				)
			}

			if fields[match[1]] {
				return nil, fmt.Errorf(
					"model %s repeats field %s",
					model.ID,
					match[1],
				)
			}

			fields[match[1]] = true
		}

		seen[model.ID] = model
	}

	return seen, nil
}

func validateAuthored(authored *Authored) error {
	if authored == nil {
		return nil
	}

	seen := map[string]bool{}

	for _, dependency := range authored.Dependencies {
		first, _, _ := strings.Cut(dependency.Module, "/")
		if !moduleName.MatchString(dependency.Module) ||
			!strings.Contains(first, ".") ||
			strings.HasPrefix(dependency.Module, "github.com/plutoframework/") {
			return fmt.Errorf(
				"authored dependency %q must be a third-party module path",
				dependency.Module,
			)
		}

		if seen[dependency.Module] {
			return fmt.Errorf(
				"duplicate authored dependency %s",
				dependency.Module,
			)
		}

		seen[dependency.Module] = true

		if strings.TrimSpace(dependency.Reason) == "" ||
			len(dependency.Reason) > 500 {
			return fmt.Errorf(
				"authored dependency %s requires a reason (<=500 bytes)",
				dependency.Module,
			)
		}
	}

	return nil
}

func validateMemberships(
	memberships *Memberships,
	tables map[string]Table,
) error {
	if memberships == nil {
		return nil
	}

	table, ok := tables[memberships.Table]
	if !ok {
		return fmt.Errorf(
			"memberships table %q must be a declared table",
			memberships.Table,
		)
	}

	columns := map[string]bool{}

	for _, column := range []string{
		memberships.TenantColumn,
		memberships.SubjectColumn,
		memberships.RoleColumn,
	} {
		if !snakeName.MatchString(column) || columns[column] {
			return fmt.Errorf(
				"memberships columns must be distinct snake_case names",
			)
		}

		columns[column] = true
	}

	if memberships.TenantColumn != table.TenantColumn {
		return fmt.Errorf(
			"memberships tenant_column must equal table %s tenant_column %s",
			table.ID(),
			table.TenantColumn,
		)
	}

	return validateMembershipActive(memberships)
}

var runtimeRoleName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)

func validateRuntimeRole(m Manifest) error {
	role := m.Database.RuntimeRole
	if role == "" {
		return nil
	}

	if !runtimeRoleName.MatchString(role) ||
		role == "postgres" || role == "public" ||
		strings.HasPrefix(role, "pg_") {
		return fmt.Errorf(
			"database runtime_role must be a lowercase snake_case role name " +
				"that is not postgres, public or pg_*",
		)
	}

	if len(m.Tables) == 0 {
		return fmt.Errorf("database runtime_role requires declared tables")
	}

	// The framework membership table is created in the reserved range after
	// every numbered migration, so no earlier grant can cover it.
	if m.Memberships == nil {
		return fmt.Errorf(
			"database runtime_role requires [memberships]: the framework " +
				"membership table is created after the role migration",
		)
	}

	return nil
}

func validateMembershipActive(memberships *Memberships) error {
	if memberships.ActiveColumn == "" {
		if len(memberships.ActiveValues) != 0 {
			return fmt.Errorf("memberships active_values require active_column")
		}

		return nil
	}

	if !snakeName.MatchString(memberships.ActiveColumn) {
		return fmt.Errorf("memberships active_column must be snake_case")
	}

	for _, other := range []string{
		memberships.TenantColumn,
		memberships.SubjectColumn,
		memberships.RoleColumn,
	} {
		if other == memberships.ActiveColumn {
			return fmt.Errorf(
				"memberships active_column must be distinct from other columns",
			)
		}
	}

	if len(memberships.ActiveValues) > 16 {
		return fmt.Errorf("memberships active_values allows at most 16 values")
	}

	seen := map[string]bool{}

	for _, value := range memberships.ActiveValues {
		if !activeValue.MatchString(value) || seen[value] {
			return fmt.Errorf(
				"memberships active_values must be distinct 1-64 character " +
					"values of letters, digits, '_', '-' or '.'",
			)
		}

		seen[value] = true
	}

	return nil
}

func (o Operation) validateSeams(
	m Manifest,
	ports map[string]Port,
	models map[string]Model,
) error {
	switch o.Body {
	case "", BodyGenerated, BodyAuthored, BodySystem:
	default:
		return fmt.Errorf("body must be authored or generated")
	}

	if o.Scope != "" {
		return o.validateSystemSeams(m, ports, models)
	}

	if o.Principal != "" {
		return fmt.Errorf("principal requires scope = \"enroll\"")
	}

	if o.isMachine() {
		return o.validateMachineOperation(m, ports, models)
	}

	if o.Body == BodySystem {
		return fmt.Errorf("body = \"system\" requires scope = \"lookup\"")
	}

	if o.Body != BodyAuthored {
		if len(o.Ports) != 0 || o.Resource != "" || o.ResourcePolicy != "" {
			return fmt.Errorf(
				"ports, resource and resource_policy require body = " +
					"\"authored\"",
			)
		}
	}

	if o.Body == BodyAuthored && o.Resource == "" {
		return fmt.Errorf(
			"authored body requires resource and resource_policy = " +
				"\"tenant\"; the generated gate binds the resource",
		)
	}

	for _, id := range o.Ports {
		if _, ok := ports[id]; !ok {
			return fmt.Errorf("references undeclared port %s", id)
		}

		if err := o.checkPortGroup(ports[id]); err != nil {
			return err
		}
	}

	if err := o.validateResource(); err != nil {
		return err
	}

	if err := o.validateModelRefs(models); err != nil {
		return err
	}

	return o.validateQuery()
}

func (o Operation) validateResource() error {
	if o.Resource == "" {
		if o.ResourcePolicy != "" {
			return fmt.Errorf("resource_policy requires resource")
		}

		return nil
	}

	if !resourceRef.MatchString(o.Resource) ||
		!strings.HasPrefix(o.Resource, o.Group+".") {
		return fmt.Errorf(
			"resource %q must be <group>.<name> in the operation group",
			o.Resource,
		)
	}

	if o.ResourcePolicy != "tenant" {
		return fmt.Errorf("resource requires resource_policy = \"tenant\"")
	}

	return nil
}

func (o Operation) validateModelRefs(models map[string]Model) error {
	for field, ref := range map[string]string{
		"item":   o.Item,
		"output": o.Output,
		"input":  o.Input,
	} {
		if ref == "" {
			continue
		}

		if _, ok := models[ref]; !ok {
			return fmt.Errorf("%s references undeclared model %s", field, ref)
		}
	}

	switch o.Response {
	case "":
		if o.Item != "" || o.Output != "" {
			return fmt.Errorf("item and output require a response")
		}
	case ResponsePage, ResponseStream:
		if o.Item == "" || o.Output != "" {
			return fmt.Errorf("response %s requires item only", o.Response)
		}
	case ResponseObject:
		if o.Output == "" || o.Item != "" {
			return fmt.Errorf("response object requires output only")
		}
	default:
		return fmt.Errorf("response must be page, object or stream")
	}

	if o.Response != "" && o.Body != BodyAuthored && o.Body != BodyGenerated &&
		o.Body != BodySystem {
		return fmt.Errorf("response requires an explicit body")
	}

	if o.Response == ResponsePage && !o.Collection {
		return fmt.Errorf("response page requires collection = true")
	}

	return o.validateStreamOperation()
}

func (o Operation) validateQuery() error {
	if len(o.Query) == 0 {
		return nil
	}

	if o.Write || o.Public && o.Scope != ScopeLookup {
		return fmt.Errorf("query parameters require a protected read")
	}

	types := map[string]string{}

	for _, param := range o.Query {
		if !snakeName.MatchString(param.Name) {
			return fmt.Errorf("query name %q must be snake_case", param.Name)
		}

		if _, dup := types[param.Name]; dup {
			return fmt.Errorf("duplicate query parameter %s", param.Name)
		}

		if !queryParamTypes[param.Type] {
			return fmt.Errorf(
				"query %s type must be string, int64, bool or time",
				param.Name,
			)
		}

		types[param.Name] = param.Type

		if err := param.validateLimit(); err != nil {
			return err
		}
	}

	for _, param := range o.Query {
		if param.After == "" {
			continue
		}

		if param.Type != "time" || param.After == param.Name ||
			types[param.After] != "time" {
			return fmt.Errorf(
				"query %s after must name a different time parameter",
				param.Name,
			)
		}
	}

	return nil
}

func (p OperationQuery) validateLimit() error {
	switch p.Type {
	case "string":
		if p.Max < 1 || p.Max > maxQueryString {
			return fmt.Errorf(
				"query %s requires max between 1 and %d",
				p.Name,
				maxQueryString,
			)
		}
	case "int64":
		if p.Max < 0 || p.Max > maxQueryInt {
			return fmt.Errorf(
				"query %s max must be between 0 and %d",
				p.Name,
				maxQueryInt,
			)
		}
	default:
		if p.Max != 0 {
			return fmt.Errorf("query %s cannot declare max", p.Name)
		}
	}

	return nil
}

func splitQualified(id string) (string, string, bool) {
	group, name, ok := strings.Cut(id, ".")

	return group, name, ok && group != "" && name != "" &&
		!strings.Contains(name, ".")
}
