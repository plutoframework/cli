package project

import (
	"bytes"
	"fmt"
	"go/format"
	"sort"
	"strconv"
)

func candidateRuntime(m Manifest) ([]byte, error) {
	var source bytes.Buffer

	q := strconv.Quote

	fmt.Fprintf(
		&source,
		`package app

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
"github.com/go-chi/chi/v5"
"github.com/jackc/pgx/v5/pgxpool"
auth "github.com/plutoframework/pluto-auth"
"github.com/plutoframework/pluto-core/cache"
identitygate "github.com/plutoframework/pluto-rbac/identity"
jobs "github.com/plutoframework/pluto-jobs"
jobspostgres "github.com/plutoframework/pluto-jobs/stores/postgres"
`,
	)

	if m.hasScope(ScopeProvision) || m.hasScope(ScopeLookup) {
		source.WriteString("\t\"time\"\n")
	}

	local := m.Capabilities.LocalCredentials
	internalImports := []string{
		"access",
		"config",
		"transport",
		"features",
		"storage/identity",
		"paths",
	}

	if local {
		internalImports = append(internalImports, "auth/credentials")
	} else {
		fmt.Fprintf(
			&source,
			`authpostgres "github.com/plutoframework/pluto-auth/stores/postgres"
`,
		)
	}

	if m.AuthEventsOn() {
		operation, _ := m.Operation(m.Auth.Events.Operation)
		fmt.Fprintf(
			&source,
			"\tauthEventModels %s\n",
			q(m.Project.Module+"/internal/models/"+operation.Group),
		)
	}

	for _, name := range internalImports {
		fmt.Fprintf(&source, `	%s
`, q(m.Project.Module+"/internal/"+name))
	}

	groups := map[string]bool{}
	resources := map[string]ResourceContract{}
	contracts := map[string]OperationContract{}

	usedResources := map[string]bool{}
	for _, contract := range m.OperationContracts {
		usedResources[contract.Resource] = true
	}

	for _, resource := range m.Resources {
		resources[resource.ID()] = resource
		if usedResources[resource.ID()] {
			groups[resource.Group] = true
		}
	}

	sorted := []string{}
	for group := range groups {
		sorted = append(sorted, group)
	}

	sort.Strings(sorted)

	for _, group := range sorted {
		fmt.Fprintf(
			&source,
			`%sdomain %s
%sdata %s
`,
			group,
			q(m.Project.Module+"/internal/domain/"+group),
			group,
			q(m.Project.Module+"/internal/storage/"+group),
		)
	}

	opGroups := map[string]bool{}
	effectGroups := map[string]bool{}
	handlerGroups := map[string]bool{}
	jobGroups := map[string]bool{}

	for _, operation := range m.Operations {
		opGroups[operation.Group] = true
	}

	for _, contract := range m.OperationContracts {
		contracts[contract.ID] = contract
		if contract.Kind == "effect" {
			effectGroups[resources[contract.Resource].Group] = true
		}
	}

	for _, entry := range m.Endpoints {
		handlerGroups[entry.Group] = true
	}

	for _, job := range m.Jobs {
		jobGroups[job.Group] = true
	}

	for _, item := range []struct {
		set         map[string]bool
		alias, path string
	}{
		{opGroups, "operations", "operations"},
		{effectGroups, "effects", "infrastructure"},
		{handlerGroups, "handlers", "handlers"},
		{jobGroups, "jobs", "jobs"},
	} {
		names := []string{}
		for group := range item.set {
			names = append(names, group)
		}

		sort.Strings(names)

		for _, group := range names {
			fmt.Fprintf(
				&source,
				`%s%s %s
`,
				group,
				item.alias,
				q(m.Project.Module+"/internal/"+item.path+"/"+group),
			)
		}
	}

	authoredGroups := map[string]bool{}
	lookupGroups := map[string]bool{}

	for _, operation := range m.Operations {
		if operation.isAuthored() {
			authoredGroups[operation.Group] = true
		}

		if operation.Scope == ScopeLookup {
			lookupGroups[operation.Group] = true
		}
	}

	for group := range lookupGroups {
		fmt.Fprintf(
			&source,
			`%slookup %s
`,
			group,
			q(m.Project.Module+"/internal/storage/"+group),
		)
	}

	if len(authoredGroups) > 0 {
		source.WriteString(
			`rbac "github.com/plutoframework/pluto-rbac"
`,
		)

		names := []string{}
		for group := range authoredGroups {
			names = append(names, group)
		}

		sort.Strings(names)

		for _, group := range names {
			fmt.Fprintf(
				&source,
				`%slogic %s
`,
				group,
				q(m.Project.Module+"/internal/logic/"+group),
			)
		}
	}

	source.WriteString(`)

type CandidateOperations struct {
`)

	for _, operation := range m.Operations {
		fmt.Fprintf(
			&source,
			`%s %soperations.%sBoundary
`,
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
		)
	}

	source.WriteString(
		`}

type CandidateRuntime struct {
	Operations              CandidateOperations
	candidateRuntimeManager *auth.Manager
	candidateRuntimeHandler http.Handler
	candidateRuntimePool    *pgxpool.Pool
}
`,
	)
	source.WriteString(
		`func (r *CandidateRuntime) Handler() http.Handler {
	return r.candidateRuntimeHandler
}

func (r *CandidateRuntime) WithSession(
	ctx context.Context,
	credential, tenant string,
	run func(context.Context, access.Caller) error,
) error {
	if r == nil {
		return errors.New("nil runtime")
	}

	return access.WithSession(
		ctx,
		r.candidateRuntimeManager,
		credential,
		tenant,
		run,
	)
}
`,
	)

	if local {
		source.WriteString(
			`// NewCandidateRuntime owns the only credential session issuer. Its
// password/OIDC manager also issues every opaque operation identity.
func NewCandidateRuntime(
	ctx context.Context,
	cfg config.Config,
	pool *pgxpool.Pool,
	logger *slog.Logger,
	store cache.Store,
	environment string,
	destinations map[string]string,
	integrations credentials.Integrations,
) (*CandidateRuntime, error) {
`,
		)
	} else {
		source.WriteString(
			`func NewCandidateRuntime(
	cfg config.Config,
	pool *pgxpool.Pool,
	logger *slog.Logger,
	store cache.Store,
	environment string,
	destinations map[string]string,
) (*CandidateRuntime, error) {
`,
		)
	}

	source.WriteString(
		`
	if pool == nil || logger == nil || environment == "" {
		return nil, errors.New("invalid candidate runtime")
	}

	if store == nil {
		store = cache.Disabled{}
	}

	policy, err := access.NewPolicy()
	if err != nil {
		return nil, err
	}

	memberships := identity.NewMemberships(pool)
`,
	)

	if local {
		source.WriteString(
			`
	authentication, err := newCandidateAuthentication(
		ctx,
		cfg,
		pool,
		memberships,
		integrations,
	)
	if err != nil {
		return nil, err
	}
`,
		)
	} else {
		source.WriteString(
			`
	sessions, err := authpostgres.New(pool)
	if err != nil {
		return nil, err
	}

	authentication, err := transport.NewAuthentication(
		cfg.Auth,
		sessions,
		memberships,
	)
	if err != nil {
		return nil, err
	}
`,
		)
	}

	source.WriteString(
		`
	authorizer, err := access.NewAuthorizer(policy, memberships)
	if err != nil {
		return nil, err
	}

	_ = authorizer
	_ = destinations
	runtime := &CandidateRuntime{
		candidateRuntimeManager: authentication.Manager,
		candidateRuntimePool:    pool,
	}
`,
	)

	for _, resource := range m.Resources {
		if !usedResources[resource.ID()] {
			continue
		}

		fmt.Fprintf(
			&source,
			`
	machine%s, err := %sdomain.New%sMachine()
	if err != nil {
		return nil, err
	}
`,
			resource.Group+resource.Name,
			resource.Group,
			resource.Name,
		)
	}

	if len(authoredGroups) > 0 && len(m.Ports) > 0 {
		source.WriteString(
			`
	adapters := NewAuthoredAdapters(pool)
`,
		)
	}

	for index, operation := range m.Operations {
		if operation.Scope != "" {
			if err := writeSystemOperation(
				&source,
				operation,
				index,
			); err != nil {
				return nil, err
			}

			continue
		}

		if operation.isAuthored() {
			writeAuthoredOperation(&source, operation, index)

			continue
		}

		contract := contracts[operation.ID()]
		resource := resources[contract.Resource]
		suffix := fmt.Sprint(index)
		fmt.Fprintf(
			&source,
			`
	policy%s, err := %sdata.New%sResourcePolicy(
		pool,
		%s,
	)
	if err != nil {
		return nil, err
	}

	requirement%s, err := authorizer.Require(%s)
	if err != nil {
		return nil, err
	}

	resource%s, err := requirement%s.WithResourcePolicy(
		policy%s,
	)
	if err != nil {
		return nil, err
	}

	gate%s, err := identitygate.New(
		authentication.Manager,
		resource%s,
		memberships,
	)
	if err != nil {
		return nil, err
	}
`,
			suffix,
			resource.Group,
			resource.Name,
			q(operation.ID()),
			suffix,
			q(operation.Permission),
			suffix,
			suffix,
			suffix,
			suffix,
			suffix,
		)

		if contract.Kind == "effect" {
			fmt.Fprintf(
				&source,
				`
	data%s, err := %seffects.New%sDelivery(
		pool,
		gate%s,
		destinations[%s],
	)
	if err != nil {
		return nil, err
	}
`,
				suffix,
				resource.Group,
				symbol(operation.Endpoint()),
				suffix,
				q(operation.ID()),
			)
		} else {
			fmt.Fprintf(
				&source,
				`
	data%s, err := %sdata.New%sData(
		pool,
		gate%s,
		machine%s,
		store,
		%s,
		environment,
		%s,
	)
	if err != nil {
		return nil, err
	}
`,
				suffix,
				resource.Group,
				resource.Name,
				suffix,
				resource.Group+resource.Name,
				q(m.Project.Name),
				q(operation.ID()),
			)
		}

		fmt.Fprintf(
			&source,
			`
	runtime.Operations.%s = %soperations.New%s(
		gate%s,
		machine%s,
		data%s,
	)
`,
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
			suffix,
			resource.Group+resource.Name,
			suffix,
		)
	}

	if err := writeAuthEvents(&source, m); err != nil {
		return nil, err
	}

	source.WriteString(
		`
	router := chi.NewRouter()
	router.Use(transport.SecurityHeaders)
	router.Use(transport.Recover(logger))

	reg := transport.Registrar{
		Router:   router,
		Auth:     authentication.Verifier,
		Sessions: authentication.HTTP,
		Features: features.New(cfg.Features),
		Logger:   logger,
		Policy:   policy,
	}
`,
	)
	source.WriteString(
		`	transport.RegisterSessions(reg, authentication)
`,
	)

	if local && m.AuthEventsOn() {
		source.WriteString(
			`	registerCandidateCredentials(router, authentication, events)
`,
		)
	} else if local {
		source.WriteString(
			`	registerCandidateCredentials(router, authentication)
`,
		)
	}

	source.WriteString(
		`	reg.Handle(
		paths.Health,
		func(w http.ResponseWriter, r *http.Request) {
			transport.JSON(w, 200, paths.HealthResponse{Data: "ok"})
		},
	)
`,
	)

	for _, entry := range m.Endpoints {
		operation, _ := m.Operation(entry.Operation)
		fmt.Fprintf(
			&source,
			`
	%shandlers.Register%s(
		reg,
		runtime.Operations.%s,
		cfg.Server.BodyLimit,
	)
`,
			entry.Group,
			entry.Action,
			symbol(operation.Endpoint()),
		)
	}

	source.WriteString(
		`
	_ = reg
	runtime.candidateRuntimeHandler = router

	return runtime, nil
}
`,
	)
	source.WriteString(`type CandidateWorkerCredentials struct {
`)

	for _, job := range m.Jobs {
		fmt.Fprintf(
			&source,
			`%s string
`,
			symbol(Endpoint{Group: job.Group, Action: job.Action}),
		)
	}

	source.WriteString(
		`}

func (r *CandidateRuntime) Worker(
	credentials CandidateWorkerCredentials,
) (*jobs.Worker, error) {
	if r == nil {
		return nil, errors.New("nil runtime")
	}

	handlers := map[jobs.Kind]jobs.Handler{}
`,
	)

	for index, job := range m.Jobs {
		operation, _ := m.Operation(job.Operation)
		fmt.Fprintf(
			&source,
			`
	handler%d, err := %sjobs.New%sHandler(
		r.Operations.%s,
		r.candidateRuntimeManager,
		credentials.%s,
	)
	if err != nil {
		return nil, err
	}

	handlers[%s] = handler%d
`,
			index,
			job.Group,
			job.Action,
			symbol(operation.Endpoint()),
			symbol(Endpoint{Group: job.Group, Action: job.Action}),
			q(job.Kind),
			index,
		)
	}

	source.WriteString(
		`
	store, err := jobspostgres.New(r.candidateRuntimePool)
	if err != nil {
		return nil, err
	}

	return jobs.NewWorker(store, handlers, jobs.DefaultConfig())
}
`,
	)

	if len(authoredGroups) > 0 {
		source.WriteString(candidateTenantResourceSource)
	}

	// Keep imported candidate framework modules available for bootstrap
	// projects.
	source.WriteString(`var _ = identitygate.New
`)

	return format.Source(source.Bytes())
}

// writeAuthoredOperation wires one authored-body operation: its gate with the
// tenant resource policy, and the declared adapters as the logic ports. The
// gate is the only way the authored body runs.
func writeAuthoredOperation(
	source *bytes.Buffer,
	operation Operation,
	index int,
) {
	suffix := fmt.Sprint(index)

	fmt.Fprintf(
		source,
		`
	requirement%[1]s, err := authorizer.Require(%[2]s)
	if err != nil {
		return nil, err
	}

	resource%[1]s, err := requirement%[1]s.WithResourcePolicy(
		candidateTenantResource(%[3]s),
	)
	if err != nil {
		return nil, err
	}

	gate%[1]s, err := identitygate.New(
		authentication.Manager,
		resource%[1]s,
		memberships,
	)
	if err != nil {
		return nil, err
	}
`,
		suffix,
		strconv.Quote(operation.Permission),
		strconv.Quote(operation.Resource),
	)

	var ports bytes.Buffer

	for _, id := range operation.Ports {
		if foreignPort(operation.Group, id) {
			fmt.Fprintf(
				&ports,
				"%s: adapters.%sTx, ",
				foreignPortField(id),
				adapterField(Port{ID: id}),
			)

			continue
		}

		fmt.Fprintf(
			&ports,
			"%s: adapters.%s, ",
			modelName(id),
			adapterField(Port{ID: id}),
		)
	}

	for _, id := range operation.Ports {
		if foreignPort(operation.Group, id) {
			ports.WriteString("TxDB: adapters.DB, ")

			break
		}
	}

	fmt.Fprintf(
		source,
		`
	runtime.Operations.%[1]s = %[2]soperations.New%[3]s(
		gate%[4]s,
		%[2]slogic.%[3]sPorts{%[5]s},
	)
`,
		symbol(operation.Endpoint()),
		operation.Group,
		operation.Action,
		suffix,
		ports.String(),
	)
}

// candidateTenantResourceSource is the tenant resource policy of authored
// operations: membership and permission are verified by the gate, row-level
// security isolates the data, and the resource must be the declared one.
const candidateTenantResourceSource = `
func candidateTenantResource(resource string) rbac.ResourcePolicyFunc {
	return func(
		_ context.Context,
		request rbac.Request,
		id string,
	) error {
		if id != resource || request.Scope.TenantID == "" {
			return rbac.ErrForbidden
		}

		return nil
	}
}
`

// writeSystemOperation wires one provision or lookup operation: the shared
// attempt limiter plus either the provisioning gate over the declared ports
// or the generated lookup reader. Neither has a membership gate: no tenant
// or membership exists at that point.
func writeSystemOperation(
	source *bytes.Buffer,
	operation Operation,
	index int,
) error {
	attempts, window, err := ParseRateLimit(operation.RateLimit)
	if err != nil {
		return fmt.Errorf("operation %s: %w", operation.ID(), err)
	}

	suffix := fmt.Sprint(index)

	fmt.Fprintf(
		source,
		`
	limiter%[1]s, err := identity.NewAttemptLimiter(
		pool,
		%[2]d,
		time.Duration(%[3]d),
	)
	if err != nil {
		return nil, err
	}
`,
		suffix,
		attempts,
		window.Nanoseconds(),
	)

	if operation.Scope == ScopeLookup {
		fmt.Fprintf(
			source,
			`
	runtime.Operations.%[1]s = %[2]soperations.New%[3]s(
		%[2]slookup.New%[3]sReader(pool),
		limiter%[4]s,
	)
`,
			symbol(operation.Endpoint()),
			operation.Group,
			operation.Action,
			suffix,
		)

		return nil
	}

	var ports bytes.Buffer

	transactional := false

	for _, id := range operation.Ports {
		if foreignPort(operation.Group, id) {
			transactional = true

			fmt.Fprintf(
				&ports,
				"%s: adapters.%sTx, ",
				foreignPortField(id),
				adapterField(Port{ID: id}),
			)

			continue
		}

		fmt.Fprintf(
			&ports,
			"%s: adapters.%s, ",
			modelName(id),
			adapterField(Port{ID: id}),
		)
	}

	if transactional {
		ports.WriteString("TxDB: adapters.DB, ")
	}

	fmt.Fprintf(
		source,
		`
	runtime.Operations.%[1]s = %[2]soperations.New%[3]s(
		authentication.Manager,
		limiter%[4]s,
		%[2]slogic.%[3]sPorts{%[5]s},
	)
`,
		symbol(operation.Endpoint()),
		operation.Group,
		operation.Action,
		suffix,
		ports.String(),
	)

	return nil
}

// writeAuthEvents composes the generated authentication event hook around the
// declared operation. The recorder is the only code that calls it.
func writeAuthEvents(source *bytes.Buffer, m Manifest) error {
	if !m.AuthEventsOn() {
		return nil
	}

	operation, ok := m.Operation(m.Auth.Events.Operation)
	if !ok {
		return fmt.Errorf(
			"auth events operation %s is not declared",
			m.Auth.Events.Operation,
		)
	}

	fmt.Fprintf(
		source,
		`
	events, err := newAuthEvents(
		authentication,
		memberships,
		integrations.Ingress,
		func(
			ctx context.Context,
			caller access.Caller,
			event string,
		) error {
			return runtime.Operations.%[1]s.Execute(
				ctx,
				caller,
				authEventModels.%[2]sRequest{Event: event},
			)
		},
		logger,
	)
	if err != nil {
		return nil, err
	}
`,
		symbol(operation.Endpoint()),
		operation.Action,
	)

	return nil
}
