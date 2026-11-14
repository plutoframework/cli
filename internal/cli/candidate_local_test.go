package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"text/template"

	"github.com/plutoframework/cli/internal/project"
)

const candidateLocalBlueprint = `[[resources]]
group = "support"
name = "Ticket"
table = "tickets"
namespace = "support-tickets"
version = 1
visibility = "tenant"
initial_state = "open"
states = ["open"]
normalize = "trim"
max_value_bytes = 1024
page_limit = 10
export_limit = 10
cache_ttl_seconds = 0

[[operations]]
group = "support"
action = "Create"
permission = "support.create"
write = true
idempotency = "required"

[[operations]]
group = "support"
action = "Load"
permission = "support.load"
idempotency = "none"

[[operation_contracts]]
id = "support.Create"
resource = "support.Ticket"
kind = "create"
permission = "support.create"
resource_policy = "create"
idempotency = "required"

[[operation_contracts]]
id = "support.Load"
resource = "support.Ticket"
kind = "load"
permission = "support.load"
resource_policy = "tenant"
idempotency = "none"

[[endpoints]]
group = "support"
action = "Create"
operation = "support.Create"
name = "Create ticket"
description = "Create a support ticket."
method = "POST"
path = "/tickets"
resource_source = "body"

[[endpoints]]
group = "support"
action = "Load"
operation = "support.Load"
name = "Load ticket"
description = "Load a support ticket."
method = "GET"
path = "/tickets/{id}"
resource_source = "path:id"

[[roles]]
id = "agent"
name = "Agent"
description = "Support agent."
permissions = ["support.create", "support.load"]
`

// Candidate local credentials and OIDC are managed ingress: sign-in creates a
// session through the runtime's fenced manager, which alone issues operation
// identities. Protected operations must pass architecture checks and vet.
func TestCandidateLocalCredentialConsumer(t *testing.T) {
	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		if os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE") == "" {
			t.Fatal("missing explicit candidate source", name)
		}
	}

	for _, oidc := range []bool{false, true} {
		name := "password"
		if oidc {
			name = "oidc"
		}

		t.Run(name, func(t *testing.T) {
			runCandidateLocalCredentialConsumer(t, oidc)
		})
	}
}

func runCandidateLocalCredentialConsumer(t *testing.T, oidc bool) {
	root := temporary(t)

	args := []string{
		"--no-input",
		"init",
		".",
		"--name",
		"credentials",
		"--module",
		"example.test/credentials",
		"--env-prefix",
		"CREDENTIALS_",
		"--multi-tenant",
		"--local-credentials",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
		"--boundary-profile",
		"candidate",
	}
	if oidc {
		args = append(args, "--oidc")
	}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		args = append(
			args,
			"--candidate-"+strings.ToLower(name)+"-source",
			os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE"),
		)
	}

	mustInvoke(t, root, args...)

	for _, path := range []string{
		"cmd/api/credentials.go",
		"cmd/api/oidc.go",
	} {
		if _, err := os.Stat(filepath.Join(root, path)); err == nil {
			t.Fatalf("published credential composition generated: %s", path)
		}
	}

	seam, err := os.ReadFile(
		filepath.Join(
			root,
			filepath.FromSlash(project.CandidateCredentialSeam),
		),
	)
	if err != nil || bytes.Contains(seam, []byte("DO NOT EDIT")) {
		t.Fatal("authored credential seam missing or marked managed", err)
	}

	path := filepath.Join(root, "ticket-blueprint.toml")
	if err := os.WriteFile(
		path,
		[]byte(candidateLocalBlueprint),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file", path)

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("candidate credential architecture: %+v", report.Diagnostics)
	}

	external(t, root, "go", "mod", "tidy")
	external(t, root, "go", "vet", "./...")
	mustInvoke(t, root, "fmt")
	mustInvoke(t, root, "openapi")
	mustInvoke(t, root, "vet")
	lintCandidateManaged(t, root)

	assertCandidateSeamRestricted(t, root, seam)

	source, err := os.ReadFile("testdata/candidate_local_runtime.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	tmpl, err := template.New("local").Parse(string(source))
	if err != nil {
		t.Fatal(err)
	}

	var fixture bytes.Buffer
	if err := tmpl.Execute(
		&fixture,
		struct {
			OIDC bool
		}{oidc},
	); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "internal/app/candidate_local_runtime_test.go"),
		fixture.Bytes(),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	output := external(
		t,
		root,
		"go",
		"test",
		"-race",
		"-json",
		"-count=1",
		"-run",
		"^TestCandidateLocal",
		"./internal/app",
	)
	assertCandidateCaseNames(
		t,
		output,
		"TestCandidateLocalCredentialLifecycle",
		"TestCandidateLocalSharedAttemptLimiter",
	)
}

// An authored seam that reaches application authority must fail the canonical
// check; restoring the generated seam passes again.
func assertCandidateSeamRestricted(t *testing.T, root string, seam []byte) {
	t.Helper()

	path := filepath.Join(
		root,
		filepath.FromSlash(project.CandidateCredentialSeam),
	)
	forged := bytes.Replace(
		seam,
		[]byte("import (\n"),
		[]byte("import (\n\t_ \"example.test/credentials/internal/storage/"+
			"identity\"\n"),
		1,
	)

	if bytes.Equal(forged, seam) {
		t.Fatal("seam fixture shape changed")
	}

	if err := os.WriteFile(path, forged, 0o644); err != nil {
		t.Fatal(err)
	}

	report := project.InspectArchitecture(root)

	rejected := false

	for _, diagnostic := range report.Diagnostics {
		if diagnostic.ID == "ARCH609" {
			rejected = true
		}
	}

	if report.Passed || !rejected {
		t.Fatalf("seam authority import accepted: %+v", report.Diagnostics)
	}

	if err := os.WriteFile(path, seam, 0o644); err != nil {
		t.Fatal(err)
	}

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("restored seam rejected: %+v", report.Diagnostics)
	}
}
