package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

func TestBlueprintChecksumsPreservePinsAndRejectConflict(t *testing.T) {
	current := []byte("example.test/other v1.0.0 h1:synthetic\n")

	output, err := blueprintChecksums(current)
	if err != nil || !bytes.HasPrefix(output, current) {
		t.Fatalf("authored checksum changed: %v", err)
	}

	again, err := blueprintChecksums(output)
	if err != nil || !bytes.Equal(again, output) {
		t.Fatalf("checksum configuration drifted: %v", err)
	}

	line := strings.Split(strings.TrimSpace(project.AgentGoSum), "\n")[0]
	fields := strings.Fields(line)

	wrong := fields[0] + " " + fields[1] + " h1:wrong\n"
	for _, input := range []string{wrong, wrong + line + "\n"} {
		if result, err := blueprintChecksums(
			[]byte(input),
		); err == nil ||
			result != nil {
			t.Fatalf("conflicting checksum accepted: %v", err)
		}
	}
}

func TestReadOperationBlueprintRequiresExplicitContracts(t *testing.T) {
	for _, test := range []struct{ name, source, want string }{
		{"empty", "", "must declare"},
		{"unknown section", "[[operation]]\ngroup='sales'\naction='Approve'\n", "invalid blueprint"},
		{"unknown obligation", "[[resources]]\ngroup='sales'\nname='Invoice'\nallow_unsafe=true\n", "invalid blueprint"},
		{"malformed", "[[operations\n", "invalid blueprint"},
		{"oversized", strings.Repeat(" ", (2<<20)+1), "2 MiB"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "blueprint.toml")
			if err := os.WriteFile(
				path,
				[]byte(test.source),
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			if _, err := readOperationBlueprint(
				path,
			); err == nil ||
				!strings.Contains(err.Error(), test.want) {
				t.Fatalf("expected %s rejection: %v", test.want, err)
			}
		})
	}

	if _, err := readOperationBlueprint(
		t.TempDir(),
	); err == nil ||
		!strings.Contains(err.Error(), "regular") {
		t.Fatalf("directory accepted: %v", err)
	}
}

func TestReadOperationBlueprintKeepsIndependentAuthority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blueprint.toml")

	const source = `[[resources]]
group = "sales"
name = "Invoice"
[[operations]]
group = "sales"
action = "Approve"
permission = "invoice:approve"
write = true
idempotency = "required"
[[operation_contracts]]
id = "sales.Approve"
resource = "sales.Invoice"
kind = "transition"
permission = "invoice:approve"
resource_policy = "tenant"
event = "approve"
idempotency = "required"
[[jobs]]
group = "deliveries"
action = "Approve"
kind = "invoice.approve"
operation = "sales.Approve"
`
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	blueprint, err := readOperationBlueprint(path)
	if err != nil {
		t.Fatal(err)
	}

	if len(blueprint.Resources) != 1 || len(blueprint.Operations) != 1 ||
		len(blueprint.OperationContracts) != 1 ||
		len(blueprint.Jobs) != 1 {
		t.Fatalf("declarations lost: %+v", blueprint)
	}

	if blueprint.Operations[0].ID() != blueprint.OperationContracts[0].ID ||
		blueprint.Jobs[0].Operation != blueprint.Operations[0].ID() {
		t.Fatal("entry reference lost its independent operation")
	}
}
