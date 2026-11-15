package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/migrate"
	"github.com/plutoframework/cli/internal/project"
)

const upgradedPath = "internal/paths/endpoint.go"

func generateFixture(t *testing.T) string {
	t.Helper()

	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	root := temporary(t)
	args := []string{
		"--no-input", "init", ".", "--name", "shop",
		"--module", "example.com/shop", "--multi-tenant",
		"--no-mise", "--no-fnox", "--no-docker",
		"--boundary-profile", "candidate",
	}

	for _, name := range []string{"auth", "rbac", "core", "jobs"} {
		source := os.Getenv("PLUTO_CLI_CANDIDATE_" + strings.ToUpper(name) +
			"_SOURCE")
		if source == "" {
			t.Fatal("missing explicit candidate source", name)
		}

		args = append(args, "--candidate-"+name+"-source", source)
	}

	mustInvoke(t, root, args...)
	commitAll(t, root)

	return root
}

// olderRender returns the recorded earlier scaffold of a managed path.
func olderRender(t *testing.T) []byte {
	t.Helper()

	older, err := os.ReadFile(filepath.Join(
		"..", "migrate", "history", "2026-11-13", upgradedPath+".txt",
	))
	if err != nil {
		t.Fatal(err)
	}

	text := strings.ReplaceAll(string(older), "@@MODULE@@", "example.com/shop")

	return []byte(strings.ReplaceAll(text, "@@NAME@@", "shop"))
}

func TestGenerateRegeneratesUpgradeDriftIdempotently(t *testing.T) {
	root := generateFixture(t)
	path := filepath.Join(root, upgradedPath)

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	older := olderRender(t)
	if string(older) == string(current) {
		t.Skip("history render equals current render")
	}

	if err := os.WriteFile(path, older, 0o644); err != nil {
		t.Fatal(err)
	}

	// AGENTS.md as the previous CLI's template rendered it.
	oldAgents, err := os.ReadFile(filepath.Join(
		"..", "migrate", "history", "docs", "2026-11-14",
		"AGENTS.md.tmpl.txt",
	))
	if err != nil {
		t.Fatal(err)
	}

	_, manifest, err := project.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	docs, err := project.DocumentsFromTemplates(
		manifest,
		map[string][]byte{"AGENTS.md": oldAgents},
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "AGENTS.md"), docs["AGENTS.md"], 0o644,
	); err != nil {
		t.Fatal(err)
	}

	commitAll(t, root)

	output := mustInvoke(t, root, "--no-input", "generate", "--dry-run")
	if !strings.Contains(output, "M "+upgradedPath) ||
		!strings.Contains(output, "M AGENTS.md") ||
		!strings.Contains(output, "Dry run: nothing written.") {
		t.Fatalf("dry run:\n%s", output)
	}

	if got, _ := os.ReadFile(path); string(got) != string(older) {
		t.Fatal("dry run wrote files")
	}

	plan, err := migrate.Generate(root)
	if err != nil || plan.Blocked || len(plan.Paths()) != 2 {
		t.Fatalf("plan: %v %+v", err, plan.Manual)
	}

	if err := migrate.Write(plan); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil || !project.Equivalent(got, current) {
		t.Fatalf("not regenerated: %v\n%s", err, got)
	}

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || !strings.Contains(string(agents), "provision") {
		t.Fatalf("AGENTS.md not refreshed with seam guidance: %v", err)
	}

	commitAll(t, root)

	output = mustInvoke(t, root, "--no-input", "generate", "--dry-run")
	if !strings.Contains(output, "Already up to date") {
		t.Fatalf("not idempotent:\n%s", output)
	}
}

func TestGenerateRefusesHandEditedManagedFile(t *testing.T) {
	root := generateFixture(t)
	path := filepath.Join(root, upgradedPath)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	edited := string(data) + "\n// local tweak\nvar tweak = 1\n"
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Also make a second file stale so a partial write would be visible.
	other := filepath.Join(root, "internal/transport/register.go")
	if err := os.Remove(other); err != nil {
		t.Fatal(err)
	}

	commitAll(t, root)

	output, err := invoke(t, root, "--no-input", "generate")
	if err == nil || !strings.Contains(err.Error(), "edited by hand") {
		t.Fatalf("hand edit accepted: %v\n%s", err, output)
	}

	for _, want := range []string{
		upgradedPath + ":", "edited by hand", "+ ", "- ",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("refusal lacks %q:\n%s", want, output)
		}
	}

	if got, _ := os.ReadFile(path); string(got) != edited {
		t.Fatal("hand edit was overwritten")
	}

	if _, err := os.Stat(other); err == nil {
		t.Fatal("blocked generate wrote other files")
	}
}

func TestGenerateRestoresDeletedManagedFileAndGuidesAgents(t *testing.T) {
	root := generateFixture(t)

	agents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || !strings.Contains(string(agents), "pluto generate") {
		t.Fatalf("AGENTS.md must tell agents to run pluto generate: %v", err)
	}

	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil || !strings.Contains(string(readme), "pluto generate") {
		t.Fatalf("README.md must mention pluto generate: %v", err)
	}
}

// TestGenerateGrytShapedFixture runs generate on a real application (gryt's
// api at PLUTO_CLI_GRYT_GENERATE_REF, default 2ebd772) after swapping one
// managed file for an older CLI's render. Gated on PLUTO_CLI_GRYT_REPO.
func TestGenerateGrytShapedFixture(t *testing.T) {
	repo := os.Getenv("PLUTO_CLI_GRYT_REPO")
	if repo == "" {
		t.Skip("set PLUTO_CLI_GRYT_REPO to a gryt checkout")
	}

	ref := os.Getenv("PLUTO_CLI_GRYT_GENERATE_REF")
	if ref == "" {
		ref = "2ebd772"
	}

	root := temporary(t)

	archive := exec.Command("sh", "-c", "git -C \"$1\" archive \"$2\" api | "+
		"tar -x -C \"$3\" --strip-components=1", "sh", repo, ref, root)
	if output, err := archive.CombinedOutput(); err != nil {
		t.Fatalf("archive %s: %v\n%s", ref, err, output)
	}

	initRepo := exec.Command("git", "init", "-q")
	initRepo.Dir = root

	if output, err := initRepo.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, output)
	}

	commitAll(t, root)

	_, manifest, err := project.Discover(root)
	if err != nil {
		t.Fatal(err)
	}

	older, err := os.ReadFile(filepath.Join(
		"..", "migrate", "history", "2026-11-13", upgradedPath+".txt",
	))
	if err != nil {
		t.Fatal(err)
	}

	text := strings.ReplaceAll(
		string(older),
		"@@MODULE@@",
		manifest.Project.Module,
	)
	text = strings.ReplaceAll(text, "@@NAME@@", manifest.Project.Name)
	path := filepath.Join(root, upgradedPath)

	current, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}

	commitAll(t, root)

	output := mustInvoke(t, root, "--no-input", "generate", "--dry-run")
	if !strings.Contains(output, "M "+upgradedPath) ||
		strings.Contains(output, "edited by hand") {
		t.Fatalf("upgrade drift not planned cleanly:\n%s", output)
	}

	plan, err := migrate.Generate(root)
	if err != nil || plan.Blocked {
		t.Fatalf("plan: %v %+v", err, plan.Manual)
	}

	if err := migrate.Write(plan); err != nil {
		t.Fatal(err)
	}

	if got, _ := os.ReadFile(path); !project.Equivalent(got, current) {
		t.Fatal("managed file not restored")
	}

	if err := os.WriteFile(
		path,
		append(current, "// hand\n"...),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output, err = invoke(t, root, "--no-input", "generate", "--dry-run")
	if err == nil || !strings.Contains(output, upgradedPath+":") {
		t.Fatalf("hand edit accepted: %v\n%s", err, output)
	}
}
