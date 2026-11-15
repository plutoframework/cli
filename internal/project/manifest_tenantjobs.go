package project

import (
	"fmt"
	"strings"
	"time"
)

// Tenant-iterating jobs. A scope = "each_tenant" operation is an authored
// body that runs once per tenant, on a schedule, without any request, caller
// or tenant input: generated code enumerates tenant ids from a narrow index
// of the tenant root table, enqueues one pluto-jobs delivery per tenant, and
// runs the body in a system execution bound to exactly that tenant. The
// service identity is a configured credential; its authority is the declared
// service role, checked by the same identity gate and RBAC as any request.

const (
	ScopeEachTenant = "each_tenant"

	scheduleTriggered = "triggered"
	schedulePrefix    = "every "

	minTenantJobInterval = 5 * time.Second
	maxTenantJobInterval = 24 * time.Hour
	maxTenantJobTimeout  = 5 * time.Minute
	maxTenantJobWorkers  = 32

	defaultTenantJobTimeout     = 30 * time.Second
	defaultTenantJobConcurrency = 4
	defaultTenantJobAttempts    = 3

	// tenantIndexPolicy, tenantIndexView and tenantIndexSetting name the one
	// generated, narrow enumeration of the tenant root table.
	tenantIndexPolicy  = "pluto_tenant_index"
	tenantIndexView    = "pluto_tenant_index"
	tenantIndexSetting = "app.tenant_index"
)

// ParseSchedule reads "every <duration>" (5s to 24h) or "triggered". A
// triggered job has no scheduler loop; its interval is zero.
func ParseSchedule(spec string) (time.Duration, error) {
	if spec == scheduleTriggered {
		return 0, nil
	}

	value, ok := strings.CutPrefix(spec, schedulePrefix)
	if !ok {
		return 0, fmt.Errorf(
			"schedule must be \"every <duration>\", for example " +
				"\"every 30s\", or \"triggered\"",
		)
	}

	interval, err := time.ParseDuration(value)
	if err != nil || interval < minTenantJobInterval ||
		interval > maxTenantJobInterval {
		return 0, fmt.Errorf("schedule interval must be 5s to 24h")
	}

	return interval, nil
}

// Interval is the scheduler period; zero means triggered.
func (o Operation) Interval() time.Duration {
	interval, _ := ParseSchedule(o.Schedule)

	return interval
}

// TenantTimeout bounds one tenant's execution.
func (o Operation) TenantTimeout() time.Duration {
	if o.Timeout == "" {
		return defaultTenantJobTimeout
	}

	timeout, err := time.ParseDuration(o.Timeout)
	if err != nil {
		return defaultTenantJobTimeout
	}

	return timeout
}

// TenantConcurrency bounds simultaneous tenant executions of one job.
func (o Operation) TenantConcurrency() int {
	if o.Concurrency == 0 {
		return defaultTenantJobConcurrency
	}

	return o.Concurrency
}

// TenantAttempts is the finite pluto-jobs delivery budget per tenant run.
func (o Operation) TenantAttempts() int {
	if o.MaxAttempts == 0 {
		return defaultTenantJobAttempts
	}

	return o.MaxAttempts
}

// JobKind is the pluto-jobs kind of the per-tenant deliveries.
func (o Operation) JobKind() string {
	return "tenant." + o.Group + "." + strings.ToLower(o.Action)
}

// HasEachTenant reports whether any operation iterates tenants; templates use
// it to start the schedulers.
func (m Manifest) HasEachTenant() bool {
	return m.hasEachTenant()
}

// hasEachTenant reports whether any operation iterates tenants.
func (m Manifest) hasEachTenant() bool {
	return m.hasScope(ScopeEachTenant)
}

func (o Operation) validateEachTenant(ports map[string]Port) error {
	if o.Body != BodyAuthored || o.Public ||
		o.Permission == "" || o.ServiceRole == "" {
		return fmt.Errorf(
			"each_tenant requires body = \"authored\", a protected " +
				"operation, a permission and a service_role",
		)
	}

	if o.Resource == "" {
		return fmt.Errorf(
			"each_tenant requires resource and resource_policy = " +
				"\"tenant\"; the generated gate binds the resource",
		)
	}

	if err := o.validateResource(); err != nil {
		return err
	}

	if o.Response != "" || o.Item != "" || o.Output != "" || o.Input != "" ||
		len(o.Query) != 0 || o.Collection || o.Lookup != "" ||
		o.RateLimit != "" {
		return fmt.Errorf(
			"each_tenant takes no request or response models, query, " +
				"collection, lookup or rate_limit: it has no caller",
		)
	}

	if o.Write && (o.Idempotency != "exempt" ||
		strings.TrimSpace(o.ReplayReason) == "") {
		return fmt.Errorf(
			"a writing each_tenant job runs at least once per tenant and " +
				"slot: declare idempotency = \"exempt\" and a replay_reason " +
				"stating why a repeated run is harmless",
		)
	}

	if _, err := ParseSchedule(o.Schedule); err != nil {
		return err
	}

	if err := o.validateTenantLimits(); err != nil {
		return err
	}

	for _, id := range o.Ports {
		if _, ok := ports[id]; !ok {
			return fmt.Errorf("references undeclared port %s", id)
		}

		if foreignPort(o.Group, id) {
			return fmt.Errorf(
				"each_tenant ports must belong to its own group: %s",
				id,
			)
		}
	}

	return nil
}

func (o Operation) validateTenantLimits() error {
	if o.Timeout != "" {
		timeout, err := time.ParseDuration(o.Timeout)
		if err != nil || timeout < time.Second ||
			timeout > maxTenantJobTimeout {
			return fmt.Errorf("timeout must be 1s to 5m")
		}
	}

	if o.Concurrency < 0 || o.Concurrency > maxTenantJobWorkers {
		return fmt.Errorf("concurrency must be 1 to %d", maxTenantJobWorkers)
	}

	if o.MaxAttempts < 0 || o.MaxAttempts > 100 {
		return fmt.Errorf("max_attempts must be 1 to 100")
	}

	if interval := o.Interval(); interval != 0 &&
		o.TenantTimeout() > interval*4 {
		return fmt.Errorf(
			"timeout must not exceed four schedule intervals: slow tenants " +
				"would pile up",
		)
	}

	return nil
}

// validateEachTenantJobs checks what needs the whole manifest: a tenant root
// to enumerate, a declared service role that grants the permission, no HTTP
// entry, and unique job kinds.
func validateEachTenantJobs(m Manifest) error {
	if !m.hasEachTenant() {
		return nil
	}

	if _, ok := m.TenantRoot(); !ok {
		return fmt.Errorf(
			"each_tenant jobs require one [[tables]] entry with " +
				"tenant_root = true",
		)
	}

	roles := map[string]RoleContract{}
	for _, role := range m.Roles {
		roles[role.ID] = role
	}

	kinds := map[string]string{}

	for _, operation := range m.Operations {
		if operation.Scope != ScopeEachTenant {
			continue
		}

		role, ok := roles[operation.ServiceRole]
		if !ok || !containsString(role.Permissions, operation.Permission) {
			return fmt.Errorf(
				"operation %s: service_role %q must be a declared role that "+
					"directly grants permission %q",
				operation.ID(),
				operation.ServiceRole,
				operation.Permission,
			)
		}

		for _, endpoint := range m.Endpoints {
			if endpoint.Operation == operation.ID() {
				return fmt.Errorf(
					"operation %s is a tenant job and has no HTTP entry",
					operation.ID(),
				)
			}
		}

		for _, job := range m.Jobs {
			if job.Kind == operation.JobKind() {
				return fmt.Errorf(
					"operation %s: job kind %s is already declared",
					operation.ID(),
					job.Kind,
				)
			}
		}

		if prior, dup := kinds[operation.JobKind()]; dup {
			return fmt.Errorf(
				"operations %s and %s share job kind %s",
				prior,
				operation.ID(),
				operation.JobKind(),
			)
		}

		kinds[operation.JobKind()] = operation.ID()
	}

	return nil
}

// tenantJobFile reports whether path is a generated file that legitimately
// handles pluto-jobs deliveries and requests: the gate of a declared tenant
// job, or the sealed scope package, whose Tx.Enqueue is bound to declared
// job kinds. Callers also require the file to be managed, so a hand-edited
// copy is rejected by the integrity check instead.
func (m Manifest) tenantJobFile(path string) bool {
	if path == scopeFile {
		return true
	}

	for _, operation := range m.Operations {
		if operation.Scope == ScopeEachTenant &&
			path == "internal/operations/"+operation.Group+"/"+
				lowerAction(operation.Action)+"_gate_gen.go" {
			return true
		}
	}

	return false
}
