package project

import (
	"strings"
	"testing"
)

// txFixture adds an approvals group whose Approve operation writes the audit
// port's AppendTx inside its own transaction.
func txFixture(t *testing.T) Manifest {
	t.Helper()

	m := seamFixture(t)
	m.Tables = append(m.Tables, Table{
		Group:        "approvals",
		Name:         "approval_requests",
		TenantColumn: "org_id",
		RowSecurity:  true,
	})
	m.Queries = append(m.Queries, QuerySet{
		Group:  "approvals",
		Files:  []string{"db/queries/approvals/*.sql"},
		Tables: []string{"approval_requests"},
	})
	m.Ports[0].Methods = append(
		m.Ports[0].Methods,
		"AppendTx(scope.Tx, models.Event) error",
	)
	m.Ports[0].TxMethods = []string{"AppendTx"}
	m.Ports[0].TxCallers = []string{"approvals.Approve"}
	m.Ports = append(m.Ports, Port{
		ID:      "approvals.Requests",
		Adapter: "approvals.RequestStore",
		Methods: []string{"ApproveTx(scope.Tx, models.ApproveInput) error"},
	})
	m.Models = append(m.Models, Model{
		ID:     "approvals.ApproveInput",
		Fields: []string{"id:string"},
	})
	m.Roles[0].Permissions = append(
		m.Roles[0].Permissions,
		"approvals.write",
	)
	m.Operations = append(m.Operations, Operation{
		Group:          "approvals",
		Action:         "Approve",
		Permission:     "approvals.write",
		Write:          true,
		Idempotency:    "exempt",
		ReplayReason:   "Approving is a one-way transition.",
		Body:           BodyAuthored,
		Resource:       "approvals.requests",
		ResourcePolicy: "tenant",
		Ports:          []string{"approvals.Requests", "audit.Events"},
		Input:          "approvals.ApproveInput",
	})

	return m
}

func TestTxWriteManifestValidates(t *testing.T) {
	if err := validateSeams(txFixture(t)); err != nil {
		t.Fatal(err)
	}
}

func TestTxWriteManifestRejections(t *testing.T) {
	approve := func(m *Manifest) *Operation {
		return &m.Operations[len(m.Operations)-1]
	}

	for name, tc := range map[string]struct {
		edit func(*Manifest)
		want string
	}{
		"tx methods without callers": {
			func(m *Manifest) {
				m.Ports[0].TxCallers = nil
			},
			"require tx_callers",
		},
		"callers without tx methods": {
			func(m *Manifest) {
				m.Ports[0].TxMethods = nil
			},
			"tx_callers require tx_methods",
		},
		"tx method takes an execution": {
			func(m *Manifest) {
				m.Ports[0].TxMethods = []string{"Scan"}
			},
			"takes scope.Tx first",
		},
		"undeclared tx method": {
			func(m *Manifest) {
				m.Ports[0].TxMethods = []string{"Nope"}
			},
			"takes scope.Tx first",
		},
		"unknown caller": {
			func(m *Manifest) {
				m.Ports[0].TxCallers = []string{"approvals.Missing"}
			},
			"must be a declared authored operation",
		},
		"caller is not a write": {
			func(m *Manifest) {
				approve(m).Write = false
				approve(m).Idempotency = "none"
				approve(m).ReplayReason = ""
			},
			"cross-group transactional write",
		},
		"operation not listed as caller": {
			func(m *Manifest) {
				m.Ports[0].TxCallers = []string{"audit.Append"}
				m.Operations = append(m.Operations, Operation{
					Group:          "audit",
					Action:         "Append",
					Permission:     "audit.read",
					Write:          true,
					Idempotency:    "exempt",
					ReplayReason:   "dedupe upstream",
					Body:           BodyAuthored,
					Resource:       "audit.events",
					ResourcePolicy: "tenant",
					Ports:          []string{"audit.Events"},
					Input:          "audit.Entry",
				})
			},
			"does not list approvals.Approve in tx_callers",
		},
		"port without tx methods used across groups": {
			func(m *Manifest) {
				m.Ports[0].TxMethods, m.Ports[0].TxCallers = nil, nil
			},
			"another group",
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := txFixture(t)
			tc.edit(&m)

			err := validateSeams(m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTxWriteGeneratesNarrowPortAndWiring(t *testing.T) {
	m := txFixture(t)

	files, err := candidateAdapterFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	port := string(files["internal/ports/audit/events_port_gen.go"])
	for _, want := range []string{
		"type EventsTx interface {",
		"AppendTx(context.Context, scope.Tx, models.Event) error",
		"func NewEventsTx(inner Events) EventsTx",
	} {
		if !strings.Contains(port, want) {
			t.Fatalf("port file lacks %q:\n%s", want, port)
		}
	}

	narrow := port[strings.Index(port, "type EventsTx interface {"):]
	narrow = narrow[:strings.Index(narrow, "}\n")]

	if strings.Contains(narrow, "Query") ||
		strings.Contains(narrow, "Append(") {
		t.Fatalf("narrow interface exposes execution methods:\n%s", narrow)
	}

	wiring := string(files[candidateAdaptersFile])
	for _, want := range []string{
		"AuditEventsTx auditports.EventsTx",
		"DB scope.DB",
		"adapters.AuditEventsTx = auditports.NewEventsTx(adapters.AuditEvents)",
	} {
		if !strings.Contains(wiring, want) {
			t.Fatalf("wiring lacks %q:\n%s", want, wiring)
		}
	}
}

func TestTxWriteContractGivesCallerOnlyNarrowPort(t *testing.T) {
	m := txFixture(t)

	files, err := candidateAuthoredManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	contract := string(files["internal/logic/approvals/contract_gen.go"])
	for _, want := range []string{
		`auditports "example.com/gryt/internal/ports/audit"`,
		"ports.Requests",
		"auditports.EventsTx",
		"TxDB scope.DB",
	} {
		if !strings.Contains(contract, want) {
			t.Fatalf("contract lacks %q:\n%s", want, contract)
		}
	}

	if strings.Contains(contract, "auditports.Events\n") {
		t.Fatal("caller received the full audit port")
	}
}
