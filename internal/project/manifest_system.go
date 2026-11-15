package project

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Tenant-less system operations. Everything else in a candidate application
// is tenant scoped; these two explicit shapes are the only ways to act before
// a tenant exists or before the caller has chosen one:
//
//   - scope = "provision": an authored write that creates a new tenant root
//     row and its first rows under a tenant id minted by generated code, in
//     one transaction, for a verified identity that has no membership yet.
//   - scope = "lookup": a generated, public, read-only resolution of one
//     tenant root row by a unique key, returning only declared columns.

const (
	ScopeProvision = "provision"
	ScopeLookup    = "lookup"

	maxLookupColumns = 16
)

// Lookup declares one narrow tenant-root read by unique key, for example
// resolving an organization by slug before login.
type Lookup struct {
	ID      string   `toml:"id"`
	Table   string   `toml:"table"`
	Key     string   `toml:"key"`
	Columns []string `toml:"columns"`

	// castText compares the key as text, for non-text key columns. It is
	// set only on lookups derived from machine principals.
	castText bool
}

// Group is the owning group of the lookup.
func (l Lookup) Group() string {
	group, _, _ := splitQualified(l.ID)

	return group
}

// Name is the exported name part of the lookup id.
func (l Lookup) Name() string {
	return modelName(l.ID)
}

// cast is the SQL suffix comparing the key as text when required.
func (l Lookup) cast() string {
	if l.castText {
		return "::text"
	}

	return ""
}

// Setting is the transaction-local setting that selects the one visible row.
func (l Lookup) Setting() string {
	return "app.lookup_" + strings.ToLower(l.Group()) + "_" +
		strings.ToLower(l.Name())
}

// PolicyName is the row-level security policy that admits the lookup.
func (l Lookup) PolicyName() string {
	return "pluto_lookup_" + strings.ToLower(l.Group()) + "_" +
		strings.ToLower(l.Name())
}

// ViewName is the security-invoker view exposing only the declared columns.
func (l Lookup) ViewName() string {
	return l.PolicyName()
}

// TenantRoot returns the declared tenant root table.
func (m Manifest) TenantRoot() (Table, bool) {
	for _, table := range m.Tables {
		if table.TenantRoot {
			return table, true
		}
	}

	return Table{}, false
}

// LookupByID returns one declared lookup.
func (m Manifest) LookupByID(id string) (Lookup, bool) {
	for _, lookup := range m.Lookups {
		if lookup.ID == id {
			return lookup, true
		}
	}

	return Lookup{}, false
}

// hasScope reports whether any operation uses the named system scope.
func (m Manifest) hasScope(scope string) bool {
	for _, operation := range m.Operations {
		if operation.Scope == scope {
			return true
		}
	}

	return false
}

// HasProvision tells the scope template to render the provisioning
// capability.
func (d templateData) HasProvision() bool {
	return d.Manifest.hasScope(ScopeProvision)
}

// usesSeamWire reports operations whose wire models and query parser come
// from the seam generator: authored bodies and generated system lookups.
func (o Operation) usesSeamWire() bool {
	return (o.Body == BodyAuthored || o.Body == BodySystem) &&
		o.Scope != ScopeEachTenant
}

// outsideContracts reports operations that have no resource behavior
// contract: authored and system bodies are governed by their own seams.
func (o Operation) outsideContracts() bool {
	return o.Body == BodyAuthored || o.Body == BodySystem
}

func validateTenantRoots(tables []Table) error {
	var root *Table

	for index := range tables {
		table := &tables[index]
		if !table.TenantRoot {
			continue
		}

		if root != nil {
			return fmt.Errorf(
				"tables %s and %s are both tenant_root; a project has "+
					"exactly one tenant root",
				root.ID(),
				table.ID(),
			)
		}

		if table.AppendOnly {
			return fmt.Errorf(
				"tenant root %s cannot be append_only",
				table.ID(),
			)
		}

		root = table
	}

	return nil
}

func validateSystem(
	m Manifest,
	tables map[string]Table,
	models map[string]Model,
) error {
	lookups := map[string]Lookup{}

	for _, lookup := range m.Lookups {
		if err := lookup.validate(tables, models, m); err != nil {
			return fmt.Errorf("lookup %s: %w", lookup.ID, err)
		}

		if _, dup := lookups[lookup.ID]; dup {
			return fmt.Errorf("duplicate lookup %s", lookup.ID)
		}

		lookups[lookup.ID] = lookup
	}

	if err := validateLookupShape(m); err != nil {
		return err
	}

	used := map[string]string{}

	for _, operation := range m.Operations {
		if operation.Scope != ScopeLookup {
			continue
		}

		if previous, taken := used[operation.Lookup]; taken {
			return fmt.Errorf(
				"operations %s and %s share lookup %s",
				previous,
				operation.ID(),
				operation.Lookup,
			)
		}

		used[operation.Lookup] = operation.ID()
	}

	for id := range lookups {
		if used[id] == "" {
			return fmt.Errorf(
				"lookup %s is not used by any scope = \"lookup\" operation",
				id,
			)
		}
	}

	if err := validateSystemEndpoints(m); err != nil {
		return err
	}

	if err := validateEachTenantJobs(m); err != nil {
		return err
	}

	if m.hasScope(ScopeProvision) || m.hasScope(ScopeLookup) {
		if _, ok := m.TenantRoot(); !ok {
			return fmt.Errorf(
				"system operations require one [[tables]] entry with " +
					"tenant_root = true",
			)
		}

		if !m.Capabilities.LocalCredentials {
			return fmt.Errorf(
				"system operations require local credentials: their rate " +
					"limit uses the shared attempt limiter",
			)
		}
	}

	return nil
}

func (l Lookup) validate(
	tables map[string]Table,
	models map[string]Model,
	m Manifest,
) error {
	group, name, ok := splitQualified(l.ID)
	if !ok || !packageName.MatchString(group) || !exportName.MatchString(name) {
		return fmt.Errorf("id must be <group>.<ExportedName>")
	}

	root, ok := tables[group+"."+l.Table]
	if !ok || !root.TenantRoot {
		return fmt.Errorf(
			"table %q must be the tenant_root table of group %s",
			l.Table,
			group,
		)
	}

	if !snakeName.MatchString(l.Key) || l.Key == root.TenantColumn {
		return fmt.Errorf(
			"key must be a snake_case column other than %s",
			root.TenantColumn,
		)
	}

	if len(l.Columns) == 0 || len(l.Columns) > maxLookupColumns ||
		!containsString(l.Columns, l.Key) {
		return fmt.Errorf(
			"columns requires 1 to %d declared columns including the key",
			maxLookupColumns,
		)
	}

	seen := map[string]bool{}

	for _, column := range l.Columns {
		if !snakeName.MatchString(column) || seen[column] {
			return fmt.Errorf(
				"column %q must be a distinct snake_case name",
				column,
			)
		}

		seen[column] = true
	}

	return nil
}

// lookupOutputTypes are the model field types a lookup column can scan into.
var lookupOutputTypes = map[string]bool{
	"string": true, "int64": true, "bool": true, "time": true,
}

// validateSystemSeams checks one scope = "provision", "lookup" or "enroll"
// operation.
func (o Operation) validateSystemSeams(
	m Manifest,
	ports map[string]Port,
	models map[string]Model,
) error {
	if _, _, err := ParseRateLimit(o.RateLimit); err != nil {
		return err
	}

	if len(o.Query) != 0 && o.Scope == ScopeProvision {
		return fmt.Errorf("a provision operation takes a JSON body, not query")
	}

	if o.Resource != "" || o.ResourcePolicy != "" || o.Permission != "" ||
		o.Collection || o.Item != "" {
		return fmt.Errorf(
			"scope %q takes no permission, resource, collection or item: "+
				"no tenant or membership exists yet",
			o.Scope,
		)
	}

	switch o.Scope {
	case ScopeProvision:
		return o.validateProvision(ports, models)
	case ScopeLookup:
		return o.validateLookup(models)
	case ScopeEnroll:
		if err := o.validateEnroll(m); err != nil {
			return err
		}

		return o.validateModelRefs(models)
	default:
		return fmt.Errorf("scope must be provision, lookup or enroll")
	}
}

func (o Operation) validateProvision(
	ports map[string]Port,
	models map[string]Model,
) error {
	if o.Body != BodyAuthored || !o.Write || o.Public ||
		o.Idempotency != "exempt" || strings.TrimSpace(o.ReplayReason) == "" {
		return fmt.Errorf(
			"provision requires body = \"authored\", write = true, a " +
				"protected (not public) operation, idempotency = " +
				"\"exempt\" and a replay_reason",
		)
	}

	if o.Response != ResponseObject || o.Output == "" || o.Input == "" {
		return fmt.Errorf(
			"provision requires response = \"object\", an output model " +
				"and an input model",
		)
	}

	if len(o.Ports) == 0 {
		return fmt.Errorf("provision requires at least one port")
	}

	for _, id := range o.Ports {
		if _, ok := ports[id]; !ok {
			return fmt.Errorf("references undeclared port %s", id)
		}

		if group, _, _ := splitQualified(id); group != o.Group {
			return fmt.Errorf("port %s belongs to another group", id)
		}
	}

	return o.validateModelRefs(models)
}

func (o Operation) validateLookup(models map[string]Model) error {
	if o.Body != BodySystem || o.Write || !o.Public ||
		o.Idempotency != "none" || len(o.Ports) != 0 || o.Input != "" {
		return fmt.Errorf(
			"lookup requires body = \"system\", a public read with " +
				"idempotency = \"none\", no ports and no input",
		)
	}

	if o.Response != ResponseObject || o.Output == "" {
		return fmt.Errorf("lookup requires response = \"object\" and output")
	}

	if err := o.validateModelRefs(models); err != nil {
		return err
	}

	return o.validateQuery()
}

// validateLookupShape ties a lookup operation to its declaration and its
// output model once both are known.
func validateLookupShape(m Manifest) error {
	models := map[string]Model{}
	for _, model := range m.Models {
		models[model.ID] = model
	}

	for _, operation := range m.Operations {
		if operation.Scope != ScopeLookup {
			continue
		}

		lookup, ok := m.LookupByID(operation.Lookup)
		if !ok || lookup.Group() != operation.Group {
			return fmt.Errorf(
				"operation %s references undeclared lookup %q in its group",
				operation.ID(),
				operation.Lookup,
			)
		}

		if len(operation.Query) != 1 ||
			operation.Query[0].Name != lookup.Key ||
			operation.Query[0].Type != "string" {
			return fmt.Errorf(
				"operation %s must declare exactly one string query "+
					"parameter named %s",
				operation.ID(),
				lookup.Key,
			)
		}

		output := models[operation.Output]
		if len(output.Fields) != len(lookup.Columns) {
			return fmt.Errorf(
				"operation %s output model must have exactly the lookup "+
					"columns %v",
				operation.ID(),
				lookup.Columns,
			)
		}

		for index, field := range output.Fields {
			match := modelField.FindStringSubmatch(field)
			if match == nil || match[1] != lookup.Columns[index] ||
				!lookupOutputTypes[match[2]] {
				return fmt.Errorf(
					"operation %s output field %q must be %s with type "+
						"string, int64, bool or time",
					operation.ID(),
					field,
					lookup.Columns[index],
				)
			}
		}
	}

	return nil
}

// ParseRateLimit reads "<attempts>/<window>", for example "5/1h".
func ParseRateLimit(spec string) (int, time.Duration, error) {
	count, window, ok := strings.Cut(spec, "/")
	if !ok {
		return 0, 0, fmt.Errorf(
			"rate_limit must be <attempts>/<window>, for example 5/1h",
		)
	}

	attempts, err := strconv.Atoi(count)
	if err != nil || attempts < 1 || attempts > 10000 {
		return 0, 0, fmt.Errorf("rate_limit attempts must be 1 to 10000")
	}

	duration, err := time.ParseDuration(window)
	if err != nil || duration < time.Second || duration > 24*time.Hour {
		return 0, 0, fmt.Errorf("rate_limit window must be 1s to 24h")
	}

	return attempts, duration, nil
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}
