package project

import (
	"errors"
	"strings"
	"testing"

	rbac "github.com/plutoframework/pluto-rbac"
)

func roleContractFixture() []RoleContract {
	return []RoleContract{
		{
			ID:          "reader",
			Name:        "Reader",
			Description: "Read visible records.",
			Permissions: []string{"record:read"},
		},
		{
			ID:          "approver",
			Name:        "Approver",
			Description: "Approve another actor's records.",
			Permissions: []string{"record:approve"},
			Inherits:    []string{"reader"},
		},
	}
}

func TestRoleContractsUseCanonicalPermissionGraph(t *testing.T) {
	policy, err := CompileRoleContracts(
		roleContractFixture(),
		[]string{"record:read", "record:approve", "record:read"},
	)
	if err != nil {
		t.Fatal(err)
	}

	if !policy.Allows([]rbac.RoleID{"approver"}, "record:read") ||
		!policy.Allows([]rbac.RoleID{"approver"}, "record:approve") {
		t.Fatal("declared inheritance did not grant its capabilities")
	}

	if policy.Allows(nil, "record:approve") ||
		policy.Allows([]rbac.RoleID{"reader"}, "record:approve") ||
		policy.Allows([]rbac.RoleID{"approver", "unknown"}, "record:approve") {
		t.Fatal("catalog conferred undeclared or unassigned authority")
	}

	empty, err := CompileRoleContracts(nil, nil)
	if err != nil || empty.Allows([]rbac.RoleID{"approver"}, "record:approve") {
		t.Fatalf("empty catalog did not fail closed: %v", err)
	}
}

func TestRoleContractsRejectInvalidDeclarations(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func([]RoleContract) []RoleContract
	}{
		{"unknown grant", func(r []RoleContract) []RoleContract { r[0].Permissions = []string{"record:destroy"}; return r }},
		{"duplicate grant", func(r []RoleContract) []RoleContract {
			r[0].Permissions = append(r[0].Permissions, "record:read")
			return r
		}},
		{"unknown parent", func(r []RoleContract) []RoleContract { r[1].Inherits = []string{"missing"}; return r }},
		{"duplicate parent", func(r []RoleContract) []RoleContract { r[1].Inherits = append(r[1].Inherits, "reader"); return r }},
		{"cycle", func(r []RoleContract) []RoleContract { r[0].Inherits = []string{"approver"}; return r }},
		{"self cycle", func(r []RoleContract) []RoleContract { r[0].Inherits = []string{"reader"}; return r }},
		{"duplicate role", func(r []RoleContract) []RoleContract { return append(r, r[0]) }},
		{"invalid identifier", func(r []RoleContract) []RoleContract { r[0].ID = "Admin!"; return r }},
		{"missing name", func(r []RoleContract) []RoleContract { r[0].Name = ""; return r }},
		{"control metadata", func(r []RoleContract) []RoleContract { r[0].Description = "Allow\nanything"; return r }},
		{"oversized metadata", func(r []RoleContract) []RoleContract { r[0].Description = strings.Repeat("x", 1025); return r }},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := CompileRoleContracts(
				test.change(roleContractFixture()),
				[]string{"record:read", "record:approve"},
			); !errors.Is(
				err,
				rbac.ErrInvalidPolicy,
			) {
				t.Fatalf("expected canonical invalid policy: %v", err)
			}
		})
	}

	if _, err := CompileRoleContracts(
		nil,
		[]string{"!!!!!!"},
	); !errors.Is(
		err,
		rbac.ErrInvalidPolicy,
	) {
		t.Fatalf("invalid permission grammar accepted: %v", err)
	}
}
