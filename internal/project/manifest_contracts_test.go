package project

import (
	"strings"
	"testing"
)

func manifestContractFixture(t *testing.T) Manifest {
	t.Helper()

	resources, contracts, effects := contractFixture()
	m := Manifest{
		Boundary:           boundaryProfileFixture(t),
		Capabilities:       Capabilities{Tenancy: "multi"},
		Resources:          resources,
		OperationContracts: contracts,
		Effects:            effects,
	}
	permissions := []string{}
	seen := map[string]bool{}

	for _, contract := range contracts {
		group, action, _ := strings.Cut(contract.ID, ".")

		m.Operations = append(
			m.Operations,
			Operation{
				Group:      group,
				Action:     action,
				Permission: contract.Permission,
				Public:     contract.Public,
				Write: contract.Kind == "create" ||
					contract.Kind == "transition" ||
					contract.Kind == "effect",
				Collection:   contract.Kind == "list",
				Idempotency:  contract.Idempotency,
				ReplayReason: contract.ExemptionReason,
			},
		)
		if !seen[contract.Permission] {
			permissions = append(permissions, contract.Permission)
			seen[contract.Permission] = true
		}
	}

	m.Roles = []RoleContract{
		{
			ID:          "operator",
			Name:        "Operator",
			Description: "Capabilities explicitly selected for this test.",
			Permissions: permissions,
		},
	}
	m.Jobs = []Job{
		{
			Group:       "delivery",
			Action:      "Notify",
			Kind:        effects[0].Kind,
			Operation:   effects[0].Operation,
			MaxAttempts: effects[0].MaxAttempts,
		},
		{
			Group:       "delivery",
			Action:      "Approve",
			Kind:        "invoice.approve",
			Operation:   "sales.Approve",
			MaxAttempts: 3,
		},
	}
	m.Endpoints = []Endpoint{
		{
			Group:          "sales",
			Action:         "Approve",
			Operation:      "sales.Approve",
			Name:           "Approve invoice",
			Description:    "Apply the declared lifecycle transition.",
			Method:         "POST",
			Path:           "/api/invoices/{id}",
			ResourceSource: "path:id",
		},
	}
	m.Agents = []Agent{
		{
			Group:     "assistant",
			Action:    "Approve",
			Operation: "sales.Approve",
			Provider:  "scripted",
			Approval:  "host",
		},
	}

	return m
}

func TestManifestContractsJoinAllEntryAuthority(t *testing.T) {
	m := manifestContractFixture(t)
	if err := ValidateManifestContracts(m); err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		kind, method, path, source string
	}{
		{"load", "GET", "/api/invoices/{id}", "path:id"},
		{"load", "GET", "/api/invoices", "query:id"},
		{"load", "POST", "/api/invoices/read", "body"},
		{"list", "GET", "/api/invoices", "collection"},
		{"export", "GET", "/api/invoices/export", "collection"},
	} {
		for _, contract := range m.OperationContracts {
			if contract.Kind != test.kind {
				continue
			}

			group, action, _ := strings.Cut(contract.ID, ".")

			m.Endpoints = []Endpoint{
				{
					Group:     group,
					Action:    action,
					Operation: contract.ID,
					Name:      "Read invoice",
					Description: "Read through a declared scoped data " +
						"contract.",
					Method:         test.method,
					Path:           test.path,
					ResourceSource: test.source,
				},
			}
			if err := ValidateManifestContracts(m); err != nil {
				t.Fatalf("%s %s: %v", test.kind, test.source, err)
			}
		}
	}

	m.Agents = []Agent{
		{
			Group:     "assistant",
			Action:    "Read",
			Operation: "sales.Read",
			Provider:  "scripted",
		},
	}
	if err := ValidateManifestContracts(m); err != nil {
		t.Fatal(err)
	}
}

func TestManifestContractsRejectIndependentAuthorityDrift(t *testing.T) {
	for _, test := range []struct {
		name, want string
		change     func(*Manifest)
	}{
		{
			"missing contract",
			"exactly one",
			func(m *Manifest) {
				m.OperationContracts = m.OperationContracts[1:]
			},
		},
		{
			"unsupported credential ingress",
			"session-only identity ingress",
			func(m *Manifest) {
				m.Capabilities.LocalCredentials = true
			},
		},
		{
			"unsupported OIDC ingress",
			"session-only identity ingress",
			func(m *Manifest) {
				m.Capabilities.OIDC = true
			},
		},
		{"orphan contract", "undeclared operation", func(m *Manifest) {
			copy := m.OperationContracts[0]
			copy.ID = "sales.Orphan"
			m.OperationContracts = append(m.OperationContracts, copy)
		}},
		{
			"permission disagreement",
			"disagree",
			func(m *Manifest) {
				m.OperationContracts[0].Permission = "invoice:approve"
			},
		},
		{
			"write disagreement",
			"disagree",
			func(m *Manifest) {
				m.Operations[3].Write = false
				m.Operations[3].Idempotency = "none"
			},
		},
		{
			"collection disagreement",
			"disagree",
			func(m *Manifest) {
				m.Operations[1].Collection = false
			},
		},
		{
			"exemption disagreement",
			"disagree",
			func(m *Manifest) {
				m.Operations[5].ReplayReason = "A different unverified " +
					"delivery guarantee."
			},
		},
		{
			"unknown role grant",
			"unknown permission",
			func(m *Manifest) {
				m.Roles[0].Permissions = append(
					m.Roles[0].Permissions,
					"invoice:destroy",
				)
			},
		},
		{
			"entry permission override",
			"cannot override",
			func(m *Manifest) {
				m.Endpoints[0].Permission = "invoice:read"
			},
		},
		{
			"entry public override",
			"cannot override",
			func(m *Manifest) {
				m.Endpoints[0].Public = true
			},
		},
		{
			"entry replay override",
			"replay contract",
			func(m *Manifest) {
				m.Endpoints[0].Idempotency = "exempt"
			},
		},
		{
			"route-only feature",
			"operation-owned shared gate",
			func(m *Manifest) {
				m.Endpoints[0].Feature = "invoice.approve"
			},
		},
		{
			"missing resource source",
			"explicit",
			func(m *Manifest) {
				m.Endpoints[0].ResourceSource = ""
			},
		},
		{
			"wrong resource path",
			"one declared",
			func(m *Manifest) {
				m.Endpoints[0].ResourceSource = "path:other"
			},
		},
		{
			"unused resource path",
			"unused path",
			func(m *Manifest) {
				m.Endpoints[0].ResourceSource = "body"
			},
		},
		{"unsupported DELETE body", "POST, PUT or PATCH", func(m *Manifest) {
			m.Endpoints[0].Method = "DELETE"
			m.Endpoints[0].Path = "/api/invoices"
			m.Endpoints[0].ResourceSource = "body"
		}},
		{"unsafe resource query", "safe explicit", func(m *Manifest) {
			m.Endpoints[0].Path = "/api/invoices"
			m.Endpoints[0].ResourceSource = "query:id&tenant"
		}},
		{
			"worker unknown target",
			"protected declared",
			func(m *Manifest) {
				m.Jobs[0].Operation = "delivery.Missing"
			},
		},
		{
			"missing effect binding",
			"matching managed",
			func(m *Manifest) {
				m.Jobs = m.Jobs[1:]
			},
		},
		{
			"orphan effect operation",
			"transactional producer",
			func(m *Manifest) {
				m.Effects = nil
				m.OperationContracts[4].Effects = nil
				m.Jobs = m.Jobs[1:]
			},
		},
		{
			"effect budget disagreement",
			"retry budget",
			func(m *Manifest) {
				m.Jobs[0].MaxAttempts++
			},
		},
		{
			"unbounded worker",
			"bounded max_attempts",
			func(m *Manifest) {
				m.Jobs[0].MaxAttempts = 0
			},
		},
		{
			"effect target disagreement",
			"target or retry",
			func(m *Manifest) {
				m.Jobs[0].Operation = "sales.Approve"
			},
		},
		{
			"effect kind disagreement",
			"no declared delivery",
			func(m *Manifest) {
				m.Jobs[0].Kind = "invoice.unknown"
			},
		},
		{
			"worker collection",
			"worker collections",
			func(m *Manifest) {
				m.Jobs[1].Operation = "sales.List"
			},
		},
		{
			"tool unknown target",
			"protected declared",
			func(m *Manifest) {
				m.Agents[0].Operation = "sales.Unknown"
			},
		},
		{
			"tool permission override",
			"cannot override",
			func(m *Manifest) {
				m.Agents[0].Permission = "invoice:read"
			},
		},
		{
			"tool no host approval",
			"host approval",
			func(m *Manifest) {
				m.Agents[0].Approval = "none"
			},
		},
		{
			"tool model approval",
			"host approval",
			func(m *Manifest) {
				m.Agents[0].Approval = "model"
			},
		},
		{
			"tool read approval claim",
			"unsupported approval",
			func(m *Manifest) {
				m.Agents[0].Operation = "sales.Read"
			},
		},
		{
			"effect tool",
			"tools support",
			func(m *Manifest) {
				m.Agents[0].Operation = "delivery.Notify"
			},
		},
		{
			"collection tool",
			"tools support",
			func(m *Manifest) {
				m.Agents[0].Operation = "sales.List"
			},
		},
		{"effect HTTP", "managed worker", func(m *Manifest) {
			m.Endpoints[0].Group = "delivery"
			m.Endpoints[0].Action = "Notify"
			m.Endpoints[0].Operation = "delivery.Notify"
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := manifestContractFixture(t)
			test.change(&m)

			if err := ValidateManifestContracts(
				m,
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %s rejection: %v", test.want, err)
			}
		})
	}
}

func TestManifestContractsRejectCaseFoldedFiles(t *testing.T) {
	for _, kind := range []string{
		"operation",
		"resource",
		"HTTP entry",
		"worker entry",
		"tool entry",
	} {
		t.Run(kind, func(t *testing.T) {
			m := manifestContractFixture(t)

			switch kind {
			case "operation":
				copy := m.Operations[0]
				copy.Action = strings.ToUpper(copy.Action)
				m.Operations = append(m.Operations, copy)
			case "resource":
				copy := m.Resources[0]
				copy.Name = strings.ToUpper(copy.Name)
				copy.Table = "another"
				copy.Namespace = "another"
				m.Resources = append(m.Resources, copy)
			case "HTTP entry":
				copy := m.Endpoints[0]
				copy.Action = strings.ToUpper(copy.Action)
				m.Endpoints = append(m.Endpoints, copy)
			case "worker entry":
				copy := m.Jobs[0]
				copy.Action = strings.ToUpper(copy.Action)
				copy.Kind = "another.kind"
				m.Jobs = append(m.Jobs, copy)
			case "tool entry":
				copy := m.Agents[0]
				copy.Action = strings.ToUpper(copy.Action)
				m.Agents = append(m.Agents, copy)
			}

			if err := ValidateManifestContracts(
				m,
			); err == nil ||
				!strings.Contains(err.Error(), "filename collision") {
				t.Fatalf("colliding %s accepted: %v", kind, err)
			}
		})
	}
}

func TestManifestContractsBootstrapAndPublishedDrafts(t *testing.T) {
	m := Manifest{
		Boundary:     boundaryProfileFixture(t),
		Capabilities: Capabilities{Tenancy: "multi"},
	}
	if err := ValidateManifestContracts(m); err != nil {
		t.Fatalf("empty candidate bootstrap: %v", err)
	}

	m.Resources = []ResourceContract{{Group: "sales", Name: "Invoice"}}
	if err := ValidateManifestContracts(
		m,
	); err == nil ||
		!strings.Contains(err.Error(), "independent operations") {
		t.Fatalf("partial candidate accepted: %v", err)
	}

	m = Manifest{
		Operations: []Operation{
			{
				Group:       "sales",
				Action:      "Read",
				Permission:  "invoice:read",
				Idempotency: "none",
			},
		},
	}
	if err := ValidateManifestContracts(m); err != nil {
		t.Fatalf("published structural draft rejected: %v", err)
	}

	m.Resources = []ResourceContract{{Group: "sales", Name: "Invoice"}}
	if err := ValidateManifestContracts(
		m,
	); err == nil ||
		!strings.Contains(err.Error(), "published compatibility") {
		t.Fatalf("unsupported candidate semantics accepted: %v", err)
	}
}

func TestManifestRejectsGeneratedOperationSymbolCollisions(t *testing.T) {
	for _, name := range []string{
		"CreateBoundary",
		"CreateIntent",
		"NewCreate",
	} {
		m := Manifest{
			Boundary: BoundaryProfile{Mode: "candidate"},
			Operations: []Operation{
				{Group: "sales", Action: "Create"},
				{Group: "sales", Action: name},
			},
		}
		if err := validateContractFilenames(
			m,
		); err == nil ||
			!strings.Contains(err.Error(), "Go identifier collision") {
			t.Fatalf(
				"accepted generated identifier collision %s: %v",
				name,
				err,
			)
		}
	}

	m := Manifest{
		Boundary: BoundaryProfile{Mode: "candidate"},
		Operations: []Operation{
			{Group: "sales", Action: "Create"},
			{Group: "billing", Action: "CreateBoundary"},
		},
	}
	if err := validateContractFilenames(m); err != nil {
		t.Fatal(err)
	}
}
