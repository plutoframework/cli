package project

import (
	"fmt"
	"go/format"
	"sort"
	"strings"
)

// permissionDeclarations emits capability contracts, never role assignments.
// App-owned Roles/Permissions functions customize grants and display metadata.
func permissionDeclarations(m Manifest) ([]byte, error) {
	var source strings.Builder
	source.WriteString(
		"package access\nimport rbac \"github.com/plutoframework/pluto-rbac\"\n",
	)

	permissions := map[string]bool{}

	for _, endpoint := range m.Endpoints {
		fmt.Fprintf(
			&source,
			"const %sPermission rbac.PermissionID = %q\n",
			symbol(endpoint),
			endpoint.Permission,
		)

		if endpoint.Permission != "" {
			permissions[endpoint.Permission] = true
		}
	}

	for _, a := range m.Agents {
		permissions[a.Permission] = true
	}

	keys := make([]string, 0, len(permissions))
	for permission := range permissions {
		keys = append(keys, permission)
	}

	sort.Strings(keys)
	source.WriteString(
		"func GeneratedPermissions() []rbac.Permission { return []rbac.Permission{\n",
	)

	for _, permission := range keys {
		fmt.Fprintf(
			&source,
			"{\nID:%q,\nName:%q,\nDescription:%q,\n},\n",
			permission,
			"Capability "+permission,
			"Access to operations requiring "+permission+".",
		)
	}

	source.WriteString("}\n}\n")

	return format.Source([]byte(source.String()))
}
