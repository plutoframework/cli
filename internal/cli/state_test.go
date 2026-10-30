package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"

	"github.com/spf13/cobra"
)

func TestStateScaffoldConsumers(t *testing.T) {
	for _, tenancy := range []string{"single", "multi"} {
		t.Run(tenancy, func(t *testing.T) {
			root := temporary(t)

			args := []string{
				"init", ".", "--name", "state-test",
				"--module", "example.com/state-test", "--no-mise", "--no-fnox",
			}
			if tenancy == "multi" {
				args = append(args, "--multi-tenant")
			}

			mustInvoke(t, root, args...)

			before := snapshot(t, root)
			mustInvoke(t, root, "add", "state", "purchases", "--dry-run")
			assertUnchanged(t, before, snapshot(t, root))

			for _, group := range []string{"../escape", "Bad", "http", "", "type"} {
				if _, err := invoke(
					t,
					root,
					"add",
					"state",
					group,
				); err == nil {
					t.Fatalf("invalid group accepted: %q", group)
				}

				assertUnchanged(t, before, snapshot(t, root))
			}

			mustInvoke(t, root, "add", "state", "purchases")

			before = snapshot(t, root)
			if _, err := invoke(
				t,
				root,
				"add",
				"state",
				"purchases",
			); err == nil {
				t.Fatal("authored lifecycle overwritten")
			}

			assertUnchanged(t, before, snapshot(t, root))

			// Names that overlap library imports must still produce valid Go.
			mustInvoke(t, root, "add", "state", "state")
			external(t, root, "go", "test", "-race", "./...")
			external(t, root, "go", "test", "./internal/domain/purchases",
				"-run=^$", "-fuzz=FuzzLifecycle", "-fuzztime=1s", "-parallel=2")
			mustInvoke(t, root, "fmt")
			mustInvoke(t, root, "vet")
		})
	}
}

func TestStatePreflight(t *testing.T) {
	root := temporary(t)
	mustInvoke(t, root, "init", ".", "--name", "state-test",
		"--module", "example.com/state-test", "--no-mise", "--no-fnox")

	destination := filepath.Join(root, "internal/domain/purchases")
	if err := os.Symlink(temporary(t), destination); err != nil {
		t.Fatal(err)
	}

	if _, err := invoke(t, root, "add", "state", "purchases"); err == nil {
		t.Fatal("symlink accepted")
	}

	if err := os.Remove(destination); err != nil {
		t.Fatal(err)
	}

	// An older pin is rejected before creating authored code or modifying sums.
	external(
		t,
		root,
		"go",
		"get",
		"github.com/plutoframework/pluto-core@v0.3.0",
	)

	before := snapshot(t, root)
	if _, err := invoke(t, root, "add", "state", "purchases"); err == nil {
		t.Fatal("unavailable state dependency accepted")
	}

	assertUnchanged(t, before, snapshot(t, root))
}

func TestStateWizard(t *testing.T) {
	model := stateForm(wizardModel{}, project.Manifest{}, &cobra.Command{}, nil)
	values := map[string]string{"group": "purchases", "dry-run": "true"}

	args, err := model.build(values)
	if err != nil || !reflect.DeepEqual(args,
		[]string{"add", "state", "purchases", "--dry-run=true"}) {
		t.Fatalf("wizard args: %v %v", args, err)
	}

	values["group"] = "../escape"
	if _, err := model.build(values); err == nil {
		t.Fatal("wizard accepted invalid group")
	}
}

func TestStateWizardDispatch(t *testing.T) {
	t.Setenv("CI", "")

	root := temporary(t)
	mustInvoke(t, root, "init", ".", "--name", "state-test",
		"--module", "example.com/state-test", "--no-mise", "--no-fnox")

	before := snapshot(t, root)

	var output bytes.Buffer

	calls := 0
	command := New(Options{
		Directory: root,
		In:        strings.NewReader(""),
		Out:       &output,
		Err:       &output,
		terminal:  func(io.Reader, io.Writer) bool { return true },
		wizard: func(_ context.Context, _ io.Reader, _ io.Writer, model wizardModel) (wizardModel, error) {
			calls++

			if model.kind != "state" {
				t.Fatal("wrong wizard", model.kind)
			}

			assertUnchanged(t, before, snapshot(t, root))

			return acceptWizard(t, model, map[string]string{
				"group": "purchases", "dry-run": "false",
			}), nil
		},
	})
	command.SetArgs([]string{"add", "state"})

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err, output.String())
	}

	if calls != 1 || !strings.Contains(output.String(), "Equivalent command:") {
		t.Fatal("wizard failed fresh noninteractive dispatch", calls)
	}

	if _, err := os.Stat(
		filepath.Join(root, "internal/domain/purchases/lifecycle.go"),
	); err != nil {
		t.Fatal("wizard did not scaffold lifecycle", err)
	}
}
