package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func lifecycleFixture(t *testing.T, extra map[string]string) string {
	t.Helper()

	root := t.TempDir()

	files := map[string]string{
		"internal/domain/order.go": `package domain

import lifecycle "github.com/plutoframework/pluto-core/state"

type Phase string
type Alias = Phase
type Event string
type Order struct {
	Phase Alias
}

func Definition() lifecycle.Definition[Phase, Event, bool] {
	return lifecycle.Definition[Phase, Event, bool]{}
}
`,
	}
	for path, source := range extra {
		files[path] = source
	}

	for path, source := range files {
		absolute := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(absolute, []byte(source), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return root
}

func TestLifecycleBoundariesRejectMutableEntityAndRawDataEntry(t *testing.T) {
	for _, parameter := range []string{
		"domain.Order",
		"*domain.Order",
		"[]domain.Order",
		"map[string]domain.Order",
		"Copy",
		"Envelope",
	} {
		t.Run(parameter, func(t *testing.T) {
			root := lifecycleFixture(t, map[string]string{
				"internal/storage/store.go": `package storage
import "example.test/app/internal/domain"
type Copy domain.Order
type Envelope struct { Value *Copy }
func Persist(input ` + parameter + `) error { return nil }
`,
			})
			diagnostics, _ := inspectLifecycleBoundaries(
				root,
				"example.test/app",
			)

			found := map[string]bool{}
			for _, d := range diagnostics {
				found[d.ID] = true
				if d.Line <= 0 || d.Repair == "" {
					t.Fatalf("incomplete finding: %+v", d)
				}
			}

			if !found["ARCH301"] || !found["ARCH303"] {
				t.Fatalf("missing mutable/raw write findings: %+v", diagnostics)
			}
		})
	}
}

func TestLifecycleNextReferencesCannotLeaveDomain(t *testing.T) {
	for _, expression := range []string{
		"_, _ = s.machine.Next(\"draft\", \"submit\", true)",
		"m := s.machine; _, _ = m.Next(\"draft\", \"submit\", true)",
		"next := s.machine.Next; _, _ = next(\"draft\", \"submit\", true)",
		"next := (*Machine).Next; _, _ = next(s.machine, \"draft\", " +
			"\"submit\", true)",
		"var m *Machine; _, _ = m.Next(\"draft\", \"submit\", true)",
		"var m = s.machine; _, _ = m.Next(\"draft\", \"submit\", true)",
	} {
		t.Run(expression, func(t *testing.T) {
			root := lifecycleFixture(t, map[string]string{
				"internal/operations/order.go": `package operations
import (
 "example.test/app/internal/domain"
 lifecycle "github.com/plutoframework/pluto-core/state"
)
type Machine = lifecycle.Machine[domain.Phase,domain.Event,bool]
type Service struct { machine *Machine }
func (s *Service) Apply() { ` + expression + ` }
`,
			})
			diagnostics, _ := inspectLifecycleBoundaries(
				root,
				"example.test/app",
			)
			found := false

			for _, d := range diagnostics {
				if d.ID == "ARCH302" &&
					d.File == "internal/operations/order.go" {
					found = true
				}
			}

			if !found {
				t.Fatalf("raw decision reference escaped: %+v", diagnostics)
			}
		})
	}
}

func TestLifecyclePackageVariableAndMethodCapture(t *testing.T) {
	for _, source := range []string{
		`
var machine *lifecycle.Machine[domain.Phase, domain.Event, bool]

func Bypass() {

	machine.Next("draft", "submit", true)
}
`,
		`
var machine *lifecycle.Machine[domain.Phase, domain.Event, bool]
var next = machine.Next

func Bypass() {

	next("draft", "submit", true)
}
`,
	} {
		root := lifecycleFixture(t, map[string]string{
			"internal/operations/global.go": `package operations
import (
 "example.test/app/internal/domain"
 lifecycle "github.com/plutoframework/pluto-core/state"
)
` + source,
		})
		diagnostics, _ := inspectLifecycleBoundaries(root, "example.test/app")
		found := false

		for _, d := range diagnostics {
			if d.ID == "ARCH302" {
				found = true
			}
		}

		if !found {
			t.Fatalf("package variable bypass missed: %+v", diagnostics)
		}
	}
}

func TestLifecycleControlledFieldsAndEvaluatedPort(t *testing.T) {
	root := lifecycleFixture(t, map[string]string{
		"internal/domain/order.go": `package domain

import lifecycle "github.com/plutoframework/pluto-core/state"

type Phase string
type Event string
type Order struct {
	phase Phase
}

func (o Order) Phase() Phase {

	return o.phase
}
func Definition() lifecycle.Definition[Phase, Event, bool] {
	return lifecycle.Definition[Phase, Event, bool]{}
}
func Preview(
	m *lifecycle.Machine[Phase, Event, bool],

) {

	_, _ = m.Next(
		"draft",
		"submit",
		true,
	)
}
`,
		"internal/storage/store.go": `package storage

import (
	"example.test/app/internal/domain"
	lifecycle "github.com/plutoframework/pluto-core/state"
)

func Commit(
	change lifecycle.Evaluated[domain.Phase, domain.Event],

) error {

	return nil
}
`,
		"internal/operations/list.go": `package operations

type Iterator struct{}

func (*Iterator) Next() bool {

	return false
}
func Read(i *Iterator) {

	i.Next()
}
`,
	})

	diagnostics, unassessed := inspectLifecycleBoundaries(
		root,
		"example.test/app",
	)
	if len(diagnostics) != 0 {
		t.Fatalf("controlled shape rejected: %+v", diagnostics)
	}

	if !strings.Contains(
		strings.Join(unassessed, " "),
		"full assignment flow",
	) {
		t.Fatal("missing semantic coverage limit")
	}
}

func TestLifecycleHelperPackageAndTaggedVariantRemainInspected(t *testing.T) {
	root := lifecycleFixture(t, map[string]string{
		"internal/helper/variant.go": `//go:build future

package helper
import (
 "example.test/app/internal/domain"
 lifecycle "github.com/plutoframework/pluto-core/state"
)
func Bypass(m *lifecycle.Machine[domain.Phase,domain.Event,bool]) {
 _, _ = m.Next("draft","submit",true)
}
`,
		"internal/domain/ignore_test.go": `package domain
type TestOnly struct { Phase Phase }
`,
	})
	diagnostics, _ := inspectLifecycleBoundaries(root, "example.test/app")
	found := false

	for _, d := range diagnostics {
		if strings.HasSuffix(d.File, "_test.go") {
			t.Fatal("test code included")
		}

		if d.ID == "ARCH302" && d.File == "internal/helper/variant.go" {
			found = true
		}
	}

	if !found {
		t.Fatalf("helper/tag bypass missed: %+v", diagnostics)
	}
}
