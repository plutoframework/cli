package project

import (
	"strings"
	"testing"
	"time"
)

// tenantJobFixture extends the system fixture with a service role, an
// own-group port and one scheduled each_tenant operation.
func tenantJobFixture(t *testing.T) Manifest {
	t.Helper()

	m := systemFixture(t)

	m.Roles = append(m.Roles, RoleContract{
		ID:          "janitor",
		Name:        "Janitor",
		Description: "Service role of the sweeps.",
		Permissions: []string{"orgs.sweep"},
	})
	m.Ports = append(m.Ports, Port{
		ID:      "orgs.Sweeps",
		Adapter: "orgs.SweepStore",
		Methods: []string{"Record(scope.Execution) error"},
	})
	m.Operations = append(m.Operations, Operation{
		Group:          "orgs",
		Action:         "Sweep",
		Scope:          ScopeEachTenant,
		Schedule:       "every 30s",
		Write:          true,
		Idempotency:    "exempt",
		ReplayReason:   "A repeated sweep is harmless.",
		Permission:     "orgs.sweep",
		ServiceRole:    "janitor",
		Body:           BodyAuthored,
		Resource:       "orgs.sweeps",
		ResourcePolicy: "tenant",
		Ports:          []string{"orgs.Sweeps"},
	})

	return m
}

func TestTenantJobManifestValidatesAndRoundTrips(t *testing.T) {
	m := tenantJobFixture(t)
	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	data, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}

	operation, ok := decoded.Operation("orgs.Sweep")
	if !ok || operation.Scope != ScopeEachTenant ||
		operation.Interval() != 30*time.Second ||
		operation.TenantTimeout() != defaultTenantJobTimeout ||
		operation.TenantConcurrency() != defaultTenantJobConcurrency ||
		operation.TenantAttempts() != defaultTenantJobAttempts ||
		operation.JobKind() != "tenant.orgs.sweep" {
		t.Fatalf("round trip lost the tenant job: %+v", operation)
	}
}

func TestTenantJobManifestRejections(t *testing.T) {
	type change func(*Manifest, *Operation)

	mutate := func(apply change) func(*testing.T) Manifest {
		return func(t *testing.T) Manifest {
			t.Helper()

			m := tenantJobFixture(t)
			index := len(m.Operations) - 1
			apply(&m, &m.Operations[index])

			return m
		}
	}

	for name, test := range map[string]struct {
		build func(*testing.T) Manifest
		want  string
	}{
		"missing service role": {
			mutate(func(_ *Manifest, o *Operation) {
				o.ServiceRole = ""
			}),
			"service_role",
		},
		"undeclared service role": {
			mutate(func(_ *Manifest, o *Operation) {
				o.ServiceRole = "ghost"
			}),
			"service_role",
		},
		"role lacks the permission": {
			mutate(func(m *Manifest, _ *Operation) {
				m.Roles[len(m.Roles)-1].Permissions = []string{"orgs.other"}
			}),
			"directly grants",
		},
		"missing permission": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Permission = ""
			}),
			"permission",
		},
		"public": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Public = true
			}),
			"protected",
		},
		"missing resource": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Resource, o.ResourcePolicy = "", ""
			}),
			"resource",
		},
		"bad schedule": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Schedule = "daily"
			}),
			"schedule",
		},
		"too frequent": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Schedule = "every 1s"
			}),
			"5s to 24h",
		},
		"timeout beyond the schedule": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Timeout = "5m"
			}),
			"pile up",
		},
		"concurrency too high": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Concurrency = 999
			}),
			"concurrency",
		},
		"attempts too high": {
			mutate(func(_ *Manifest, o *Operation) {
				o.MaxAttempts = 101
			}),
			"max_attempts",
		},
		"write without replay reason": {
			mutate(func(_ *Manifest, o *Operation) {
				o.ReplayReason = ""
			}),
			"idempotency",
		},
		"request model": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Input = "orgs.Signup"
			}),
			"no request",
		},
		"query": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Query = []OperationQuery{{Name: "x", Type: "string"}}
			}),
			"no request",
		},
		"generated body": {
			mutate(func(_ *Manifest, o *Operation) {
				o.Body = BodyGenerated
			}),
			"authored",
		},
		"foreign port": {
			mutate(func(m *Manifest, o *Operation) {
				o.Ports = []string{"audit.Events"}
			}),
			"",
		},
		"http entry": {
			mutate(func(m *Manifest, _ *Operation) {
				m.Endpoints = append(m.Endpoints, Endpoint{
					Group:       "orgs",
					Action:      "Sweep",
					Operation:   "orgs.Sweep",
					Method:      "POST",
					Path:        "/api/orgs/sweep",
					Name:        "Sweep",
					Description: "Run a sweep.",
				})
			}),
			"",
		},
		"job kind collision": {
			mutate(func(m *Manifest, _ *Operation) {
				m.Jobs = append(m.Jobs, Job{
					Group:       "orgs",
					Action:      "Other",
					Kind:        "tenant.orgs.sweep",
					Operation:   "orgs.Provision",
					MaxAttempts: 3,
				})
			}),
			"collides",
		},
		"no tenant root": {
			mutate(func(m *Manifest, _ *Operation) {
				for index := range m.Tables {
					m.Tables[index].TenantRoot = false
				}
			}),
			"",
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := test.build(t).Validate()
			if err == nil {
				t.Fatal("accepted")
			}

			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %q, got %v", test.want, err)
			}
		})
	}
}

func TestParseSchedule(t *testing.T) {
	for spec, want := range map[string]time.Duration{
		"every 30s":   30 * time.Second,
		"every 5s":    5 * time.Second,
		"every 24h":   24 * time.Hour,
		"triggered":   0,
		"every 1m30s": 90 * time.Second,
	} {
		got, err := ParseSchedule(spec)
		if err != nil || got != want {
			t.Fatalf("%q: %v %v", spec, got, err)
		}
	}

	for _, spec := range []string{
		"", "every", "every 4s", "every 25h", "30s", "daily", "every -5s",
		"Every 30s",
	} {
		if _, err := ParseSchedule(spec); err == nil {
			t.Fatalf("%q accepted", spec)
		}
	}
}

func TestTenantJobManagedOutput(t *testing.T) {
	m := tenantJobFixture(t)

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	contains := func(path string, wants ...string) {
		t.Helper()

		source, ok := files[path]
		if !ok {
			t.Fatalf("missing managed file %s", path)
		}

		for _, want := range wants {
			if !strings.Contains(string(source), want) {
				t.Fatalf("%s lacks %q:\n%s", path, want, source)
			}
		}
	}

	contains(
		"internal/operations/orgs/sweep_gate_gen.go",
		`SweepKind jobs.Kind = "tenant.orgs.sweep"`,
		"SweepInterval = time.Duration(30000000000)",
		"Tenants.Exists(ctx, tenant)",
		"if !found {\n\t\t\treturn jobs.Permanent(jobs.ErrInvalid)",
		"jobs.Permanent(jobs.ErrInvalid)",
		"WithIdentity(",
		"Gate.Within(",
		"context.WithTimeout(ctx, SweepTimeout)",
	)
	contains(
		"internal/operations/orgs/sweep_workflow_gen.go",
		"logic.Sweep(",
		"scope.Bind(",
	)
	contains(
		tenantIndexFile,
		"set_config('app.tenant_index', 'on', true)",
		"FROM pluto_tenant_index",
		"ErrUnknownTenant",
	)
	contains(
		"internal/logic/orgs/contract_gen.go",
		"var _ func(",
	)

	contract := string(files["internal/logic/orgs/contract_gen.go"])
	if strings.Contains(contract, "SweepRequest") {
		t.Fatalf("tenant job body must have no request model:\n%s", contract)
	}

	app := string(files["internal/app/candidate_operations_gen.go"])
	for _, want := range []string{
		"func (r *CandidateRuntime) Schedule(",
		"tenantindex.NewQueue(pool)",
		"rbac.RoleID(\"janitor\")",
		"orgsoperations.SweepKind",
		"OrgsSweep ",
	} {
		if !strings.Contains(app, want) {
			t.Fatalf("runtime lacks %q:\n%s", want, app)
		}
	}
}

func TestTenantJobIndexMigration(t *testing.T) {
	m := tenantJobFixture(t)
	root, _ := m.TenantRoot()

	up := tenantIndexMigrationSQL(m, root)
	for _, want := range []string{
		"CREATE POLICY pluto_tenant_index ON orgs FOR SELECT",
		"current_setting('app.tenant_index', true) = 'on'",
		"security_invoker = true",
		"SELECT id AS tenant_id FROM orgs",
	} {
		if !strings.Contains(up, want) {
			t.Fatalf("index migration lacks %q:\n%s", want, up)
		}
	}

	if down := tenantIndexDownSQL(m, root); !strings.Contains(
		down, "DROP VIEW IF EXISTS pluto_tenant_index") {
		t.Fatalf("index down migration:\n%s", down)
	}

	// Without a tenant job there is no index, and no other table has one.
	plain := systemFixture(t)
	if tenantIndexMigrationSQL(plain, root) != "" {
		t.Fatal("index rendered without a tenant job")
	}

	other := Table{Name: "users", TenantColumn: "org_id"}
	if tenantIndexMigrationSQL(m, other) != "" {
		t.Fatal("index rendered for a non-root table")
	}
}

func TestScopeEnqueueIsBoundToDeclaredKinds(t *testing.T) {
	files, err := candidateManaged(tenantJobFixture(t))
	if err != nil {
		t.Fatal(err)
	}

	source := string(files[scopeFile])

	for _, want := range []string{
		"attempts, declared := enqueueAttempts[kind]",
		"if !declared {\n\t\treturn ErrUnknownJob",
		"TenantID:    &tenant",
		"MaxAttempts: attempts",
		"EnqueueTx(ctx, t.scopeTx, request)",
	} {
		if !strings.Contains(source, want) {
			t.Fatalf("scope Enqueue lacks %q:\n%s", want, source)
		}
	}

	if strings.Contains(source, "return ErrUnsupported") {
		t.Fatal("Tx.Enqueue must no longer fail closed unconditionally")
	}
}

// Only the generated tenant job gate and the scope package may handle job
// requests without a declared effect; the same code anywhere else is still an
// undeclared effect.
func TestTenantJobExemptionIsPathSpecific(t *testing.T) {
	const body = `package operations

import queue "github.com/plutoframework/pluto-jobs"

var decode = queue.Decode[string]
`

	root := primitiveFixture(t, map[string]string{
		"internal/operations/orgs/sweep_gate_gen.go": body,
		"internal/operations/orgs/other.go":          body,
		"internal/scope/scope_gen.go":                body,
	})

	diagnostics, _ := inspectPrimitiveBoundaries(
		root,
		"example.test/app",
		[]byte("[[operations]]\ngroup='orgs'\naction='Sweep'\n"+
			"scope='each_tenant'\n"),
	)

	flagged := map[string]bool{}

	for _, diagnostic := range diagnostics {
		if diagnostic.ID == "ARCH506" {
			flagged[diagnostic.File] = true
		}
	}

	if len(flagged) != 1 || !flagged["internal/operations/orgs/other.go"] {
		t.Fatalf("exemption must cover only managed files: %+v", diagnostics)
	}

	// Without a tenant job the gate path is not exempt, the scope package
	// still is.
	diagnostics, _ = inspectPrimitiveBoundaries(
		root,
		"example.test/app",
		[]byte("[[operations]]\ngroup='orgs'\naction='Read'\n"),
	)

	flagged = map[string]bool{}

	for _, diagnostic := range diagnostics {
		if diagnostic.ID == "ARCH506" {
			flagged[diagnostic.File] = true
		}
	}

	if flagged["internal/scope/scope_gen.go"] ||
		!flagged["internal/operations/orgs/sweep_gate_gen.go"] {
		t.Fatalf("scope-only exemption broken: %+v", diagnostics)
	}
}
