package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const authoredBuildDeclarations = `
[[roles]]
id="auditor"
name="Auditor"
description="Reads audit"
permissions=["audit.read"]
[[tables]]
group="audit"
name="audit_events"
tenant_column="org_id"
row_security=true
[[ports]]
id="audit.Events"
adapter="audit.EventStore"
methods=["Query"]
[[models]]
id="audit.Entry"
fields=["seq:int64"]
[[operations]]
group="audit"
action="List"
permission="audit.read"
collection=true
idempotency="none"
body="authored"
resource="audit.events"
resource_policy="tenant"
ports=["audit.Events"]
response="page"
item="audit.Entry"
`

const authoredBuildModels = `package audit

import "github.com/plutoframework/pluto-core/pagination"

type Entry struct{ Seq int64 }

type ListRequest struct{ Page pagination.Request }
`

const authoredBuildPorts = `package audit

import (
	"context"

	models "example.test/auditapp/internal/models/audit"
	"example.test/auditapp/internal/scope"
	"github.com/plutoframework/pluto-core/pagination"
)

type Events interface {
	Query(
		context.Context,
		scope.Execution,
		pagination.Request,
	) (pagination.Page[models.Entry], error)
}
`

const authoredBuildBody = `package audit

import (
	"context"

	models "example.test/auditapp/internal/models/audit"
	"example.test/auditapp/internal/scope"
	"github.com/plutoframework/pluto-core/pagination"
)

func List(
	ctx context.Context,
	e scope.Execution,
	p ListPorts,
	in models.ListRequest,
) (pagination.Page[models.Entry], error) {
	return p.Events.Query(ctx, e, in.Page)
}
`

const authoredBuildTest = `package audit_test

import (
	"context"
	"errors"
	"testing"

	logic "example.test/auditapp/internal/logic/audit"
	models "example.test/auditapp/internal/models/audit"
	"example.test/auditapp/internal/scope"
	"example.test/auditapp/internal/scope/scopetest"
	rbac "github.com/plutoframework/pluto-rbac"
	"github.com/plutoframework/pluto-rbac/identity/identitytest"
	"github.com/plutoframework/pluto-core/pagination"
)

type fake struct{ tenant string }

func (f *fake) Query(
	_ context.Context,
	e scope.Execution,
	_ pagination.Request,
) (pagination.Page[models.Entry], error) {
	f.tenant = e.Tenant().String()

	return pagination.Page[models.Entry]{}, nil
}

func TestBodyRunsOnlyWithGateMintedScope(t *testing.T) {
	h, err := scopetest.New(identitytest.Options{
		Policy: rbac.Definition{
			Permissions: []rbac.Permission{
				{ID: "audit.read", Name: "Read", Description: "Read"},
			},
			Roles: []rbac.Role{{
				ID: "auditor", Name: "Auditor", Description: "Audit",
				Permissions: []rbac.PermissionID{"audit.read"},
			}},
		},
		Permission: "audit.read",
		Members: []identitytest.Member{{
			SubjectID: "alice", TenantID: "north",
			Roles: []rbac.RoleID{"auditor"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}

	port := &fake{}
	err = h.Within(
		context.Background(), "alice", "north", "audit.events",
		func(ctx context.Context, e scope.Execution) error {
			_, err := logic.List(ctx, e, logic.ListPorts{Events: port}, models.ListRequest{})

			return err
		},
	)
	if err != nil || port.tenant != "north" {
		t.Fatalf("err=%v tenant=%q", err, port.tenant)
	}

	called := false
	err = h.Within(
		context.Background(), "mallory", "north", "audit.events",
		func(context.Context, scope.Execution) error {
			called = true

			return nil
		},
	)
	if err == nil || called {
		t.Fatal("non-member reached the body scope")
	}

	_, err = logic.List(
		context.Background(), scope.Execution{},
		logic.ListPorts{Events: port}, models.ListRequest{},
	)
	if err != nil || port.tenant != "" {
		t.Fatalf("zero execution must carry no tenant: %v %q", err, port.tenant)
	}

	var zero scope.Execution
	if _, err := zero.Tenant().Value(); !errors.Is(err, scope.ErrNoTenant) {
		t.Fatal("zero tenant must be rejected as a query argument")
	}

	var forged scope.Tenant
	if err := forged.Scan("south"); err != nil {
		t.Fatal(err)
	}

	if _, err := forged.Value(); err == nil {
		t.Fatal("scanned tenant became usable")
	}
}
`

// Builds a generated candidate application containing an authored body,
// scope, scopetest and the generated gate and workflow, then runs the body
// through a real gate. Needs explicit local candidate sources.
func TestAuthoredSeamCompilesAndGatesBody(t *testing.T) {
	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	enableAuthoredSeam(t)

	metadata := fmt.Sprintf(`schema_version=%d
[project]
name="auditapp"
module="example.test/auditapp"
env_prefix="AUDITAPP_"
[capabilities]
tenancy="multi"
cache="none"
[database]
engine="postgres"
queries="sqlc"
[boundary]
mode="candidate"
auth_source=%q
rbac_source=%q
core_source=%q
jobs_source=%q
%s`,
		SchemaVersion,
		os.Getenv("PLUTO_CLI_CANDIDATE_AUTH_SOURCE"),
		os.Getenv("PLUTO_CLI_CANDIDATE_RBAC_SOURCE"),
		os.Getenv("PLUTO_CLI_CANDIDATE_CORE_SOURCE"),
		os.Getenv("PLUTO_CLI_CANDIDATE_JOBS_SOURCE"),
		authoredBuildDeclarations,
	)

	m, err := Decode([]byte(metadata))
	if err != nil {
		t.Fatal(err)
	}

	files, err := InitialFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	operation, _ := m.Operation("audit.List")

	scaffold, err := candidateAuthoredScaffold(m, operation)
	if err != nil {
		t.Fatal(err)
	}

	for path, source := range scaffold {
		files[path] = source
	}

	files["internal/models/audit/models.go"] = []byte(authoredBuildModels)
	files["internal/ports/audit/events.go"] = []byte(authoredBuildPorts)
	files["internal/logic/audit/list.go"] = []byte(authoredBuildBody)
	files["internal/logic/audit/list_test.go"] = []byte(authoredBuildTest)

	root := t.TempDir()

	for path, source := range files {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(target, source, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run := func(args ...string) {
		t.Helper()

		command := exec.Command("go", args...)
		command.Dir = root

		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	run("mod", "tidy")
	run("build", "./internal/scope/...", "./internal/logic/...",
		"./internal/operations/audit/...")
	run("vet", "./internal/scope/...", "./internal/logic/...",
		"./internal/operations/audit/...")
	run("test", "-race", "-count=1", "./internal/logic/...")

	for _, diagnostic := range InspectArchitecture(root).Diagnostics {
		switch diagnostic.ID {
		case "ARCH603", "ARCH604", "ARCH610", "ARCH611":
			t.Fatalf("authored seam violates its own checks: %+v", diagnostic)
		}
	}
}
