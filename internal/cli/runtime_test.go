package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"schema_version = 5",
		"schema_version = 4",
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
			"unsupported schema_version 4 (need 5)",
		) {
		t.Fatal(output, err)
	}

	assertUnchanged(t, before, snapshot(t, root))
}
