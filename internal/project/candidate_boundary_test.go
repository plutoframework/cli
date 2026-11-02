package project

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	candidateReviewModule      = "example.test/candidate"
	candidateReviewDefinitions = `
[boundary]
mode="candidate"
[[resources]]
group="sales"
name="Invoice"
table="invoices"
namespace="sales-invoices"
version=1
visibility="owner"
initial_state="draft"
states=["draft","approved"]
transitions=[{from="draft",event="approve",to="approved",guard="none"}]
normalize="trim"
max_value_bytes=4096
page_limit=50
export_limit=500
cache_ttl_seconds=60
[[operations]]
group="sales"
action="Approve"
permission="invoice:approve"
write=true
idempotency="required"
[[operation_contracts]]
id="sales.Approve"
resource="sales.Invoice"
kind="transition"
permission="invoice:approve"
resource_policy="owner"
event="approve"
idempotency="required"
`
)

func candidateReviewMetadata(t *testing.T) string {
	t.Helper()

	profile := boundaryProfileFixture(t)

	settings := fmt.Sprintf(
		"mode=\"candidate\"\nauth_source=%q\nrbac_source=%q\ncore_source=%q\n"+
			"jobs_source=%q",
		profile.AuthSource,
		profile.RBACSource,
		profile.CoreSource,
		profile.JobsSource,
	)
	definitions := strings.Replace(
		candidateReviewDefinitions,
		`mode="candidate"`,
		settings,
		1,
	)

	return fmt.Sprintf(`schema_version=%d
[project]
name="candidate"
module="example.test/candidate"
env_prefix="CANDIDATE_"
[capabilities]
tenancy="multi"
cache="none"
[database]
engine="postgres"
queries="sqlc"
%s`, SchemaVersion, definitions)
}

func candidateReviewFixture(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()

	foundation := map[string]string{
		"internal/domain/sales/invoice_contract_gen.go": `package sales

import "github.com/plutoframework/pluto-core/state"

type InvoiceRecord struct {
	invoiceRecordID    string
	invoiceRecordValid bool
}
type InvoiceCommand struct {
	invoiceCommandID string
}

func NewInvoiceMachine() (
	*state.Machine[string, string, bool],
	error,

) {

	return nil, nil
}
func RehydrateInvoice() InvoiceRecord {

	return InvoiceRecord{}
}
func EvaluateInvoice() {}
func NewInvoiceCommand() InvoiceCommand {

	return InvoiceCommand{}
}
`,
		"internal/access/caller_gen.go": `package access

import "github.com/plutoframework/pluto-auth"

type Caller struct {
	callerIdentity                             *auth.Identity
	callerTenant, callerWorkID, callerWorkKind string
}
`,
	}
	for path, source := range files {
		foundation[path] = source
	}

	for path, source := range foundation {
		absolute := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(absolute, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func candidateReviewHas(
	diagnostics []ArchitectureDiagnostic,
	id, path string,
) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.ID == id && (path == "" || diagnostic.File == path) &&
			diagnostic.Line > 0 &&
			diagnostic.Repair != "" {
			return true
		}
	}

	return false
}

func TestCandidateBoundaryMetadataAndPublishedBlocker(t *testing.T) {
	root := candidateReviewFixture(t, nil)
	for _, test := range []struct {
		name, metadata, want string
	}{
		{"supported", candidateReviewMetadata(t), ""},
		{
			"published",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`mode="candidate"`,
				`mode="published"`,
				1,
			),
			"ARCH401",
		},
		{
			"unknown mode",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`mode="candidate"`,
				`mode="other"`,
				1,
			),
			"ARCH601",
		},
		{
			"permission drift",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`permission="invoice:approve"`,
				`permission="other"`,
				1,
			),
			"ARCH601",
		},
		{
			"write drift",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`write=true`,
				`write=false`,
				1,
			),
			"ARCH601",
		},
		{
			"replay drift",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`idempotency="required"`,
				`idempotency="none"`,
				1,
			),
			"ARCH601",
		},
		{
			"missing contract",
			strings.Split(
				candidateReviewMetadata(
					t,
				),
				"[[operation_contracts]]",
			)[0],
			"ARCH601",
		},
		{
			"empty resource name",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`name="Invoice"`,
				`name=""`,
				1,
			),
			"ARCH601",
		},
		{
			"empty operation action",
			strings.Replace(
				candidateReviewMetadata(
					t,
				),
				`action="Approve"`,
				`action=""`,
				1,
			),
			"ARCH601",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostics, _ := inspectCandidateBoundaries(
				root,
				candidateReviewModule,
				[]byte(test.metadata),
			)
			if test.want == "" && len(diagnostics) > 0 {
				t.Fatalf("supported ownership rejected: %+v", diagnostics)
			}

			if test.want != "" &&
				!candidateReviewHas(diagnostics, test.want, "") {
				t.Fatalf("missing %s: %+v", test.want, diagnostics)
			}
		})
	}
}

func TestCandidateBoundaryCapabilitiesAndForgery(t *testing.T) {
	for _, test := range []struct {
		name, path, source, id string
	}{
		{
			"rehydration capture",
			"internal/domain/sales/policy.go",
			`package sales

var rehydrate = RehydrateInvoice
`,
			"ARCH603",
		},
		{
			"qualified machine capture",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "example.test/candidate/internal/domain/sales"

var machine = sales.NewInvoiceMachine
`,
			"ARCH603",
		},
		{
			"data constructor capture",
			"internal/app/legacy.go",
			`package app

import data "example.test/candidate/internal/storage/sales"

var constructor = data.NewInvoiceData
`,
			"ARCH603",
		},
		{
			"operation constructor",
			"internal/app/legacy.go",
			`package app

import op "example.test/candidate/internal/operations/sales"

var constructor = op.NewApproveWithDependencies
`,
			"ARCH603",
		},
		{
			"domain evaluator capture",
			"internal/domain/sales/policy.go",
			`package sales

var evaluate = EvaluateInvoice
`,
			"ARCH603",
		},
		{
			"private record assignment",
			"internal/domain/sales/policy.go",
			`package sales

func mutate(r *InvoiceRecord) {

	r.invoiceRecordID = "other"
}
`,
			"ARCH604",
		},
		{
			"record composite",
			"internal/domain/sales/policy.go",
			`package sales

var record = InvoiceRecord{}
`,
			"ARCH604",
		},
		{
			"record alias",
			"internal/domain/sales/policy.go",
			`package sales

type Alias = InvoiceRecord

var record = Alias{}
`,
			"ARCH604",
		},
		{
			"record defined wrapper",
			"internal/domain/sales/policy.go",
			`package sales

type Alias InvoiceRecord

var record = Alias{}
`,
			"ARCH604",
		},
		{
			"caller alias mutation",
			"internal/access/legacy.go",
			`package access

type Alias = Caller

func mutate(c Alias) {

	copy := c
	copy.callerTenant = "other"
}
`,
			"ARCH604",
		},
		{
			"caller field capture",
			"internal/access/legacy.go",
			`package access

func capture(c Caller) {

	_ = c.callerIdentity
}
`,
			"ARCH604",
		},
		{
			"opaque identity composite",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "github.com/plutoframework/pluto-auth"

var fake = auth.Identity{}
`,
			"ARCH604",
		},
		{
			"opaque scope allocation",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "github.com/plutoframework/pluto-rbac/identity"

var fake = new(identitygate.Execution)
`,
			"ARCH604",
		},

		{
			"typed issuer Start capture",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "github.com/plutoframework/pluto-auth"

func bad(manager *auth.Manager) {

	_ = manager.Start
}
`,
			"ARCH606",
		},
		{
			"runtime manager private mutation",
			"internal/app/legacy.go",
			`package app

func bad(r *CandidateRuntime) {

	r.candidateRuntimeManager = nil
}
`,
			"ARCH604",
		},
		{
			"caller work binding mutation",
			"internal/access/legacy.go",
			`package access

func bad(c *Caller) {

	c.callerWorkKind = "other"
}
`,
			"ARCH604",
		},
		{
			"issuer default package name",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "github.com/plutoframework/pluto-auth"

var factory = auth.New
`,
			"ARCH603",
		},
		{
			"gate default package name",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "github.com/plutoframework/pluto-rbac/identity"

var factory = identitygate.New
`,
			"ARCH603",
		},
		{
			"dot hidden domain factory",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import . "example.test/candidate/internal/domain/sales"

var factory = NewInvoiceMachine
`,
			"ARCH606",
		},

		{
			"operation boundary wrapper",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import op "example.test/candidate/internal/operations/sales"

type inherited op.ApproveBoundary
type fake struct{ inherited }
`,
			"ARCH604",
		},
		{
			"operation boundary embedding",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import op "example.test/candidate/internal/operations/sales"

type fake struct{ op.ApproveBoundary }
`,
			"ARCH604",
		},
		{
			"raw port alias",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import contract "example.test/candidate/internal/ports/sales"

type Raw contract.InvoiceData
`,
			"ARCH604",
		},
		{
			"unsupported authored domain hook",
			"internal/domain/sales/policy.go",
			`package sales

func ApprovalRule() bool {

	return true
}
`,
			"ARCH602",
		},

		{
			"unmanaged startup initializer",
			"internal/app/legacy.go",
			`package app

func init() {}
`,
			"ARCH602",
		},
		{
			"unmanaged command initializer",
			"cmd/api/legacy.go",
			`package main

func init() {}
`,
			"ARCH602",
		},
		{
			"unmanaged unrelated import",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import _ "example.test/opaque"
`,
			"ARCH602",
		},
		{
			"raw database client",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import pool "github.com/jackc/pgx/v5/pgxpool"

var factory = pool.New
`,
			"ARCH608",
		},
		{
			"raw authentication store",
			"internal/app/legacy.go",
			`package app

import store "github.com/plutoframework/pluto-auth/stores/postgres"

var factory = store.New
`,
			"ARCH608",
		},
		{
			"extra operation source",
			"internal/operations/sales/legacy.go",
			`package sales

func extra() {}
`,
			"ARCH602",
		},
		{
			"extra data source",
			"internal/storage/sales/legacy.go",
			`package sales

func extra() {}
`,
			"ARCH602",
		},
		{
			"extra port source",
			"internal/ports/sales/legacy.go",
			`package sales

type Raw interface{ Commit() }
`,
			"ARCH602",
		},
		{
			"extra worker entry",
			"internal/jobs/legacy/work.go",
			`package legacy

func extra() {}
`,
			"ARCH602",
		},
		{
			"extra tool entry",
			"internal/agents/legacy/work.go",
			`package legacy

func extra() {}
`,
			"ARCH602",
		},

		{
			"reflection dynamic method escape",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "reflect"

func escape(
	v any,

) {

	reflect.ValueOf(
		v,
	).MethodByName(
		"UnsafePointer",
	).Call(
		nil,
	)
}
`,
			"ARCH605",
		},
		{
			"reflection indexed method escape",
			"internal/infrastructure/legacy.go",
			`package infrastructure

import "reflect"

func escape(v any) {

	reflect.ValueOf(v).Method(0).Call(nil)
}
`,
			"ARCH605",
		},
		{
			"local unsafe",
			"internal/domain/sales/policy.go",
			`package sales

import _ "unsafe"
`,
			"ARCH605",
		},
		{
			"linkname",
			"internal/domain/sales/policy.go",
			"package sales\n//go:linkname escape target\nfunc escape()",
			"ARCH605",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := candidateReviewFixture(
				t,
				map[string]string{test.path: test.source},
			)

			diagnostics, _ := inspectCandidateBoundaries(
				root,
				candidateReviewModule,
				[]byte(candidateReviewMetadata(t)),
			)
			if !candidateReviewHas(diagnostics, test.id, test.path) {
				t.Fatalf(
					"missing %s at %s: %+v",
					test.id,
					test.path,
					diagnostics,
				)
			}
		})
	}
}

func TestCandidateManagedOwners(t *testing.T) {
	root := candidateReviewFixture(t, map[string]string{
		"internal/storage/sales/invoice_reads_gen.go": `package sales

import domain "example.test/candidate/internal/domain/sales"

var hydrate = domain.RehydrateInvoice
`,
		"internal/operations/sales/approve_workflow_gen.go": `package sales

import domain "example.test/candidate/internal/domain/sales"

var evaluate = domain.EvaluateInvoice
`,
		"internal/app/candidate_operations_gen.go": `package app

import domain "example.test/candidate/internal/domain/sales"

var machine = domain.NewInvoiceMachine
`,

		"internal/domain/sales/ignored_test.go": `package sales

import _ "unsafe"

var captured = RehydrateInvoice
`,
	})

	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(candidateReviewMetadata(t)),
	)
	if len(diagnostics) > 0 {
		t.Fatalf("supported source owners rejected: %+v", diagnostics)
	}
}

func TestCandidateUnsafeCannotHideInManagedFile(t *testing.T) {
	path := "internal/app/candidate_operations_gen.go"
	root := candidateReviewFixture(
		t,
		map[string]string{path: `package app

import _ "unsafe"
`},
	)

	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(candidateReviewMetadata(t)),
	)
	if !candidateReviewHas(diagnostics, "ARCH605", path) {
		t.Fatalf("managed marker hid unsafe: %+v", diagnostics)
	}
}

func TestCandidateResourceNamesAreDeclarationDerived(t *testing.T) {
	metadata := strings.NewReplacer(
		"sales",
		"catalog",
		"Invoice",
		"Document",
		"invoice",
		"document",
	).
		Replace(candidateReviewMetadata(t))
	path := "internal/domain/catalog/forged.go"
	root := candidateReviewFixture(
		t,
		map[string]string{
			path: `package catalog

var forged = DocumentRecord{
 documentRecordID: "other",
 documentRecordValid: true,
}
`,
		},
	)

	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(metadata),
	)
	if !candidateReviewHas(diagnostics, "ARCH604", path) {
		t.Fatalf("second resource name escaped: %+v", diagnostics)
	}
}

func TestCandidateHiddenAndNativeBuildInputs(t *testing.T) {
	for _, path := range []string{
		"internal/app/escape.s",
		"internal/app/escape.syso",
		"internal/app/nested/go.mod",
	} {
		root := candidateReviewFixture(
			t,
			map[string]string{path: "module example.test/nested\n"},
		)
		diagnostics, _ := inspectCandidateBoundaries(
			root,
			candidateReviewModule,
			[]byte(candidateReviewMetadata(t)),
		)

		want := "ARCH605"
		if strings.HasSuffix(path, "go.mod") {
			want = "ARCH608"
		}

		if !candidateReviewHas(diagnostics, want, "") {
			t.Fatalf("hidden build input %s escaped: %+v", path, diagnostics)
		}
	}

	root := candidateReviewFixture(t, nil)

	target := filepath.Join(t.TempDir(), "hidden.go")
	if err := os.WriteFile(
		target,
		[]byte("package hidden\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink(target, filepath.Join(root, "linked.go")); err != nil {
		t.Fatal(err)
	}

	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(candidateReviewMetadata(t)),
	)
	if !candidateReviewHas(diagnostics, "ARCH605", "") {
		t.Fatalf("symlink escaped source inspection: %+v", diagnostics)
	}
}

func TestCandidatePreparedIntentAndAgentReceiptFields(t *testing.T) {
	metadata := candidateReviewMetadata(t) + `
[[agents]]
group="reviews"
action="Inspect"
operation="sales.Approve"
permission="invoice:approve"
provider="scripted"
approval="host"
`
	for _, test := range []struct {
		path, source string
	}{
		{
			"internal/operations/sales/extra.go",
			`package sales

func mutate(i *ApproveIntent) {

	i.approveIntentOwner = nil
}
`,
		},
		{
			"internal/agents/reviews/extra.go",
			`package reviews

func mutate(
	d *InspectDependencies,

) {

	d.inspectIntent = nil
	d.inspectReceipt = nil
}
`,
		},
		{
			"internal/agents/reviews/extra.go",
			`package reviews

var forged = inspectReceipt{applied: true}
`,
		},
	} {
		root := candidateReviewFixture(
			t,
			map[string]string{test.path: test.source},
		)

		diagnostics, _ := inspectCandidateBoundaries(
			root,
			candidateReviewModule,
			[]byte(metadata),
		)
		if !candidateReviewHas(diagnostics, "ARCH604", test.path) {
			t.Fatalf("prepared authority/receipt escaped: %+v", diagnostics)
		}
	}

	path := "internal/app/extra.go"
	root := candidateReviewFixture(
		t,
		map[string]string{
			path: `package app

var output = "display only"
var applied = false
`,
		},
	)

	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(metadata),
	)
	if candidateReviewHas(diagnostics, "ARCH604", path) {
		t.Fatalf(
			"generic receipt names were globally reserved: %+v",
			diagnostics,
		)
	}

	if !candidateReviewHas(diagnostics, "ARCH602", path) {
		t.Fatal("unowned production source was accepted")
	}
}
