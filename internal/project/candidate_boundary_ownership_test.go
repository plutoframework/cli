package project

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCandidateReflectionOnlyCanonicalSchemaOwner(t *testing.T) {
	for _, path := range []string{
		"internal/api/schema.go",
		"internal/access/caller_gen.go",
		"internal/api/extra.go",
	} {
		root := candidateReviewFixture(
			t,
			map[string]string{
				path: `package api

import "reflect"

var t = reflect.TypeOf(0)
`,
			},
		)
		diagnostics, _ := inspectCandidateBoundaries(
			root,
			candidateReviewModule,
			[]byte(candidateReviewMetadata(t)),
		)

		blocked := candidateReviewHas(diagnostics, "ARCH605", path)
		if blocked != (path != "internal/api/schema.go") {
			t.Fatalf("reflection owner %s: %+v", path, diagnostics)
		}
	}
}

func TestCandidateDeclaredSkippedDirectoryCannotHideInitializer(t *testing.T) {
	for _, group := range []string{"testdata", "dist", "vendor"} {
		t.Run(group, func(t *testing.T) {
			metadata := strings.ReplaceAll(
				candidateReviewMetadata(t),
				"sales",
				group,
			)
			path := "internal/domain/" + group + "/extra.go"
			root := candidateReviewFixture(
				t,
				map[string]string{path: "package " + group + ";func init(){}"},
			)

			// Remove unrelated original sales fixtures so the assertion tests
			// only this namespace.
			if err := os.RemoveAll(
				filepath.Join(root, "internal/domain/sales"),
			); err != nil {
				t.Fatal(err)
			}

			diagnostics, _ := inspectCandidateBoundaries(
				root,
				candidateReviewModule,
				[]byte(metadata),
			)
			if !candidateReviewHas(diagnostics, "ARCH602", path) {
				t.Fatalf(
					"declared ignored-name package hid initializer: %+v",
					diagnostics,
				)
			}
		})
	}
}

func TestCandidateOwnershipBranches(t *testing.T) {
	for _, test := range []struct {
		path, source, id, marker string
	}{
		{
			"internal/access/policy.go",
			`package access

func erase(c *Caller) {

	c.callerIdentity = nil
}
`,
			"ARCH604",
			"managed sibling altered opaque caller field",
		},
		{
			"internal/app/extra.go",
			`package app

func init() {}
`,
			"ARCH602",
			"unowned initializer accepted",
		},
	} {
		root := candidateReviewFixture(
			t,
			map[string]string{test.path: test.source},
		)

		diagnostics, _ := inspectCandidateBoundaries(
			root,
			candidateReviewModule,
			[]byte(candidateReviewMetadata(t)),
		)
		if len(diagnostics) != 1 || diagnostics[0].ID != test.id ||
			diagnostics[0].File != test.path {
			t.Fatalf(
				"%s: expected only %s at %s, got %+v",
				test.marker,
				test.id,
				test.path,
				diagnostics,
			)
		}
	}
}
