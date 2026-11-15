package project

import (
	"bytes"
	"fmt"
	"strconv"
	"time"
)

// writeEachTenantOperation wires one tenant job: a service authorizer whose
// only role is the declared service role, the gate over the generated tenant
// index and the job queue. The authorizer is used by this gate alone, whose
// identity can only come from the configured service credential.
func writeEachTenantOperation(
	source *bytes.Buffer,
	operation Operation,
	index int,
) {
	suffix := fmt.Sprint(index)

	var ports bytes.Buffer

	for _, id := range operation.Ports {
		fmt.Fprintf(
			&ports,
			"%s: adapters.%s, ",
			modelName(id),
			adapterField(Port{ID: id}),
		)
	}

	fmt.Fprintf(
		source,
		`
	serviceAuthorizer%[1]s, err := rbac.NewAuthorizer(
		policy,
		rbac.RoleResolverFunc(
			func(
				context.Context,
				string,
				rbac.Scope,
			) ([]rbac.RoleID, error) {
				return []rbac.RoleID{rbac.RoleID(%[2]s)}, nil
			}),
		rbac.Options{RequireTenant: true})
	if err != nil {
		return nil, err
	}

	requirement%[1]s, err := serviceAuthorizer%[1]s.Require(%[3]s)
	if err != nil {
		return nil, err
	}

	resource%[1]s, err := requirement%[1]s.WithResourcePolicy(
		candidateTenantResource(%[4]s),
	)
	if err != nil {
		return nil, err
	}

	tenants%[1]s := tenantindex.New(pool)

	gate%[1]s, err := identitygate.New(
		authentication.Manager,
		resource%[1]s,
		tenants%[1]s,
	)
	if err != nil {
		return nil, err
	}

	queue%[1]s, err := tenantindex.NewQueue(pool)
	if err != nil {
		return nil, err
	}

	runtime.Operations.%[5]s = %[6]soperations.New%[7]s(
		gate%[1]s,
		authentication.Manager,
		tenants%[1]s,
		queue%[1]s,
		%[6]slogic.%[7]sPorts{%[8]s},
	)
`,
		suffix,
		strconv.Quote(operation.ServiceRole),
		strconv.Quote(operation.Permission),
		strconv.Quote(operation.Resource),
		symbol(operation.Endpoint()),
		operation.Group,
		operation.Action,
		ports.String(),
	)
}

// writeEachTenantHandlers registers every tenant job delivery handler and
// sizes the worker for the slowest declared tenant timeout.
func writeEachTenantHandlers(source *bytes.Buffer, m Manifest) {
	longest := defaultTenantJobTimeout

	for _, operation := range m.Operations {
		if operation.Scope != ScopeEachTenant {
			continue
		}

		if timeout := operation.TenantTimeout(); timeout > longest {
			longest = timeout
		}

		fmt.Fprintf(
			source,
			`
	tenantJob%[1]s, err := r.Operations.%[1]s.Handler(credentials.%[1]s)
	if err != nil {
		return nil, err
	}

	handlers[%[2]soperations.%[3]sKind] = tenantJob%[1]s
`,
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
		)
	}

	if longest > defaultTenantJobTimeout {
		fmt.Fprintf(
			source,
			`
	workerConfig.HandlerTimeout = time.Duration(%d)
	workerConfig.LeaseDuration = workerConfig.HandlerTimeout + time.Minute
`,
			int64(longest),
		)
	}
}

// writeEachTenantSchedule renders the scheduler: one loop per scheduled tenant
// job, each tick enumerating tenants through the generated index.
func writeEachTenantSchedule(source *bytes.Buffer, m Manifest) {
	if !m.hasEachTenant() {
		return
	}

	source.WriteString(`
// Schedule runs the tenant job schedulers until ctx ends. A job with
// schedule = "triggered" has no loop; call its Enqueue on the runtime's
// operations instead.
func (r *CandidateRuntime) Schedule(ctx context.Context) error {
	if r == nil {
		return errors.New("nil runtime")
	}

	var group sync.WaitGroup
`)

	for _, operation := range m.Operations {
		if operation.Scope != ScopeEachTenant ||
			operation.Interval() == time.Duration(0) {
			continue
		}

		fmt.Fprintf(
			source,
			`
	group.Add(1)

	go func() {
		defer group.Done()

		r.scheduleEvery(
			ctx,
			%[1]soperations.%[2]sInterval,
			%[3]s,
			r.Operations.%[4]s.Enqueue,
		)
	}()
`,
			operation.Group,
			operation.Action,
			strconv.Quote(operation.ID()),
			symbol(operation.Endpoint()),
		)
	}

	source.WriteString(`
	group.Wait()

	return ctx.Err()
}

// scheduleEvery runs one tick immediately and then every interval. A failed
// tick is logged without its error text, which can carry data, and retried on
// the next tick.
func (r *CandidateRuntime) scheduleEvery(
	ctx context.Context,
	interval time.Duration,
	id string,
	tick func(context.Context) error,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		if err := tick(ctx); err != nil && ctx.Err() == nil {
			r.candidateRuntimeLogger.Error(
				"tenant job enqueue failed",
				"job", id)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
`)
}
