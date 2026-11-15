package project

import (
	"go/parser"
	"go/token"
	"strings"
	"testing"
)

func TestCandidateAppMembershipsBindsDeclaredTable(t *testing.T) {
	m := seamFixture(t)

	source, err := candidateAppMemberships(m)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := parser.ParseFile(
		token.NewFileSet(), "memberships_gen.go", source, 0,
	); err != nil {
		t.Fatalf("generated source does not parse: %v", err)
	}

	text := string(source)
	for _, want := range []string{
		`SELECT \"role\" FROM \"users\" WHERE \"id\" = $1 AND \"org_id\" = $2`,
		"set_config('app.tenant_id', $1, true)",
		`"example.com/gryt/internal/domain"`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}

	if strings.Contains(text, "pluto_resource_memberships") {
		t.Fatal("app-bound resolver must not use the framework table")
	}
}

func TestCandidateAppMembershipsRejectsUnknownTable(t *testing.T) {
	m := seamFixture(t)
	m.Memberships.Table = "access.missing"

	if _, err := candidateAppMemberships(m); err == nil {
		t.Fatal("expected unknown table to fail")
	}
}

func TestCandidateManagedUsesAppMembershipsWhenDeclared(t *testing.T) {
	m := seamFixture(t)

	files, err := candidateManaged(m)
	if err != nil {
		t.Skipf("fixture is not a full candidate manifest: %v", err)
	}

	path := "internal/storage/identity/memberships_gen.go"
	if !strings.Contains(string(files[path]), `\"users\"`) {
		t.Fatalf("managed memberships not app-bound:\n%s", files[path])
	}

	if _, ok := files["db/migrations/099_memberships.sql"]; ok {
		t.Fatal("framework membership migration must not be generated")
	}
}

func TestCandidateAppMembershipsActivePredicate(t *testing.T) {
	for name, tc := range map[string]struct {
		column string
		values []string
		want   []string
	}{
		"none": {want: nil},
		"boolean": {
			column: "active",
			want:   []string{`AND \"active\" IS TRUE`},
		},
		"values": {
			column: "state",
			values: []string{"active", "trial"},
			want: []string{
				`AND \"state\"::text = ANY($3)`,
				`[]string{"active", "trial"}`,
				"membershipActiveValues",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := seamFixture(t)
			m.Memberships.ActiveColumn = tc.column
			m.Memberships.ActiveValues = tc.values

			if err := m.Validate(); err != nil {
				t.Fatal(err)
			}

			source, err := candidateAppMemberships(m)
			if err != nil {
				t.Fatal(err)
			}

			formatted, err := formatGenerated(source, m.Project.Module)
			if err != nil {
				t.Fatalf("generated source does not format: %v", err)
			}

			text := string(formatted)
			if tc.column == "" &&
				(strings.Contains(text, "IS TRUE") ||
					strings.Contains(text, "ANY($3)") ||
					strings.Contains(text, "membershipActiveValues")) {
				t.Fatalf("no predicate expected:\n%s", text)
			}

			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("missing %q in:\n%s", want, text)
				}
			}
		})
	}
}
