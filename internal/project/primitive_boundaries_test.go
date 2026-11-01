package project

import (
	"os"
	"path/filepath"
	"testing"
)

func primitiveFixture(t *testing.T, files map[string]string) string {
	t.Helper()

	root := t.TempDir()
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

func TestPrimitivePlacementAndHelperBypasses(t *testing.T) {
	for _, test := range []struct{ name, source, id string }{
		{"normalizer", `import s "strings"; var normalize=s.TrimSpace`, "ARCH501"},
		{"cache", `import c "github.com/plutoframework/pluto-core/cache"; var newLoader=c.NewLoader[string]`, "ARCH502"},
		{"collection", `import p "github.com/plutoframework/pluto-core/pagination"; var finish=p.Finish[string]`, "ARCH504"},
	} {
		for _, helper := range []bool{false, true} {
			t.Run(
				test.name+map[bool]string{true: " helper", false: " direct"}[helper],
				func(t *testing.T) {
					path := "internal/operations/use.go"

					files := map[string]string{}
					if helper {
						files[path] = `package operations; import _ "example.test/app/internal/helper"`
						path = "internal/helper/use.go"
					}

					files[path] = "package fixture; " + test.source
					root := primitiveFixture(t, files)
					diagnostics, _ := inspectPrimitiveBoundaries(
						root,
						"example.test/app",
						nil,
					)
					found := false

					for _, d := range diagnostics {
						if d.ID == test.id && d.File == path && d.Line > 0 &&
							d.Repair != "" {
							found = true
						}
					}

					if !found {
						t.Fatalf("missing %s: %+v", test.id, diagnostics)
					}
				},
			)
		}
	}
}

func TestPrimitiveOwnersRemainAllowed(t *testing.T) {
	root := primitiveFixture(t, map[string]string{
		"internal/domain/value.go":            `package domain; import "strings"; func Normalize(v string)string{return strings.TrimSpace(v)}`,
		"internal/storage/value.go":           `package storage; import c "github.com/plutoframework/pluto-core/cache"; var factory=c.NewLoader[string]`,
		"internal/storage/page.go":            `package storage; import p "github.com/plutoframework/pluto-core/pagination"; var finish=p.Finish[string]`,
		"internal/operations/use.go":          `package operations; import "example.test/app/internal/domain"; func Work(v string)string{return domain.Normalize(v)}`,
		"internal/operations/ignored_test.go": `package operations; import "strings"; var normalize=strings.TrimSpace`,
	})

	diagnostics, _ := inspectPrimitiveBoundaries(root, "example.test/app", nil)
	if len(diagnostics) != 0 {
		t.Fatalf("valid role placement rejected: %+v", diagnostics)
	}
}

func TestPrimitiveDeclarationPrerequisites(t *testing.T) {
	root := primitiveFixture(t, map[string]string{
		"internal/operations/work.go":  `package operations; func Work(){}`,
		"internal/transport/replay.go": `package transport; import replay "github.com/plutoframework/pluto-core/httpidem"; var build=replay.New`,
		"internal/jobs/job.go":         `package jobs; import queue "github.com/plutoframework/pluto-jobs"; var decode=queue.Decode[string]`,
	})

	valid := []byte(`[[operations]]
group="records"
action="Update"
write=true
idempotency="required"
[[jobs]]
operation="records.Update"
`)
	for _, test := range []struct {
		name           string
		manifest       []byte
		replay, effect bool
	}{
		{"legacy", nil, true, true},
		{"declared", valid, false, false},
		{"missing replay", []byte("[[operations]]\ngroup='records'\naction='Update'\nwrite=true\n"), true, true},
		{"write without replay", []byte("[[operations]]\ngroup='records'\naction='Update'\nwrite=true\nidempotency='none'\n"), true, true},
		{"unknown effect", []byte("[[operations]]\ngroup='records'\naction='Read'\n[[jobs]]\noperation='other.Unknown'\n"), false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			diagnostics, _ := inspectPrimitiveBoundaries(
				root,
				"example.test/app",
				test.manifest,
			)

			found := map[string]bool{}
			for _, d := range diagnostics {
				found[d.ID] = true
			}

			if found["ARCH503"] != test.replay ||
				found["ARCH506"] != test.effect {
				t.Fatalf("incorrect prerequisites: %+v", diagnostics)
			}
		})
	}
}

func TestPrimitiveUnboundedResultAndDotImport(t *testing.T) {
	root := primitiveFixture(t, map[string]string{
		"internal/operations/export.go": `//go:build future

package operations
import . "strings"
func Export() []string {return nil}
`,
	})
	diagnostics, _ := inspectPrimitiveBoundaries(root, "example.test/app", nil)

	found := map[string]bool{}
	for _, d := range diagnostics {
		found[d.ID] = true
	}

	if !found["ARCH505"] || !found["ARCH509"] {
		t.Fatalf("unbounded/dot import bypass: %+v", diagnostics)
	}
}

func TestPrimitiveNamedSliceCannotHideExportBound(t *testing.T) {
	for _, result := range []string{"Rows", "Alias", "*Alias", "shared.Rows", "Generic[string,int]"} {
		root := primitiveFixture(t, map[string]string{
			"internal/operations/export.go": `package operations
import "example.test/app/internal/shared"
type Rows []string
type Alias = Rows
type Generic[K comparable,V any] []V
func Export() ` + result + ` {return nil}
`,
			"internal/shared/rows.go": `package shared; type Rows []string`,
		})
		diagnostics, _ := inspectPrimitiveBoundaries(
			root,
			"example.test/app",
			nil,
		)
		found := false

		for _, d := range diagnostics {
			if d.ID == "ARCH505" {
				found = true
			}
		}

		if !found {
			t.Fatalf("named raw slice %s escaped", result)
		}
	}
}
