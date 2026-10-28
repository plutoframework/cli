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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/pflag"
)

func wizardKey(model wizardModel, key tea.KeyMsg) wizardModel {
	result, _ := model.Update(key)
	return result.(wizardModel)
}

func acceptWizard(
	t *testing.T,
	model wizardModel,
	values map[string]string,
) wizardModel {
	t.Helper()

	if model.menu {
		if action := values["action"]; action != "" {
			model.fields[0].value = action
		}

		return wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	}

	for !model.review {
		field := &model.fields[model.index]
		if value, ok := values[field.id]; ok {
			field.value = value
		}

		model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	}

	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.confirmed {
		t.Fatalf("wizard validation: %s", model.message)
	}

	return model
}

func TestWizardDispatchesSharedGenerators(t *testing.T) {
	t.Setenv("CI", "")

	root := temporary(t)

	var output bytes.Buffer

	calls := []string{}
	options := Options{
		Directory: root,
		In:        strings.NewReader(""),
		Out:       &output,
		Err:       &output,
		terminal:  func(io.Reader, io.Writer) bool { return true },
		wizard: func(_ context.Context, _ io.Reader, _ io.Writer, m wizardModel) (wizardModel, error) {
			calls = append(calls, m.kind)
			values := map[string]string{}

			switch m.kind {
			case "menu":
				values["action"] = "init"
			case "init":
				values = map[string]string{
					"directory":    "mission",
					"name":         "mission",
					"module":       "example.com/mission",
					"env-prefix":   "MISSION_",
					"cache":        "valkey",
					"multi-tenant": "true",
					"no-mise":      "true",
					"no-fnox":      "true",
					"no-docker":    "true",
				}
			case "endpoint":
				values = map[string]string{
					"group":                  "orbit",
					"action":                 "List",
					"method":                 "GET",
					"description":            "List authorized orbital records.",
					"permission":             "orbit:read",
					"feature":                "orbit.list",
					"feature-client-visible": "true",
				}
			case "job":
				values = map[string]string{
					"group":  "orbit",
					"action": "Deliver",
					"kind":   "orbit.deliver",
				}
			}

			return acceptWizard(t, m, values), nil
		},
	}
	invokeOptions := func(args ...string) {
		command := New(options)
		command.SetArgs(args)

		if err := command.ExecuteContext(context.Background()); err != nil {
			t.Fatal(err, output.String())
		}
	}
	invokeOptions()

	app := filepath.Join(root, "mission")
	options.Directory = app

	invokeOptions("add", "endpoint")
	invokeOptions("add", "job")

	_, m, err := project.Discover(app)
	if err != nil {
		t.Fatal(err)
	}

	if m.Capabilities.Cache != "valkey" || m.Capabilities.Tenancy != "multi" ||
		m.Development.Docker ||
		m.Development.Mise ||
		m.Development.Fnox {
		t.Fatal("wizard preferences lost", m)
	}

	if len(m.Endpoints) != 1 || !m.Endpoints[0].Collection ||
		m.Endpoints[0].Permission != "orbit:read" ||
		!m.Endpoints[0].ClientVisible ||
		len(m.Jobs) != 1 {
		t.Fatal("wizard metadata lost", m)
	}

	if !reflect.DeepEqual(calls, []string{"menu", "init", "endpoint", "job"}) {
		t.Fatal("wizard recursed", calls)
	}

	if !strings.Contains(output.String(), "Equivalent command:") ||
		!strings.Contains(output.String(), "--no-input") {
		t.Fatal("missing reproducible command")
	}

	external(t, app, "go", "test", "./...")
}

func TestWizardTerminalRouting(t *testing.T) {
	for _, test := range []struct {
		name                  string
		args                  []string
		terminal, ci          bool
		wantWizard, wantError bool
	}{
		{name: "bare terminal", terminal: true, wantWizard: true, wantError: true},
		{name: "add terminal", args: []string{"add"}, terminal: true, wantWizard: true, wantError: true},
		{name: "pipe help"},
		{name: "no input", args: []string{"--no-input"}, terminal: true},
		{name: "CI help", terminal: true, ci: true},
		{name: "explicit pipe", args: []string{"--interactive"}, wantError: true},
		{name: "contradictory modes", args: []string{"--interactive", "--no-input"}, terminal: true, wantError: true},
		{name: "missing endpoint in pipe", args: []string{"add", "endpoint"}, wantError: true},
		{name: "partial raw command", args: []string{"add", "endpoint", "orbit"}, terminal: true, wantError: true},
		{name: "flagged command stays raw", args: []string{"init", "--dry-run", "--name", "mission"}, terminal: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("CI", "")

			if test.ci {
				t.Setenv("CI", "true")
			}

			root := temporary(t)

			var output bytes.Buffer

			called := false
			command := New(Options{
				Directory: root,
				In:        strings.NewReader(""),
				Out:       &output,
				Err:       &output,
				terminal:  func(io.Reader, io.Writer) bool { return test.terminal },
				wizard: func(_ context.Context, _ io.Reader, _ io.Writer, m wizardModel) (wizardModel, error) {
					called = true
					return wizardKey(m, tea.KeyMsg{Type: tea.KeyEsc}), nil
				},
			})
			command.SetArgs(test.args)

			err := command.ExecuteContext(context.Background())
			if (err != nil) != test.wantError || called != test.wantWizard {
				t.Fatal("routing", called, err, output.String())
			}

			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("routing/cancellation wrote files")
			}
		})
	}

	if terminalStreams(strings.NewReader(""), io.Discard) {
		t.Fatal("pipe treated as terminal")
	}
}

func TestWizardPrefillsAndProtectsOptionLikeDirectory(t *testing.T) {
	root := temporary(t)

	var (
		output bytes.Buffer
		plan   []string
	)

	options := Options{
		Directory: root,
		In:        strings.NewReader(""),
		Out:       &output,
		Err:       &output,
		terminal:  func(io.Reader, io.Writer) bool { return true },
		wizard: func(_ context.Context, _ io.Reader, _ io.Writer, m wizardModel) (wizardModel, error) {
			values := m.values()
			if values["cache"] != "memory" || values["name"] != "mission" ||
				values["directory"] != "--no-input=false" {
				t.Fatal("prefill lost", values)
			}

			m = acceptWizard(t, m, map[string]string{"dry-run": "true"})
			plan = m.args

			return m, nil
		},
	}
	command := New(options)
	command.SetArgs(
		[]string{
			"--interactive",
			"init",
			"--cache",
			"memory",
			"--name",
			"mission",
			"--",
			"--no-input=false",
		},
	)

	if err := command.ExecuteContext(context.Background()); err != nil {
		t.Fatal(err)
	}

	if !reflect.DeepEqual(
		plan[len(plan)-2:],
		[]string{"--", "--no-input=false"},
	) {
		t.Fatal("directory interpreted as a flag", plan)
	}

	entries, _ := os.ReadDir(root)
	if len(entries) != 0 {
		t.Fatal("dry run wrote files")
	}
}

func TestWizardReviewValidationEditingAndCancellation(t *testing.T) {
	command := New(Options{})

	model := initForm(
		wizardModel{kind: "init", width: 80, height: 24},
		Options{Directory: "/tmp"},
		command,
		nil,
	)
	for !model.review {
		switch model.fields[model.index].id {
		case "name":
			model.fields[model.index].value = "../unsafe"
		case "module":
			model.fields[model.index].value = "example.com/mission"
		case "env-prefix":
			model.fields[model.index].value = "MISSION_"
		}

		model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	}

	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.confirmed || !strings.Contains(model.message, "kebab-case") {
		t.Fatal("invalid plan accepted", model.message)
	}

	model.selected = 1
	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyCtrlU})
	model = wizardKey(
		model,
		tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("mission")},
	)
	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})

	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.confirmed ||
		!strings.Contains(strings.Join(model.args, " "), "--name=mission") {
		t.Fatal("review edit lost")
	}

	model.fields[1].value = "mission\x1b[31m"
	if _, err := model.plan(); err == nil {
		t.Fatal("control injection accepted")
	}

	model.confirmed = false

	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyCtrlC})
	if !model.cancelled || model.View() != "" {
		t.Fatal("cancel failed")
	}
}

func TestWizardTextChoiceAndBoundedView(t *testing.T) {
	model := wizardModel{
		fields: []wizardField{textField("text", "Text", "", "ab")},
		width:  32,
		height: 24,
	}
	model.focus(0)

	for _, key := range []tea.KeyMsg{{Type: tea.KeyLeft}, {Type: tea.KeyRunes, Runes: []rune("é")}, {Type: tea.KeyBackspace}, {Type: tea.KeyDelete}, {Type: tea.KeySpace}, {Type: tea.KeyRunes, Runes: []rune("界\n\x1b"), Paste: true}} {
		model = wizardKey(model, key)
	}

	if model.fields[0].value != "a 界" {
		t.Fatal("text editing", model.fields[0].value)
	}

	model = wizardKey(
		model,
		tea.KeyMsg{
			Type:  tea.KeyRunes,
			Runes: []rune(strings.Repeat("a", 3000)),
		},
	)
	if len([]rune(model.fields[0].value)) != 2048 {
		t.Fatal("unbounded paste")
	}

	for _, line := range strings.Split(model.View(), "\n") {
		if len([]rune(line)) > 32 {
			t.Fatal("view overflow")
		}
	}

	model.fields = []wizardField{boolField("toggle", "Toggle", "", "false")}
	model.focus(0)

	model = wizardKey(model, tea.KeyMsg{Type: tea.KeyDown})
	if model.fields[0].value != "true" {
		t.Fatal("choice key ignored")
	}

	if shellCommand(
		[]string{"--name=it's fine"},
	) != "pluto '--name=it'\\''s fine'" {
		t.Fatal("display quoting changed")
	}
}

// New generator flags must be represented in the wizard, so human users do not
// silently miss a capability added to the scriptable CLI.
func TestWizardCoversGeneratorFlags(t *testing.T) {
	root := New(Options{})
	for _, path := range [][]string{{"init"}, {"add", "endpoint"}, {"add", "job"}} {
		command, _, err := root.Find(path)
		if err != nil {
			t.Fatal(err)
		}

		model := wizardModel{kind: path[len(path)-1]}
		switch model.kind {
		case "init":
			model = initForm(model, Options{Directory: "."}, command, nil)
		case "endpoint":
			model = endpointForm(model, project.Manifest{}, command, nil)
		case "job":
			model = jobForm(model, project.Manifest{}, command, nil)
		}

		fields := map[string]bool{}
		for _, field := range model.fields {
			fields[field.id] = true
		}

		command.Flags().VisitAll(func(flag *pflag.Flag) {
			if flag.Name != "interactive" && flag.Name != "no-input" &&
				!fields[flag.Name] {
				t.Errorf(
					"%s flag --%s has no wizard field",
					command.CommandPath(),
					flag.Name,
				)
			}
		})
	}
}

func TestWizardMaintenancePlans(t *testing.T) {
	root := New(Options{})
	for _, test := range []struct {
		kind string
		want []string
	}{
		{"fmt", []string{"fmt"}},
		{"vet", []string{"vet", "--staged=false"}},
		{"openapi", []string{"openapi", "--check=false"}},
		{"hook", []string{"hook", "install"}},
	} {
		model := maintenanceForm(wizardModel{kind: test.kind}, root)

		plan, err := model.plan()
		if err != nil || !reflect.DeepEqual(plan, test.want) {
			t.Fatal(test.kind, plan, err)
		}
	}
}
