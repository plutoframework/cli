package project

import (
	"strings"
	"testing"

	rbac "github.com/plutoframework/pluto-rbac"
)

func TestPublishedPermissionValidation(t *testing.T) {
	for _, permission := range []string{
		"purchases:read",
		"purchases.read",
		"purchases_read",
		"purchases-read",
		"purchases:*",
		"purchases::read",
		"Purchases:read",
		"purchases:",
	} {
		endpoint := Endpoint{
			Group:       "purchases",
			Action:      "Read",
			Name:        "Read purchases",
			Description: "Read authorized purchases.",
			Method:      "GET",
			Path:        "/api/purchases",
			Permission:  permission,
		}

		_, libraryError := rbac.New(rbac.Definition{
			Permissions: []rbac.Permission{
				{
					ID:          rbac.PermissionID(permission),
					Name:        "Read purchases",
					Description: "Read authorized purchases.",
				},
			},
		})
		if err := endpoint.Validate(
			false,
		); (err == nil) != (libraryError == nil) {
			t.Errorf(
				"permission %q diverged from published RBAC: %v",
				permission,
				err,
			)
		}
	}
}

func TestGeneratedRBACDependency(t *testing.T) {
	manifest := Manifest{
		Project: Project{Module: "example.com/dependency-test"},
	}

	files := ToolFiles(manifest)
	if !strings.Contains(
		string(files["go.mod"]),
		"github.com/plutoframework/pluto-rbac "+RBACVersion,
	) ||
		strings.Contains(string(files["go.mod"]), "replace ") {
		t.Fatal("generated app does not pin the published RBAC module")
	}

	if !strings.Contains(
		string(files["go.sum"]),
		"github.com/plutoframework/pluto-rbac "+RBACVersion+" h1:",
	) ||
		!strings.Contains(
			string(files["go.sum"]),
			"github.com/plutoframework/pluto-rbac "+RBACVersion+"/go.mod h1:",
		) {
		t.Fatal("generated app is missing published RBAC module checksums")
	}
}
