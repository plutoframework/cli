package migrate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

// lockedFixture writes a published application the way pluto init does, so
// the managed lock is recorded by project.Apply.
func lockedFixture(t *testing.T) (string, project.Manifest) {
	t.Helper()

	manifest := publishedManifest("multi")

	files, err := project.InitialFiles(manifest)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	if _, err := project.Apply(root, files, nil, false); err != nil {
		t.Fatal(err)
	}

	return root, manifest
}

func managedNames(t *testing.T, m project.Manifest) []string {
	t.Helper()

	managed, err := project.Managed(m)
	if err != nil {
		t.Fatal(err)
	}

	names := []string{}

	for name := range managed {
		if !reconcileSkipped[name] {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	return names
}

// olderRenderOf stands in for what an earlier CLI wrote: the current render
// with a token-level change, which the formatter cannot normalize away.
func olderRenderOf(name string, current []byte) []byte {
	if strings.HasSuffix(name, ".go") {
		return append(current, []byte("\nvar olderCLIRender = 1\n")...)
	}

	return append(current, []byte("\n# older CLI render\n")...)
}

// This is the CI guard for template changes: whatever the templates render,
// every managed file must be recorded in the lock by the command that wrote
// it, so a later template change is an upgrade and not a "hand edit".
func TestApplyRecordsEveryManagedFileInTheLock(t *testing.T) {
	root, manifest := lockedFixture(t)

	lock, found, err := project.ReadLock(root)
	if err != nil || !found {
		t.Fatalf("lock missing after init: found=%v err=%v", found, err)
	}

	for _, name := range managedNames(t, manifest) {
		data, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}

		if !lock.Matches(name, data) {
			t.Errorf("%s is not recorded in %s", name, project.LockPath)
		}
	}
}

// Simulates a CLI release that changed every template without recording a
// history snapshot: each managed file is an older render known only to the
// lock. generate must upgrade all of them with nothing flagged.
func TestGenerateUpgradesEveryLockedOlderRender(t *testing.T) {
	root, manifest := lockedFixture(t)
	older := project.Lock{}

	for _, name := range managedNames(t, manifest) {
		if isSQLMigration(name) {
			continue
		}

		current, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}

		edited := olderRenderOf(name, current)
		writeTree(t, root, map[string]string{name: string(edited)})

		older[name] = project.SourceDigest(name, edited)
	}

	writeTree(t, root, map[string]string{
		project.LockPath: string(older.Render()),
	})

	plan, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked || len(plan.Manual) != 0 {
		t.Fatalf("older locked renders refused: %+v", plan.Manual)
	}

	for name := range older {
		if _, ok := plan.Writes[name]; !ok {
			t.Errorf("%s was not regenerated", name)
		}
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	again, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(again.Paths()) != 0 || len(again.Manual) != 0 {
		t.Fatalf("second generate not idempotent: %v", again.Paths())
	}
}

func TestGenerateRefusesHandEditOfLockedFile(t *testing.T) {
	root, manifest := lockedFixture(t)
	name := firstGoName(t, manifest)

	current, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}

	// The CLI wrote an older render, then the user edited it by hand.
	older := olderRenderOf(name, current)
	lock := project.Lock{name: project.SourceDigest(name, older)}
	edited := append(older, []byte("\nvar mine = 2\n")...)

	writeTree(t, root, map[string]string{
		name:             string(edited),
		project.LockPath: string(lock.Render()),
	})

	plan, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}

	if !plan.Blocked || len(plan.Manual) != 1 ||
		plan.Manual[0].Path != name ||
		!strings.Contains(plan.Manual[0].Reason, "edited by hand") {
		t.Fatalf("hand edit not refused: %+v", plan.Manual)
	}

	if strings.Contains(plan.Manual[0].Action, "--adopt") {
		t.Fatal("adopt hint offered although a lock exists")
	}
}

func TestAdoptBootstrapsAnApplicationWithoutALock(t *testing.T) {
	root, manifest := lockedFixture(t)
	name := firstGoName(t, manifest)

	current, err := os.ReadFile(filepath.Join(root, name))
	if err != nil {
		t.Fatal(err)
	}

	writeTree(t, root, map[string]string{
		name: string(olderRenderOf(name, current)),
	})

	if err := os.Remove(filepath.Join(root, project.LockPath)); err != nil {
		t.Fatal(err)
	}

	plan, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}

	if !plan.Blocked || !strings.Contains(plan.Manual[0].Action, "--adopt") {
		t.Fatalf("expected refusal pointing at --adopt: %+v", plan.Manual)
	}

	plan, err = Adopt(root)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked || string(plan.Writes[name]) != string(current) {
		t.Fatalf("adopt did not restore the render: %+v", plan.Manual)
	}

	if _, ok := plan.Writes[project.LockPath]; !ok {
		t.Fatal("adopt did not record the lock")
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	if _, err := Adopt(root); err == nil {
		t.Fatal("adopt must refuse once a lock exists")
	}
}

func firstGoName(t *testing.T, m project.Manifest) string {
	t.Helper()

	for _, name := range managedNames(t, m) {
		if strings.HasSuffix(name, ".go") &&
			len(historicalRenders(m, name)) == 0 {
			return name
		}
	}

	t.Fatal("no managed Go file without history")

	return ""
}
