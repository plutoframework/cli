package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func candidateSources(t *testing.T) project.BoundaryProfile {
	t.Helper()

	root := t.TempDir()
	profile := project.BoundaryProfile{
		AuthSource: filepath.Join(root, "pluto-auth"),
		RBACSource: filepath.Join(root, "pluto-rbac"),
		CoreSource: filepath.Join(root, "pluto-core"),
		JobsSource: filepath.Join(root, "pluto-jobs"),
	}

	for _, relative := range []string{
		"pluto-auth",
		"pluto-auth/stores/postgres",
		"pluto-auth/methods/password",
		"pluto-rbac",
		"pluto-rbac/identity",
		"pluto-core",
		"pluto-core/stores/postgres",
		"pluto-jobs",
		"pluto-jobs/stores/postgres",
	} {
		writeTree(t, root, map[string]string{
			relative + "/go.mod": "module github.com/plutoframework/" +
				relative + "\n\ngo 1.25.5\n",
		})
	}

	return profile
}

func publishedManifest(tenancy string) project.Manifest {
	return project.Manifest{
		Boundary:      project.BoundaryProfile{Mode: ProfilePublished},
		SchemaVersion: project.SchemaVersion,
		Project: project.Project{
			Name:      "shop",
			Module:    "example.com/shop",
			EnvPrefix: "SHOP_",
		},
		Capabilities: project.Capabilities{Cache: "none", Tenancy: tenancy},
		Database:     project.Database{Engine: "postgres", Queries: "sqlc"},
		Development:  project.Development{Mise: true, Fnox: true, Docker: true},
	}
}

// publishedFixture writes the exact files pluto init renders.
func publishedFixture(t *testing.T, m project.Manifest) string {
	t.Helper()

	files, err := project.InitialFiles(m)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	for name, data := range files {
		writeTree(t, root, map[string]string{name: string(data)})
	}

	return root
}

func TestPublishedToCandidateFreshApplication(t *testing.T) {
	before := publishedManifest("multi")
	root := publishedFixture(t, before)
	sources := candidateSources(t)

	plan, err := Build(root, ProfileCandidate, Options{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked || len(plan.Manual) != 0 ||
		len(scaffoldTestNotes(plan)) != 0 {
		t.Fatalf("fresh app must migrate cleanly: %+v %+v", plan.Manual,
			plan.Notes)
	}

	if len(plan.Steps) != 1 || plan.Steps[0].ID !=
		"010-published-to-candidate" || !plan.Tidy {
		t.Fatalf("steps %+v tidy %v", plan.Steps, plan.Tidy)
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	after := before
	after.Boundary = sources
	after.Boundary.Mode = ProfileCandidate

	expected, err := project.InitialFiles(after)
	if err != nil {
		t.Fatal(err)
	}

	for name, data := range expected {
		if reconcileSkipped[name] {
			continue
		}

		if readTree(t, root, name) != string(data) {
			t.Fatalf("%s differs from candidate scaffold", name)
		}
	}

	previous, err := project.InitialFiles(before)
	if err != nil {
		t.Fatal(err)
	}

	for name := range previous {
		if _, kept := expected[name]; !kept &&
			!strings.HasSuffix(name, "_test.go") &&
			readTree(t, root, name) != "<missing>" {
			t.Fatalf("published-only scaffold %s retained", name)
		}
	}

	_, manifest, err := project.Discover(root)
	if err != nil || manifest.Boundary.Mode != ProfileCandidate {
		t.Fatalf("migrated manifest: %v %+v", err, manifest.Boundary)
	}

	module := readTree(t, root, "go.mod")
	for _, want := range []string{
		"require github.com/plutoframework/pluto-rbac/identity",
		"replace github.com/plutoframework/pluto-core => ",
	} {
		if !strings.Contains(module, want) {
			t.Fatalf("go.mod lacks %q:\n%s", want, module)
		}
	}

	again, err := Build(root, ProfileCandidate, Options{Sources: sources})
	if err != nil || len(again.Steps) != 0 || len(again.Paths()) != 0 {
		t.Fatalf("migration not idempotent: %v %+v", err, again)
	}
}

// scaffoldTestManual returns manual items that are not about pristine scaffold
// tests, which are kept (never deleted silently) and may dangle against the
// candidate API until ported.
// scaffoldTestNotes drops the notes about removed pristine scaffold tests.
func scaffoldTestNotes(plan *Plan) []Item {
	rest := []Item{}

	for _, item := range plan.Notes {
		if !strings.HasSuffix(item.Path, "_test.go") {
			rest = append(rest, item)
		}
	}

	return rest
}

func scaffoldTestManual(plan *Plan) []Item {
	rest := []Item{}

	for _, item := range plan.Manual {
		if !strings.HasSuffix(item.Path, "_test.go") {
			rest = append(rest, item)
		}
	}

	return rest
}

func TestPublishedToCandidatePreservesEditsAndReportsThem(t *testing.T) {
	root := publishedFixture(t, publishedManifest("multi"))
	edited := readTree(t, root, "internal/app/app.go") +
		"\nfunc Custom() { RegisterGenerated(nil, nil) }\n"
	writeTree(t, root, map[string]string{
		"internal/app/app.go":        edited,
		"db/migrations/005_jobs.sql": "-- tuned\n",
		"internal/transport/http_test.go": readTree(
			t,
			root,
			"internal/transport/http_test.go",
		) + "\n// my case\n",
		"internal/orders/orders.go": "package orders\n\nimport " +
			"\"example.com/shop/internal/app\"\n\nvar _ = " +
			"app.RegisterGenerated\n",
	})

	plan, err := Build(
		root,
		ProfileCandidate,
		Options{Sources: candidateSources(t)},
	)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked {
		t.Fatalf("file edits must not block: %+v", plan.Manual)
	}

	for _, name := range []string{
		"internal/app/app.go",
		"db/migrations/005_jobs.sql",
		"internal/transport/http_test.go",
		"internal/orders/orders.go",
	} {
		if _, ok := plan.Writes[name]; ok || plan.Deletes[name] {
			t.Fatalf("authored file %s changed", name)
		}
	}

	report := []string{}
	for _, item := range plan.Manual {
		report = append(report, item.String())
	}

	joined := strings.Join(report, "\n")
	for _, want := range []string{
		"db/migrations/005_jobs.sql:1: existing SQL migration",
		"internal/app/app.go:",
		"modified since scaffold",
		"internal/transport/http_test.go:",
		"has no candidate counterpart",
		"internal/orders/orders.go:5: uses app.RegisterGenerated",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in:\n%s", want, joined)
		}
	}
}

func TestPublishedToCandidateBlockers(t *testing.T) {
	sources := candidateSources(t)

	single := publishedFixture(t, publishedManifest("single"))
	assertBlocked(t, single, Options{Sources: sources}, "pluto.toml:",
		"requires multi tenancy")

	multi := publishedFixture(t, publishedManifest("multi"))
	assertBlocked(t, multi, Options{}, "pluto.toml:3:",
		"must be an explicit absolute directory")

	// The candidate profile accepts managed local credentials and OIDC, so
	// identity ingress alone no longer blocks adoption.
	credentials := publishedManifest("multi")
	credentials.Capabilities.LocalCredentials = true
	credentials.Capabilities.OIDC = true

	accepted, err := Build(
		publishedFixture(t, credentials),
		ProfileCandidate,
		Options{Sources: sources},
	)
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range accepted.Manual {
		if strings.Contains(item.Reason, "identity ingress") {
			t.Fatalf("supported identity ingress blocked: %s", item)
		}
	}

	operations := publishedManifest("multi")
	operations.Operations = []project.Operation{
		{
			Group:       "orders",
			Action:      "Create",
			Permission:  "orders.create",
			Write:       true,
			Idempotency: "required",
		},
	}
	operations.Jobs = []project.Job{
		{
			Operation: "orders.Create",
			Group:     "orders",
			Action:    "Create",
			Kind:      "orders.create",
		},
	}
	root := publishedFixture(t, operations)
	assertBlocked(t, root, Options{Sources: sources},
		"operation orders.Create has no typed behavior contract",
		"candidate job orders.create needs max_attempts")

	if _, err := Build(root, ProfileCandidate, Options{
		Sources:   sources,
		Contracts: []byte("[[unknown]]\nx = 1\n"),
	}); err == nil {
		t.Fatal("unknown contracts table accepted")
	}

	plan, err := Build(root, ProfileCandidate, Options{
		Sources: sources,
		Contracts: []byte(
			"[[jobs]]\nkind = 'orders.create'\nmax_attempts = 5\n",
		),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range plan.Manual {
		if strings.Contains(item.Reason, "max_attempts") {
			t.Fatal("job budget from --contracts ignored")
		}
	}
}

func assertBlocked(t *testing.T, root string, options Options, want ...string) {
	t.Helper()

	before := readTree(t, root, "pluto.toml")

	plan, err := Build(root, ProfileCandidate, options)
	if err != nil {
		t.Fatal(err)
	}

	if !plan.Blocked {
		t.Fatalf("expected blocked plan for %v", want)
	}

	report := []string{}
	for _, item := range plan.Manual {
		report = append(report, item.String())
	}

	joined := strings.Join(report, "\n")
	for _, item := range want {
		if !strings.Contains(joined, item) {
			t.Fatalf("missing %q in:\n%s", item, joined)
		}
	}

	if err := Write(plan); err == nil {
		t.Fatal("blocked plan written")
	}

	if readTree(t, root, "pluto.toml") != before {
		t.Fatal("blocked plan changed pluto.toml")
	}

	if _, err := os.Stat(filepath.Join(root, "db/migrations/"+
		"099_memberships.sql")); err == nil {
		t.Fatal("blocked plan created candidate files")
	}
}
