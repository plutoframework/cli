package sqlcheck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodMigration = `
CREATE TABLE audit_events (
    id bigserial PRIMARY KEY,
    org_id uuid NOT NULL,
    body text
);
ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE audit_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON audit_events
    USING (org_id = current_setting('app.tenant_id', true)::uuid);
CREATE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;
CREATE TRIGGER audit_events_no_update BEFORE UPDATE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
CREATE TRIGGER audit_events_no_delete BEFORE DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
`

func migrationConfig(t *testing.T, files map[string]string) Config {
	t.Helper()

	root := t.TempDir()
	if err := os.MkdirAll(
		filepath.Join(root, "db/migrations"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}

	for name, content := range files {
		path := filepath.Join(root, "db/migrations", name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return Config{
		Root:       root,
		SchemaDirs: []string{"db/migrations"},
		Tables: []Table{{
			Name:         "audit_events",
			TenantColumn: "org_id",
			AppendOnly:   true,
		}},
	}
}

func messages(diagnostics []Diagnostic) string {
	var out []string
	for _, d := range diagnostics {
		out = append(out, d.Message)
	}

	return strings.Join(out, "\n")
}

func TestCheckMigrationsAccepts(t *testing.T) {
	cfg := migrationConfig(t, map[string]string{"001_init.sql": goodMigration})

	got, err := CheckMigrations(cfg)
	if err != nil || len(got) != 0 {
		t.Fatalf("want clean, got %v %v", got, err)
	}
}

func TestCheckMigrationsAcrossFiles(t *testing.T) {
	cfg := migrationConfig(t, map[string]string{
		"001_table.sql": strings.Split(goodMigration, "ALTER TABLE")[0],
		"002_rls.sql": `ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY,
    FORCE ROW LEVEL SECURITY;
CREATE POLICY p ON audit_events FOR ALL TO app
    USING ((org_id = (SELECT current_setting('app.tenant_id')::uuid)))
    WITH CHECK (org_id = current_setting('app.tenant_id')::uuid);
`,
		"003_triggers.sql": "CREATE FUNCTION f() RETURNS trigger AS $x$ " +
			"BEGIN RAISE EXCEPTION 'no'; END; $x$ LANGUAGE plpgsql;\n" +
			"CREATE TRIGGER t BEFORE UPDATE OR DELETE ON audit_events " +
			"EXECUTE FUNCTION f();\n",
	})

	got, err := CheckMigrations(cfg)
	if err != nil || len(got) != 0 {
		t.Fatalf("want clean, got %v %v", got, err)
	}
}

func TestCheckMigrationsRejects(t *testing.T) {
	base := strings.Split(goodMigration, "ALTER TABLE")[0]
	cases := map[string]struct {
		sql  string
		want string
	}{
		"no rls": {
			base, "ENABLE and FORCE",
		},
		"enable only": {
			base + "ALTER TABLE audit_events ENABLE ROW LEVEL SECURITY;" +
				"CREATE POLICY p ON audit_events USING " +
				"(org_id = current_setting('app.tenant_id'));",
			"ENABLE and FORCE",
		},
		"no force later": {
			goodMigration + "ALTER TABLE audit_events " +
				"NO FORCE ROW LEVEL SECURITY;",
			"ENABLE and FORCE",
		},
		"disabled later": {
			goodMigration + "ALTER TABLE audit_events " +
				"DISABLE ROW LEVEL SECURITY;",
			"ENABLE and FORCE",
		},
		"no policy": {
			strings.Replace(
				goodMigration,
				"CREATE POLICY",
				"-- x\nSELECT 1; --",
				1,
			),
			"no tenant policy covering SELECT",
		},
		"using true": {
			strings.Replace(
				goodMigration,
				"org_id = current_setting('app.tenant_id', true)::uuid",
				"true",
				1,
			),
			"widens access",
		},
		"wrong setting": {
			strings.Replace(goodMigration, "app.tenant_id", "app.other", 1),
			"widens access",
		},
		"or in policy": {
			strings.Replace(
				goodMigration,
				"::uuid);",
				"::uuid OR org_id IS NULL);",
				1,
			),
			"widens access",
		},
		"wrong column": {
			strings.Replace(
				goodMigration,
				"USING (org_id",
				"USING (id",
				1,
			),
			"widens access",
		},
		"extra permissive policy": {
			goodMigration + "CREATE POLICY open ON audit_events " +
				"FOR SELECT USING (true);",
			"widens access",
		},
		"select only policy": {
			strings.Replace(
				goodMigration,
				"ON audit_events\n    USING",
				"ON audit_events FOR SELECT\n    USING",
				1,
			),
			"covering INSERT",
		},
		"policy dropped": {
			goodMigration + "DROP POLICY tenant_isolation ON audit_events;",
			"no tenant policy covering",
		},
		"table dropped": {
			goodMigration + "DROP TABLE audit_events;",
			"not created by any migration",
		},
		"table renamed": {
			goodMigration + "ALTER TABLE audit_events RENAME TO old;",
			"not created by any migration",
		},
		"column dropped": {
			goodMigration + "ALTER TABLE audit_events DROP COLUMN org_id;",
			"no tenant column",
		},
		"trigger dropped": {
			goodMigration + "DROP TRIGGER audit_events_no_delete " +
				"ON audit_events;",
			"BEFORE DELETE",
		},
		"trigger disabled": {
			goodMigration + "ALTER TABLE audit_events " +
				"DISABLE TRIGGER audit_events_no_update;",
			"BEFORE UPDATE",
		},
		"all triggers disabled": {
			goodMigration + "ALTER TABLE audit_events DISABLE TRIGGER ALL;",
			"BEFORE UPDATE",
		},
		"replica only trigger": {
			goodMigration + "ALTER TABLE audit_events " +
				"ENABLE REPLICA TRIGGER audit_events_no_update;",
			"BEFORE UPDATE",
		},
		"function returns new": {
			strings.Replace(
				goodMigration,
				"RAISE EXCEPTION 'audit_events is append-only';",
				"IF false THEN RAISE EXCEPTION 'x'; END IF; RETURN NEW;",
				1,
			),
			"always raises",
		},
		"function replaced": {
			goodMigration + "CREATE OR REPLACE FUNCTION " +
				"audit_events_immutable() RETURNS trigger AS $$ " +
				"BEGIN RETURN NEW; END; $$ LANGUAGE plpgsql;",
			"always raises",
		},
		"after trigger": {
			strings.ReplaceAll(goodMigration, "BEFORE", "AFTER"),
			"always raises",
		},
		"missing delete": {
			strings.Replace(
				goodMigration,
				"CREATE TRIGGER audit_events_no_delete BEFORE DELETE",
				"CREATE TRIGGER audit_events_no_delete BEFORE INSERT",
				1,
			),
			"BEFORE DELETE",
		},
		"down section ignored": {
			base + "-- +goose Down\n" + goodMigration[len(base):],
			"ENABLE and FORCE",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := migrationConfig(t, map[string]string{"001.sql": tc.sql})

			got, err := CheckMigrations(cfg)
			if err != nil {
				t.Fatal(err)
			}

			if !strings.Contains(messages(got), tc.want) {
				t.Fatalf("want %q in:\n%s", tc.want, messages(got))
			}

			for _, d := range got {
				if d.ID != "ARCH612" || d.Line < 1 || d.Repair == "" {
					t.Fatalf("malformed diagnostic %+v", d)
				}
			}
		})
	}
}

func TestCheckMigrationsNonAppendOnlySkipsTriggers(t *testing.T) {
	cfg := migrationConfig(t, map[string]string{
		"001.sql": strings.Split(goodMigration, "CREATE FUNCTION")[0],
	})
	cfg.Tables[0].AppendOnly = false

	got, err := CheckMigrations(cfg)
	if err != nil || len(got) != 0 {
		t.Fatalf("want clean, got %v %v", got, err)
	}
}

func TestCheckMigrationsMissingTable(t *testing.T) {
	cfg := migrationConfig(t, map[string]string{"001.sql": "SELECT 1;"})

	got, _ := CheckMigrations(cfg)
	if !strings.Contains(messages(got), "not created") {
		t.Fatalf("got %v", got)
	}
}

func TestCheckMigrationsCustomSetting(t *testing.T) {
	cfg := migrationConfig(t, map[string]string{"001.sql": goodMigration})
	cfg.TenantSetting = "app.org"

	got, _ := CheckMigrations(cfg)
	if !strings.Contains(messages(got), "widens access") {
		t.Fatalf("custom setting must be enforced: %v", got)
	}
}
