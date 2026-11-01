package project

import (
	"fmt"

	rbac "github.com/plutoframework/pluto-rbac"
)

// RoleContract declares policy grants, never actor or tenant assignments.
// Live membership remains a separate verified persistence boundary.
type RoleContract struct {
	ID          string   `toml:"id"`
	Name        string   `toml:"name"`
	Description string   `toml:"description"`
	Permissions []string `toml:"permissions"`
	Inherits    []string `toml:"inherits"`
}

// CompileRoleContracts uses the framework's actual catalog grammar and graph
// compiler. Repeated operation permissions are one capability; unknown grants,
// repeated role edges and cycles fail the same rules as runtime authorization.
func CompileRoleContracts(
	roles []RoleContract,
	permissions []string,
) (*rbac.Policy, error) {
	if len(roles) > 128 || len(permissions) > 1000 {
		return nil, fmt.Errorf(
			"%w: declared role catalog exceeds supported bounds",
			rbac.ErrInvalidPolicy,
		)
	}

	definition := rbac.Definition{}

	seen := map[string]bool{}
	for _, permission := range permissions {
		if seen[permission] {
			continue
		}

		seen[permission] = true
		definition.Permissions = append(definition.Permissions, rbac.Permission{
			ID: rbac.PermissionID(
				permission,
			),
			Name:        "Capability " + permission,
			Description: "Access to operations requiring " + permission + ".",
		})
	}

	for _, role := range roles {
		if len(role.ID) > 128 || len(role.Name) > 256 ||
			len(role.Description) > 1024 ||
			len(role.Permissions) > 1000 ||
			len(role.Inherits) > 128 {
			return nil, fmt.Errorf(
				"%w: declared role %s exceeds supported metadata bounds",
				rbac.ErrInvalidPolicy,
				role.ID,
			)
		}

		value := rbac.Role{
			ID:          rbac.RoleID(role.ID),
			Name:        role.Name,
			Description: role.Description,
		}
		for _, permission := range role.Permissions {
			value.Permissions = append(
				value.Permissions,
				rbac.PermissionID(permission),
			)
		}

		for _, parent := range role.Inherits {
			value.Inherits = append(value.Inherits, rbac.RoleID(parent))
		}

		definition.Roles = append(definition.Roles, value)
	}

	return rbac.New(definition)
}
