package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestOldSchemaRejectedBeforeWrites(t *testing.T) {
	root := temporary(t)
	mustInvoke(
		t,
		root,
		"init",
		".",
		"--name",
		"migration",
		"--module",
		"example.com/migration",
		"--env-prefix",
		"MIGRATION_",
		"--no-mise",
		"--no-docker",
		"--no-fnox",
	)

	path := filepath.Join(root, "pluto.toml")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	old := strings.Replace(
		string(data),
		fmt.Sprintf("schema_version = %d", project.SchemaVersion),
		fmt.Sprintf("schema_version = %d", project.SchemaVersion-1),
		1,
	)
	if old == string(data) {
		t.Fatal("missing schema version")
	}

	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}

	before := snapshot(t, root)

	output, err := invoke(
		t,
		root,
		"add",
		"endpoint",
		"items",
		"List",
		"--method",
		"GET",
		"--path",
		"/api/items",
		"--name",
		"List items",
		"--description",
		"List authorized items.",
	)
	if err == nil ||
		!strings.Contains(
			err.Error(),
			fmt.Sprintf(
				"unsupported schema_version %d (need %d)",
				project.SchemaVersion-1,
				project.SchemaVersion,
			),
		) {
		t.Fatal(output, err)
	}

	assertUnchanged(t, before, snapshot(t, root))
}
