package project

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Machine identity. Gateways and other non-human callers are not users: they
// enroll once with a single-use token and afterwards authenticate every
// request with an ed25519 signature. A declared principal binds that
// lifecycle to an app-owned table; generated code resolves the tenant from
// the machine row, never from request input.
//
//   - [[principals]] kind = "machine" declares the table and its columns.
//   - scope = "enroll" is the public, generated enrollment operation.
//   - auth = "machine:<principal>" marks an authored operation whose gate
//     verifies the signature and yields a scope.Execution for the machine.
//
// Machine permissions are declared on the principal, never on user roles.

const (

	// ScopeEnroll is the public enrollment operation of a principal.
	ScopeEnroll = "enroll"

	// ScopeMachine is the endpoint scope of a machine-authenticated
	// operation: not a user session, no user permission.
	ScopeMachine = "machine"

	// PrincipalKindMachine is the only principal kind.
	PrincipalKindMachine = "machine"

	machineAuthPrefix = "machine:"

	defaultEnrollTTL    = 24 * time.Hour
	maxEnrollTTL        = 7 * 24 * time.Hour
	defaultReplayWindow = 2 * time.Minute
	maxReplayWindow     = 10 * time.Minute
	maxMachinePerms     = 32

	defaultRevokedValue = "revoked"
	defaultRequestLimit = 600
	maxRequestLimit     = 100000
)

var machinePermission = regexp.MustCompile(
	`^[a-z][a-z0-9_]*(?:\.[a-z][a-z0-9_]*)+$`,
)

// Principal declares one machine identity kind.
type Principal struct {
	ID          string `toml:"id"`
	Kind        string `toml:"kind"`
	Table       string `toml:"table"`
	IDColumn    string `toml:"id_column"`
	KeyColumn   string `toml:"key_column"`
	StateColumn string `toml:"state_column"`

	// PendingValue is the state a machine row has until it enrolls;
	// ActiveValue is the only state allowed to authenticate. Any other
	// state, for example revoked, denies on the next request.
	PendingValue string `toml:"pending_value"`
	ActiveValue  string `toml:"active_value"`

	// RevokedValue is the state generated revocation writes; it defaults to
	// "revoked". Only generated code writes the key and state columns.
	RevokedValue string `toml:"revoked_value,omitempty"`

	// Permissions are the machine capabilities this principal may be
	// granted by operations. They are separate from user roles.
	Permissions []string `toml:"permissions"`

	EnrollTTL    string `toml:"enroll_ttl,omitempty"`
	ReplayWindow string `toml:"replay_window,omitempty"`

	// RequestLimit caps the verified requests one machine may make within
	// two replay windows (the nonce lifetime); the count is shared by every
	// replica because it reads the nonce table. Default 600.
	RequestLimit int `toml:"request_limit,omitempty"`
}

// Revoked is the state written by generated revocation.
func (p Principal) Revoked() string {
	if p.RevokedValue == "" {
		return defaultRevokedValue
	}

	return p.RevokedValue
}

// Group is the owning group of the principal.
func (p Principal) Group() string {
	group, _, _ := splitQualified(p.ID)

	return group
}

// Name is the exported name part of the principal id.
func (p Principal) Name() string {
	return modelName(p.ID)
}

// Lookup is the derived pre-tenant lookup of the machine row by id: a
// security-invoker view of four columns and a one-row select policy.
func (p Principal) Lookup(tenantColumn string) Lookup {
	return Lookup{
		ID:    p.Group() + "." + p.Name() + "Machine",
		Table: p.Table,
		Key:   p.IDColumn,
		Columns: []string{
			p.IDColumn,
			tenantColumn,
			p.KeyColumn,
			p.StateColumn,
		},
		castText: true,
	}
}

// TTL is the enrollment token lifetime.
func (p Principal) TTL() time.Duration {
	value, err := time.ParseDuration(p.EnrollTTL)
	if err != nil || p.EnrollTTL == "" {
		return defaultEnrollTTL
	}

	return value
}

// Limit is the verified-request cap per nonce lifetime.
func (p Principal) Limit() int {
	if p.RequestLimit == 0 {
		return defaultRequestLimit
	}

	return p.RequestLimit
}

// Window is the accepted clock skew and nonce lifetime base.
func (p Principal) Window() time.Duration {
	value, err := time.ParseDuration(p.ReplayWindow)
	if err != nil || p.ReplayWindow == "" {
		return defaultReplayWindow
	}

	return value
}

// PrincipalByID returns one declared principal.
func (m Manifest) PrincipalByID(id string) (Principal, bool) {
	for _, principal := range m.Principals {
		if principal.ID == id {
			return principal, true
		}
	}

	return Principal{}, false
}

// HasMachine tells templates to render the machine identity capability.
func (m Manifest) HasMachine() bool {
	return len(m.Principals) != 0
}

// HasMachine tells the scope template to render machine executions.
func (d templateData) HasMachine() bool {
	return d.Manifest.HasMachine()
}

// MachinePrincipal returns the principal id of a machine-authenticated
// operation, or empty.
func (o Operation) MachinePrincipal() string {
	return strings.TrimPrefix(machineAuthOnly(o.Auth), machineAuthPrefix)
}

func machineAuthOnly(auth string) string {
	if strings.HasPrefix(auth, machineAuthPrefix) {
		return auth
	}

	return ""
}

// isMachine reports a machine-authenticated operation.
func (o Operation) isMachine() bool {
	return o.Auth != ""
}

// allLookups are the declared tenant-root lookups plus the machine lookups
// derived from principals; both share one policy and view shape.
func (m Manifest) allLookups() []Lookup {
	lookups := append([]Lookup(nil), m.Lookups...)

	for _, principal := range m.Principals {
		column := ""

		for _, table := range m.Tables {
			if table.Group == principal.Group() &&
				table.Name == principal.Table {
				column = table.TenantColumn
			}
		}

		lookups = append(lookups, principal.Lookup(column))
	}

	return lookups
}

func validatePrincipals(m Manifest, tables map[string]Table) error {
	seen := map[string]bool{}
	enrolled := map[string]string{}

	for _, principal := range m.Principals {
		if err := principal.validate(m, tables); err != nil {
			return fmt.Errorf("principal %s: %w", principal.ID, err)
		}

		if seen[principal.ID] {
			return fmt.Errorf("duplicate principal %s", principal.ID)
		}

		seen[principal.ID] = true
	}

	for _, operation := range m.Operations {
		if operation.Scope != ScopeEnroll {
			continue
		}

		if previous, taken := enrolled[operation.Principal]; taken {
			return fmt.Errorf(
				"operations %s and %s both enroll principal %s",
				previous,
				operation.ID(),
				operation.Principal,
			)
		}

		enrolled[operation.Principal] = operation.ID()
	}

	for id := range seen {
		if enrolled[id] == "" {
			return fmt.Errorf(
				"principal %s has no scope = \"enroll\" operation; "+
					"machines could never activate",
				id,
			)
		}
	}

	return nil
}

func (p Principal) validate(m Manifest, tables map[string]Table) error {
	group, name, ok := splitQualified(p.ID)
	if !ok || !packageName.MatchString(group) || !exportName.MatchString(name) {
		return fmt.Errorf("id must be <group>.<ExportedName>")
	}

	if p.Kind != PrincipalKindMachine {
		return fmt.Errorf("kind must be %q", PrincipalKindMachine)
	}

	table, ok := tables[group+"."+p.Table]
	if !ok || table.TenantRoot {
		return fmt.Errorf(
			"table %q must be a declared non-root table of group %s",
			p.Table,
			group,
		)
	}

	columns := map[string]bool{table.TenantColumn: true}

	for _, column := range []string{p.IDColumn, p.KeyColumn, p.StateColumn} {
		if !snakeName.MatchString(column) || columns[column] {
			return fmt.Errorf(
				"id_column, key_column and state_column must be distinct " +
					"snake_case columns other than the tenant column",
			)
		}

		columns[column] = true
	}

	if !activeValue.MatchString(p.PendingValue) ||
		!activeValue.MatchString(p.ActiveValue) ||
		!activeValue.MatchString(p.Revoked()) ||
		p.PendingValue == p.ActiveValue ||
		p.Revoked() == p.ActiveValue || p.Revoked() == p.PendingValue {
		return fmt.Errorf(
			"pending_value, active_value and revoked_value must be distinct " +
				"1-64 " +
				"character values of letters, digits, '_', '-' or '.'",
		)
	}

	if p.RequestLimit < 0 || p.RequestLimit > maxRequestLimit {
		return fmt.Errorf("request_limit must be 1 to %d", maxRequestLimit)
	}

	if err := p.validateTimes(); err != nil {
		return err
	}

	return p.validatePermissions(m)
}

func (p Principal) validateTimes() error {
	if p.EnrollTTL != "" {
		value, err := time.ParseDuration(p.EnrollTTL)
		if err != nil || value < time.Minute || value > maxEnrollTTL {
			return fmt.Errorf("enroll_ttl must be 1m to 168h")
		}
	}

	if p.ReplayWindow != "" {
		value, err := time.ParseDuration(p.ReplayWindow)
		if err != nil || value < 5*time.Second || value > maxReplayWindow {
			return fmt.Errorf("replay_window must be 5s to 10m")
		}
	}

	return nil
}

func (p Principal) validatePermissions(m Manifest) error {
	if len(p.Permissions) == 0 || len(p.Permissions) > maxMachinePerms {
		return fmt.Errorf(
			"permissions requires 1 to %d machine permissions",
			maxMachinePerms,
		)
	}

	user := map[string]bool{}

	for _, role := range m.Roles {
		for _, permission := range role.Permissions {
			user[permission] = true
		}
	}

	for _, operation := range m.Operations {
		if !operation.isMachine() && operation.Permission != "" {
			user[operation.Permission] = true
		}
	}

	seen := map[string]bool{}

	for _, permission := range p.Permissions {
		if !machinePermission.MatchString(permission) || seen[permission] {
			return fmt.Errorf(
				"permission %q must be a distinct dotted lowercase id",
				permission,
			)
		}

		if user[permission] {
			return fmt.Errorf(
				"permission %q is also a user permission; machine "+
					"permissions are declared separately from user roles",
				permission,
			)
		}

		seen[permission] = true
	}

	return nil
}

// validateEnroll checks one scope = "enroll" operation.
func (o Operation) validateEnroll(m Manifest) error {
	principal, ok := m.PrincipalByID(o.Principal)
	if !ok || principal.Group() != o.Group {
		return fmt.Errorf(
			"principal %q must be a declared principal of group %s",
			o.Principal,
			o.Group,
		)
	}

	if o.Body != BodySystem || !o.Write || !o.Public ||
		o.Idempotency != "exempt" || strings.TrimSpace(o.ReplayReason) == "" ||
		len(o.Ports) != 0 || len(o.Query) != 0 {
		return fmt.Errorf(
			"enroll requires body = \"system\", a public write with " +
				"idempotency = \"exempt\", a replay_reason, no ports and " +
				"no query",
		)
	}

	if o.Response != ResponseObject || o.Output == "" || o.Input == "" {
		return fmt.Errorf(
			"enroll requires response = \"object\", an input model with " +
				"fields token:string and public_key:string, and an " +
				"output model with field machine_id:string",
		)
	}

	return o.checkEnrollModels(m)
}

func (o Operation) checkEnrollModels(m Manifest) error {
	want := map[string][]string{
		o.Input:  {"token:string", "public_key:string"},
		o.Output: {"machine_id:string"},
	}

	for _, model := range m.Models {
		fields, ok := want[model.ID]
		if !ok {
			continue
		}

		if !slices.Equal(model.Fields, fields) {
			return fmt.Errorf(
				"model %s must have exactly the fields %v",
				model.ID,
				fields,
			)
		}

		delete(want, model.ID)
	}

	for id := range want {
		return fmt.Errorf("references undeclared model %s", id)
	}

	return nil
}

// validateMachineOperation checks one auth = "machine:<principal>" operation.
func (o Operation) validateMachineOperation(
	m Manifest,
	ports map[string]Port,
	models map[string]Model,
) error {
	principal, ok := m.PrincipalByID(o.MachinePrincipal())
	if !ok || machineAuthOnly(o.Auth) == "" {
		return fmt.Errorf(
			"auth must be machine:<principal> naming a declared principal",
		)
	}

	if o.Body != BodyAuthored || o.Public || o.Collection ||
		o.Scope != "" || o.Lookup != "" || o.RateLimit != "" ||
		o.Response != ResponseObject || o.Output == "" || o.Item != "" {
		return fmt.Errorf(
			"a machine operation requires body = \"authored\", a " +
				"protected non-collection operation and response = " +
				"\"object\" with an output model",
		)
	}

	if !slices.Contains(principal.Permissions, o.Permission) {
		return fmt.Errorf(
			"permission %q is not a machine permission of principal %s",
			o.Permission,
			principal.ID,
		)
	}

	if o.Write && (o.Input == "" || len(o.Query) != 0) ||
		!o.Write && o.Input != "" {
		return fmt.Errorf(
			"a machine write takes a JSON input model; a machine read " +
				"takes declared query parameters and no input",
		)
	}

	if o.Resource == "" || o.ResourcePolicy != "tenant" {
		return fmt.Errorf(
			"a machine operation requires resource and resource_policy = " +
				"\"tenant\"",
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

// validateMachineEndpoints requires every machine operation to be exposed by
// exactly one endpoint whose method matches the operation.
func validateMachineEndpoints(m Manifest) error {
	for _, operation := range m.Operations {
		if !operation.isMachine() {
			continue
		}

		method := "GET"
		if operation.Write {
			method = "POST"
		}

		count := 0

		for _, endpoint := range m.Endpoints {
			if endpoint.Operation != operation.ID() {
				continue
			}

			count++

			if endpoint.Method != method {
				return fmt.Errorf(
					"operation %s endpoint must use %s",
					operation.ID(),
					method,
				)
			}
		}

		if count != 1 {
			return fmt.Errorf(
				"operation %s requires exactly one endpoint",
				operation.ID(),
			)
		}
	}

	return nil
}

// validateMachineSystem ties machine declarations to the profile: machines
// need multi tenancy and the shared attempt limiter that enrollment uses.
func validateMachineSystem(m Manifest) error {
	if !m.HasMachine() {
		for _, operation := range m.Operations {
			if operation.isMachine() || operation.Scope == ScopeEnroll {
				return fmt.Errorf(
					"operation %s requires a declared [[principals]] entry",
					operation.ID(),
				)
			}
		}

		return nil
	}

	if m.Capabilities.Tenancy != "multi" || !m.Capabilities.LocalCredentials {
		return fmt.Errorf(
			"machine principals require multi tenancy and local " +
				"credentials: enrollment uses the shared attempt limiter",
		)
	}

	return validateMachineEndpoints(m)
}

// protectedColumns lists the columns of a table that only generated machine
// code may write: the key and state of the principal bound to it.
func (m Manifest) protectedColumns(table Table) []string {
	var columns []string

	for _, principal := range m.Principals {
		if principal.Group() == table.Group && principal.Table == table.Name {
			columns = append(columns, principal.KeyColumn, principal.StateColumn)
		}
	}

	return columns
}
