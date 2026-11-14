package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestDetectAndParseTarget(t *testing.T) {
	state, err := Detect([]byte("schema_version = 7\n"))
	if err != nil || state != (State{7, ProfilePublished}) {
		t.Fatalf("detect: %v %v", state, err)
	}

	state, err = Detect(
		[]byte("schema_version = 10\n[boundary]\nmode = 'candidate'\n"),
	)
	if err != nil || state.Profile != ProfileCandidate {
		t.Fatalf("detect candidate: %v %v", state, err)
	}

	for _, bad := range []string{
		"",
		"kind = 'frontend'\nschema_version = 10\n",
		"schema_version = 10\n[boundary]\nmode = 'other'\n",
		"schema_version = [",
	} {
		if _, err := Detect([]byte(bad)); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}

	current := State{4, ProfilePublished}
	for value, want := range map[string]State{
		"":          {project.SchemaVersion, ProfilePublished},
		"candidate": {project.SchemaVersion, ProfileCandidate},
		"6":         {6, ProfilePublished},
		"schema:5":  {5, ProfilePublished},
	} {
		got, err := ParseTarget(value, current)
		if err != nil || got != want {
			t.Fatalf("target %q: %v %v", value, got, err)
		}
	}

	for _, bad := range []string{"0", "99", "multi"} {
		if _, err := ParseTarget(bad, current); err == nil {
			t.Fatalf("accepted target %q", bad)
		}
	}
}

func TestChainOrdersStepsAndRejectsMissingPaths(t *testing.T) {
	steps := []Step{
		{ID: "b", From: State{2, "p"}, To: State{3, "p"}},
		{ID: "a", From: State{1, "p"}, To: State{2, "p"}},
		{ID: "c", From: State{3, "p"}, To: State{3, "c"}},
	}

	chain, err := Chain(steps, State{1, "p"}, State{3, "c"})
	if err != nil {
		t.Fatal(err)
	}

	ids := []string{}
	for _, step := range chain {
		ids = append(ids, step.ID)
	}

	if strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("chain %v", ids)
	}

	if chain, err := Chain(steps, State{3, "c"}, State{3, "c"}); err != nil ||
		len(chain) != 0 {
		t.Fatalf("current state must be a no-op: %v %v", chain, err)
	}

	if _, err := Chain(steps, State{3, "c"}, State{3, "p"}); err == nil {
		t.Fatal("reverse path accepted")
	}

	looping := []Step{
		{From: State{1, "p"}, To: State{2, "p"}},
		{From: State{2, "p"}, To: State{1, "p"}},
	}
	if _, err := Chain(looping, State{1, "p"}, State{3, "p"}); err == nil {
		t.Fatal("cycle accepted")
	}
}

func writeTree(t *testing.T, root string, files map[string]string) {
	t.Helper()

	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readTree(t *testing.T, root, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return "<missing>"
	}

	return string(data)
}

func TestBuildChainsStepsAndBlockedPlansWriteNothing(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"pluto.toml": "schema_version = 1\n",
		"keep.txt":   "old\n",
		"gone.txt":   "gone\n",
	})

	steps := []Step{
		{
			ID:   "001",
			From: State{1, ProfilePublished},
			To:   State{2, ProfilePublished},
			Plan: func(w *Workspace) error {
				if err := w.Write("keep.txt", []byte("new\n")); err != nil {
					return err
				}

				return w.Delete("gone.txt")
			},
		},
		{
			ID:   "002",
			From: State{2, ProfilePublished},
			To:   State{3, ProfilePublished},
			Plan: func(w *Workspace) error {
				data, _, err := w.Read("keep.txt")
				if err != nil {
					return err
				}

				if _, exists, _ := w.Read("gone.txt"); exists {
					t.Fatal("later step sees deleted file")
				}

				return w.Write("keep.txt", append(data, "more\n"...))
			},
		},
	}

	plan, err := build(steps, root, "3", Options{})
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(plan.Paths(), ","); got != "gone.txt,keep.txt" {
		t.Fatalf("paths %s", got)
	}

	if string(plan.Writes["keep.txt"]) != "new\nmore\n" {
		t.Fatalf("chained write %q", plan.Writes["keep.txt"])
	}

	if string(plan.Original["keep.txt"]) != "old\n" {
		t.Fatal("original content not retained")
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	if readTree(t, root, "keep.txt") != "new\nmore\n" ||
		readTree(t, root, "gone.txt") != "<missing>" {
		t.Fatal("plan not applied")
	}

	steps[1].Plan = func(w *Workspace) error {
		w.Block(Item{Path: "pluto.toml", Line: 1, Reason: "r", Action: "a"})
		return w.Write("keep.txt", []byte("blocked\n"))
	}
	writeTree(t, root, map[string]string{"gone.txt": "gone\n"})

	plan, err = build(steps, root, "3", Options{})
	if err != nil {
		t.Fatal(err)
	}

	if !plan.Blocked || len(plan.Manual) != 1 {
		t.Fatalf("expected blocked plan: %+v", plan.Manual)
	}

	if err := Write(plan); err == nil {
		t.Fatal("blocked plan written")
	}

	if readTree(t, root, "keep.txt") != "new\nmore\n" {
		t.Fatal("blocked plan changed files")
	}
}

func TestWorkspaceRefusesUnsafePaths(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{"pluto.toml": "schema_version = 1\n"})

	steps := []Step{
		{
			ID:   "escape",
			From: State{1, ProfilePublished},
			To:   State{2, ProfilePublished},
			Plan: func(w *Workspace) error {
				return w.Write("../outside", []byte("x"))
			},
		},
	}

	if _, err := build(steps, root, "2", Options{}); err == nil {
		t.Fatal("path traversal accepted")
	}
}
