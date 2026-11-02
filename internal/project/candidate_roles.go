package project

import (
	"bytes"
	"fmt"
	"go/format"
)

func candidateRoleSource(m Manifest) ([]byte, error) {
	var source bytes.Buffer
	source.WriteString(
		"package access\nimport rbac \"github.com/plutoframework/" +
			"pluto-rbac\"\nfunc Roles()[]rbac.Role{return []rbac.Role{\n",
	)

	for _, role := range m.Roles {
		fmt.Fprintf(
			&source,
			"{\nID:%q,\nName:%q,\nDescription:%q,\n"+
				"Permissions:[]rbac.PermissionID{\n",
			role.ID,
			role.Name,
			role.Description,
		)

		for _, permission := range role.Permissions {
			fmt.Fprintf(&source, "%q,\n", permission)
		}

		source.WriteString("},\nInherits:[]rbac.RoleID{\n")

		for _, parent := range role.Inherits {
			fmt.Fprintf(&source, "%q,\n", parent)
		}

		source.WriteString("},\n},\n")
	}

	source.WriteString("}}\n")

	return format.Source(source.Bytes())
}
