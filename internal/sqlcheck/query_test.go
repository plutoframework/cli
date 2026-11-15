package sqlcheck

import (
	"strings"
	"testing"
)

func testEnv() queryEnv {
	return queryEnv{
		tables: map[string]Table{
			"audit_events": {
				Name:         "audit_events",
				TenantColumn: "org_id",
			},
			"audit_tags": {Name: "audit_tags", TenantColumn: "org_id"},
			"countries":  {Name: "countries"},
		},
		allowed: map[string]bool{
			"audit_events": true,
			"audit_tags":   true,
			"countries":    true,
		},
		set: "audit",
	}
}

func params(count int) []queryParam {
	var out []queryParam
	for i := 1; i <= count; i++ {
		out = append(out, queryParam{Number: i, NotNull: true})
	}

	return out
}

func TestAnalyzeQueryAccepts(t *testing.T) {
	cases := map[string]string{
		"select":  "SELECT id FROM audit_events WHERE org_id = $1",
		"reverse": "SELECT id FROM audit_events WHERE $1 = org_id",
		"and": "SELECT id FROM audit_events WHERE org_id = $1 " +
			"AND (kind = $2 OR kind = $3)",
		"cast": "SELECT id FROM audit_events WHERE org_id = $1::uuid",
		"parens": "SELECT id FROM audit_events WHERE (org_id = $1) " +
			"AND id > $2",
		"between": "SELECT id FROM audit_events WHERE id BETWEEN $2 " +
			"AND $3 AND org_id = $1",
		"qualified": "SELECT e.id FROM audit_events AS e " +
			"WHERE e.org_id = $1",
		"schema": "SELECT id FROM public.audit_events " +
			"WHERE org_id = $1 ORDER BY id LIMIT 10",
		"quoted": `SELECT id FROM "audit_events" WHERE "org_id" = $1`,
		"join": "SELECT e.id FROM audit_events e JOIN audit_tags t " +
			"ON t.event_id = e.id WHERE e.org_id = $1 AND t.org_id = $1",
		"global join": "SELECT e.id FROM audit_events e " +
			"JOIN countries c ON c.id = e.country " +
			"WHERE e.org_id = $1",
		"global": "SELECT id FROM countries",
		"distinct from": "SELECT id FROM audit_events WHERE org_id = $1 " +
			"AND kind IS DISTINCT FROM $2",
		"insert": "INSERT INTO audit_events (org_id, body) " +
			"VALUES ($1, $2)",
		"insert cast": "INSERT INTO audit_events (body, org_id) " +
			"VALUES ($2, $1::uuid) RETURNING id",
		"insert rows": "INSERT INTO audit_events (org_id, body) " +
			"VALUES ($1, $2), ($1, $3)",
		"insert nothing": "INSERT INTO audit_events (org_id) VALUES ($1) " +
			"ON CONFLICT DO NOTHING",
		"update": "UPDATE audit_events SET body = $2 " +
			"WHERE org_id = $1 AND id = $3",
		"delete": "DELETE FROM audit_events WHERE org_id = $1 AND id = $2",
		"trailing semicolon": "SELECT id FROM audit_events " +
			"WHERE org_id = $1;",
		"string with or": "SELECT id FROM audit_events " +
			"WHERE org_id = $1 AND kind = 'a or b'",
	}

	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			q := query{
				Name: "Q", File: "q.sql", Line: 3, Text: text,
				Params: params(3),
			}
			if got := analyzeQuery(q, testEnv()); len(got) != 0 {
				t.Fatalf("want accept, got %v", got)
			}
		})
	}
}

func TestAnalyzeQueryRejects(t *testing.T) {
	cases := map[string]struct {
		text string
		want string
	}{
		"no tenant": {
			"SELECT id FROM audit_events WHERE id = $1",
			"does not bind",
		},
		"no where": {
			"SELECT id FROM audit_events",
			"does not bind",
		},
		"or branch": {
			"SELECT id FROM audit_events WHERE org_id = $1 OR kind = $2",
			"OR at the top level",
		},
		"or leading": {
			"SELECT id FROM audit_events WHERE kind = $2 OR org_id = $1",
			"OR at the top level",
		},
		"tenant inside or group": {
			"SELECT id FROM audit_events WHERE (org_id = $1 OR id = $2)",
			"does not bind",
		},
		"literal": {
			"SELECT id FROM audit_events WHERE org_id = 'abc'",
			"does not bind",
		},
		"column to column": {
			"SELECT id FROM audit_events WHERE org_id = id AND id = $1",
			"does not bind",
		},
		"function arg": {
			"SELECT id FROM audit_events WHERE org_id = lower($1)",
			"does not bind",
		},
		"inequality": {
			"SELECT id FROM audit_events WHERE org_id <> $1",
			"does not bind",
		},
		"not equal wrapper": {
			"SELECT id FROM audit_events WHERE NOT (org_id = $1)",
			"does not bind",
		},
		"is true": {
			"SELECT id FROM audit_events WHERE (org_id = $1) IS NOT TRUE",
			"does not bind",
		},
		"any array": {
			"SELECT id FROM audit_events WHERE org_id = ANY($1::uuid[])",
			"does not bind",
		},
		"in list": {
			"SELECT id FROM audit_events WHERE org_id IN ($1, $2)",
			"does not bind",
		},
		"wrong alias": {
			"SELECT e.id FROM audit_events e JOIN audit_tags t " +
				"ON t.event_id = e.id WHERE t.org_id = $1",
			"audit_events.org_id",
		},
		"join not bound": {
			"SELECT e.id FROM audit_events e JOIN audit_tags t " +
				"ON t.event_id = e.id AND t.org_id = $1 " +
				"WHERE e.org_id = $1",
			"audit_tags.org_id",
		},
		"ambiguous unqualified": {
			"SELECT e.id FROM audit_events e JOIN audit_tags t " +
				"ON t.event_id = e.id WHERE org_id = $1",
			"does not bind",
		},
		"undeclared join": {
			"SELECT e.id FROM audit_events e JOIN secrets s " +
				"ON s.id = e.id WHERE e.org_id = $1",
			"not declared",
		},
		"undeclared from": {
			"SELECT id FROM secrets",
			"not declared",
		},
		"comma join undeclared": {
			"SELECT e.id FROM audit_events e, secrets s " +
				"WHERE e.org_id = $1",
			"not declared",
		},
		"cte": {
			"WITH x AS (SELECT 1) SELECT id FROM audit_events " +
				"WHERE org_id = $1",
			"common table expression",
		},
		"subquery where": {
			"SELECT id FROM audit_events WHERE org_id = $1 AND " +
				"id IN (SELECT id FROM secrets)",
			"subquery",
		},
		"subquery from": {
			"SELECT id FROM (SELECT * FROM audit_events) x " +
				"WHERE org_id = $1",
			"subquery",
		},
		"scalar subquery select list": {
			"SELECT (SELECT 1 FROM secrets) FROM audit_events " +
				"WHERE org_id = $1",
			"subquery",
		},
		"union": {
			"SELECT id FROM audit_events WHERE org_id = $1 UNION " +
				"SELECT id FROM audit_events WHERE org_id = $2",
			"UNION",
		},
		"function in from": {
			"SELECT * FROM generate_series(1, 3) WHERE true",
			"function or subquery",
		},
		"lateral": {
			"SELECT 1 FROM audit_events e, LATERAL f(e.id) " +
				"WHERE e.org_id = $1",
			"LATERAL",
		},
		"insert no tenant": {
			"INSERT INTO audit_events (body) VALUES ($1)",
			"without the tenant column",
		},
		"insert literal tenant": {
			"INSERT INTO audit_events (org_id, body) VALUES ('x', $1)",
			"not a sqlc.arg",
		},
		"insert second row literal": {
			"INSERT INTO audit_events (org_id, body) VALUES ($1, $2), " +
				"('x', $3)",
			"not a sqlc.arg",
		},
		"insert select": {
			"INSERT INTO audit_events (org_id) SELECT org_id FROM " +
				"audit_events WHERE org_id = $1",
			"not INSERT ... VALUES",
		},
		"insert no columns": {
			"INSERT INTO audit_events VALUES ($1, $2)",
			"explicit column list",
		},
		"insert default": {
			"INSERT INTO audit_events (org_id) VALUES (DEFAULT)",
			"not a sqlc.arg",
		},
		"insert conflict update": {
			"INSERT INTO audit_events (org_id) VALUES ($1) " +
				"ON CONFLICT (id) DO UPDATE SET body = 'x'",
			"DO UPDATE",
		},
		"update sets tenant": {
			"UPDATE audit_events SET org_id = $2 WHERE org_id = $1",
			"assigns tenant column",
		},
		"update tuple sets tenant": {
			"UPDATE audit_events SET (body, org_id) = ($2, $3) " +
				"WHERE org_id = $1",
			"assigns tenant column",
		},
		"update from": {
			"UPDATE audit_events SET body = $2 FROM secrets " +
				"WHERE audit_events.org_id = $1",
			"UPDATE ... FROM",
		},
		"update no where": {
			"UPDATE audit_events SET body = $1",
			"does not bind",
		},
		"delete using": {
			"DELETE FROM audit_events USING secrets WHERE org_id = $1",
			"DELETE ... USING",
		},
		"delete no where": {
			"DELETE FROM audit_events",
			"does not bind",
		},
		"truncate": {
			"TRUNCATE audit_events",
			"statements are not allowed",
		},
		"execute": {
			"SELECT id FROM audit_events WHERE org_id = $1 AND " +
				"query_to_xml('select 1', true, true, '') IS NOT NULL",
			"dynamic",
		},
		"set_config": {
			"SELECT set_config('app.tenant_id', $2, false) " +
				"FROM audit_events WHERE org_id = $1",
			"dynamic",
		},
		"two statements": {
			"SELECT id FROM audit_events WHERE org_id = $1; " +
				"DELETE FROM audit_events WHERE org_id = $1",
			"more than one statement",
		},
		"drop": {
			"DROP TABLE audit_events",
			"statements are not allowed",
		},
		"unterminated": {
			"SELECT 'abc FROM audit_events",
			"unterminated",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			q := query{
				Name: "Q", File: "q.sql", Line: 3, Text: tc.text,
				Params: params(3),
			}

			got := analyzeQuery(q, testEnv())
			if len(got) == 0 {
				t.Fatalf("want rejection containing %q, got none", tc.want)
			}

			joined := ""
			for _, d := range got {
				joined += d.Message + "\n"
				if d.ID != "ARCH612" || d.File != "q.sql" || d.Line != 3 ||
					d.Repair == "" {
					t.Fatalf("malformed diagnostic %+v", d)
				}
			}

			if !strings.Contains(joined, tc.want) {
				t.Fatalf("want %q in %q", tc.want, joined)
			}
		})
	}
}

func TestAnalyzeQueryParamShape(t *testing.T) {
	text := "SELECT id FROM audit_events WHERE org_id = $1"

	for name, param := range map[string]queryParam{
		"nullable": {Number: 1, NotNull: false},
		"slice":    {Number: 1, NotNull: true, Slice: true},
	} {
		t.Run(name, func(t *testing.T) {
			q := query{
				Name: "Q", File: "q.sql", Line: 1, Text: text,
				Params: []queryParam{param},
			}
			if len(analyzeQuery(q, testEnv())) == 0 {
				t.Fatal("want rejection")
			}
		})
	}

	q := query{Name: "Q", File: "q.sql", Line: 1, Text: text}

	got := analyzeQuery(q, testEnv())
	if len(got) == 0 || !strings.Contains(got[0].Message, "did not report") {
		t.Fatalf("unreported param must fail closed: %v", got)
	}
}

func TestAnalyzeQuerySetMembership(t *testing.T) {
	env := testEnv()
	env.allowed = map[string]bool{"audit_events": true}
	q := query{
		Name: "Q", File: "q.sql", Line: 1, Params: params(1),
		Text: "SELECT t.id FROM audit_tags t WHERE t.org_id = $1",
	}

	got := analyzeQuery(q, env)
	if len(got) != 1 || !strings.Contains(got[0].Message, "does not list") {
		t.Fatalf("want set-membership rejection, got %v", got)
	}
}

func TestAnalyzeQueryCatalogTablesMustBeDeclared(t *testing.T) {
	q := query{
		Name: "Q", File: "q.sql", Line: 1, Params: params(1),
		Text:   "SELECT id FROM audit_events WHERE org_id = $1",
		Tables: []string{"hidden"},
	}

	got := analyzeQuery(q, testEnv())
	if len(got) != 1 || !strings.Contains(got[0].Message, "hidden") {
		t.Fatalf("catalog table must be checked, got %v", got)
	}
}

func TestAnalyzeQueryTenantRootInsert(t *testing.T) {
	env := testEnv()
	env.tables["orgs"] = Table{
		Name:         "orgs",
		TenantColumn: "id",
		TenantRoot:   true,
	}
	env.allowed["orgs"] = true

	insert := query{
		Name:   "CreateOrg",
		File:   "db/queries/orgs/orgs.sql",
		Line:   1,
		Text:   "INSERT INTO orgs (id, slug) VALUES ($1, $2)",
		Params: params(2),
	}

	got := analyzeQuery(insert, env)
	if !strings.Contains(messages(got), "outside a provisioning query set") {
		t.Fatalf("root INSERT allowed outside provisioning: %v", got)
	}

	env.provision = true
	if got := analyzeQuery(insert, env); len(got) != 0 {
		t.Fatalf("provisioning set rejected: %v", got)
	}

	// Reads and updates of the verified tenant's own row stay allowed.
	for _, text := range []string{
		"SELECT slug FROM orgs WHERE id = $1",
		"UPDATE orgs SET slug = $2 WHERE id = $1",
	} {
		env.provision = false

		read := query{
			Name: "Read", File: "q.sql", Line: 1, Text: text,
			Params: params(2),
		}
		if got := analyzeQuery(read, env); len(got) != 0 {
			t.Fatalf("%s rejected: %v", text, got)
		}
	}
}
