package migrate

import (
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

const historicalEndpoint = "internal/paths/endpoint.go"

func historicalFixture(t *testing.T) (string, string) {
	t.Helper()

	manifest := publishedManifest("multi")
	root := publishedFixture(t, manifest)

	renders := historicalRenders(manifest, historicalEndpoint)
	if len(renders) == 0 {
		t.Fatal("no recorded scaffold history for " + historicalEndpoint)
	}

	return root, string(renders[0])
}

func TestHistoricalScaffoldCountsAsPristine(t *testing.T) {
	root, old := historicalFixture(t)
	writeTree(t, root, map[string]string{historicalEndpoint: old})

	plan, err := Build(root, ProfileCandidate, Options{
		Sources: candidateSources(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range plan.Manual {
		if item.Path == historicalEndpoint {
			t.Fatalf("older pristine scaffold reported: %+v", item)
		}
	}

	written, ok := plan.Writes[historicalEndpoint]
	if !ok || !strings.Contains(string(written), "QueryParam") {
		t.Fatalf("older scaffold not upgraded to the new render: %q", written)
	}
}

func TestEditedHistoricalGeneratedPathStaysManual(t *testing.T) {
	root, old := historicalFixture(t)
	edited := old + "\n// Mine is an authored addition.\ntype Mine struct{}\n"
	writeTree(t, root, map[string]string{historicalEndpoint: edited})

	plan, err := Build(root, ProfileCandidate, Options{
		Sources: candidateSources(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := plan.Writes[historicalEndpoint]; ok {
		t.Fatal("edits to a generated path must not be merged or replaced")
	}

	found := false

	for _, item := range plan.Manual {
		found = found || item.Path == historicalEndpoint
	}

	if !found {
		t.Fatal("edited generated path must be reported")
	}
}

func TestEditedAuthoredFileMergesCleanly(t *testing.T) {
	manifest := publishedManifest("multi")
	root := publishedFixture(t, manifest)
	current := readTree(t, root, "sqlc.yaml")
	writeTree(t, root, map[string]string{
		"sqlc.yaml": current + "# mine: keep me\n",
	})

	plan, err := Build(root, ProfileCandidate, Options{
		Sources: candidateSources(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, item := range plan.Manual {
		if item.Path == "sqlc.yaml" {
			t.Fatalf("clean merge reported as manual: %+v", item)
		}
	}

	noted := false

	for _, item := range plan.Notes {
		noted = noted || item.Path == "sqlc.yaml"
	}

	if !noted {
		t.Fatal("kept or merged edits must be reported")
	}

	if merged, ok := plan.Writes["sqlc.yaml"]; ok &&
		!strings.Contains(string(merged), "# mine: keep me") {
		t.Fatalf("merge lost the user edit:\n%s", merged)
	}
}

func TestThreeWayRefusesConflictsAndInvalidGo(t *testing.T) {
	if _, ok := threeWay(
		[]byte("a\nmine\nc\n"),
		[]byte("a\nb\nc\n"),
		[]byte("a\ntheirs\nc\n"),
	); ok {
		t.Fatal("overlapping edits must conflict")
	}

	merged, ok := threeWay(
		[]byte("a\nb\nc\nmine\n"),
		[]byte("a\nb\nc\n"),
		[]byte("new\nb\nc\n"),
	)
	if !ok || string(merged) != "new\nb\nc\nmine\n" {
		t.Fatalf("clean merge failed: %q %v", merged, ok)
	}

	if validMerge("a.go", []byte("package a\nfunc {")) {
		t.Fatal("merged Go that does not parse must be rejected")
	}
}

func TestGeneratedFilesAreNeverMerged(t *testing.T) {
	root := t.TempDir()
	workspace := &Workspace{Root: root, plan: &Plan{
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
	}}

	want := []byte(generatedHeader + "\npackage a\n")
	if mergeEdited(
		workspace,
		"a.go",
		[]byte(generatedHeader+"\npackage a\n// mine\n"),
		want,
		[][]byte{want},
		ProfileCandidate,
	) {
		t.Fatal("generated file edits must stay manual")
	}
}

func TestStalePristineScaffoldTestIsDeletedWithNote(t *testing.T) {
	root := publishedFixture(t, publishedManifest("multi"))

	plan, err := Build(root, ProfileCandidate, Options{
		Sources: candidateSources(t),
	})
	if err != nil {
		t.Fatal(err)
	}

	const name = "internal/app/app_test.go"

	if !plan.Deletes[name] {
		t.Fatalf("stale pristine test kept: %v", plan.Paths())
	}

	for _, item := range plan.Notes {
		if item.Path == name &&
			strings.Contains(item.Reason, "pristine scaffold test removed") {
			return
		}
	}

	t.Fatalf("deletion not reported: %+v", plan.Notes)
}

func TestOrphanedMembershipsMigrationIsReportedNotDeleted(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"db/migrations/099_memberships.sql": "CREATE TABLE m ();\n",
	})

	plan := &Plan{
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
	}
	workspace := &Workspace{Root: root, plan: plan}

	next := publishedManifest("multi")
	if err := orphanedMembershipsSchema(workspace, next); err != nil ||
		len(plan.Notes) != 0 {
		t.Fatalf(
			"unbound memberships must stay silent: %v %+v",
			err,
			plan.Notes,
		)
	}

	next.Memberships = &project.Memberships{}
	if err := orphanedMembershipsSchema(workspace, next); err != nil {
		t.Fatal(err)
	}

	if len(plan.Notes) != 1 || len(plan.Deletes) != 0 {
		t.Fatalf(
			"want one note and no delete: %+v %v",
			plan.Notes,
			plan.Deletes,
		)
	}
}
