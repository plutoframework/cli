package project

import (
	"strings"
	"testing"
)

func candidateCredentialMetadata(t *testing.T, local bool) string {
	t.Helper()

	metadata := candidateReviewMetadata(t)
	if !local {
		return metadata
	}

	return strings.Replace(
		metadata,
		"[capabilities]\n",
		"[capabilities]\nlocal_credentials=true\n",
		1,
	)
}

func candidateSeamSource(imports ...string) string {
	var source strings.Builder

	source.WriteString("package credentials\n\nimport (\n")

	for index, path := range imports {
		source.WriteString("\tx")
		source.WriteString(strings.Repeat("x", index))
		source.WriteString(" \"")
		source.WriteString(path)
		source.WriteString("\"\n")
	}

	source.WriteString(")\n")

	return source.String()
}

func TestCandidateCredentialSeamAllowsDeliveryOnly(t *testing.T) {
	root := candidateReviewFixture(
		t,
		map[string]string{
			CandidateCredentialSeam: candidateSeamSource(
				"context",
				"net/http",
				"net/smtp",
				"os",
				"example.org/mail/provider",
				candidateSeamPassword,
			),
		},
	)
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(candidateCredentialMetadata(t, true)),
	)

	for _, id := range []string{"ARCH602", "ARCH608", "ARCH609"} {
		if candidateReviewHas(diagnostics, id, CandidateCredentialSeam) {
			t.Fatalf("allowed seam rejected by %s: %+v", id, diagnostics)
		}
	}
}

func TestCandidateCredentialSeamRejectsAuthorityImports(t *testing.T) {
	for _, path := range []string{
		candidateReviewModule,
		candidateReviewModule + "/internal/access",
		candidateReviewModule + "/internal/app",
		candidateReviewModule + "/internal/storage/identity",
		candidateReviewModule + "/internal/transport",
		"github.com/plutoframework/pluto-auth",
		"github.com/plutoframework/pluto-auth/httpauth",
		"github.com/plutoframework/pluto-auth/methods/oidc",
		"github.com/plutoframework/pluto-auth/methods/password/passwordhttp",
		"github.com/plutoframework/pluto-auth/stores/postgres",
		"github.com/plutoframework/pluto-rbac",
		"github.com/plutoframework/pluto-rbac/identity",
		"github.com/plutoframework/pluto-core/state",
		"github.com/plutoframework/pluto-jobs",
		"github.com/jackc/pgx/v5/pgxpool",
		"github.com/redis/go-redis/v9",
		"database/sql",
		"unsafe",
		"reflect",
		"plugin",
		"C",
	} {
		t.Run(path, func(t *testing.T) {
			root := candidateReviewFixture(
				t,
				map[string]string{
					CandidateCredentialSeam: candidateSeamSource(
						"context",
						path,
					),
				},
			)
			diagnostics, _ := inspectCandidateBoundaries(
				root,
				candidateReviewModule,
				[]byte(candidateCredentialMetadata(t, true)),
			)

			if !candidateReviewHas(
				diagnostics,
				"ARCH609",
				CandidateCredentialSeam,
			) {
				t.Fatalf("restricted seam import accepted: %+v", diagnostics)
			}
		})
	}
}

// The seam is one exact directory and exists only with local credentials.
// Neighboring paths and other profiles remain closed managed ownership.
func TestCandidateCredentialSeamIsExact(t *testing.T) {
	source := candidateSeamSource(candidateSeamPassword)

	for _, test := range []struct {
		name, path string
		local      bool
	}{
		{"disabled", CandidateCredentialSeam, false},
		{"nested", "internal/auth/credentials/extra/extra.go", true},
		{"sibling", "internal/auth/delivery/delivery.go", true},
		{"parent", "internal/auth/auth.go", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := candidateReviewFixture(
				t,
				map[string]string{test.path: source},
			)
			diagnostics, _ := inspectCandidateBoundaries(
				root,
				candidateReviewModule,
				[]byte(candidateCredentialMetadata(t, test.local)),
			)

			if !candidateReviewHas(diagnostics, "ARCH602", test.path) {
				t.Fatalf("seam escaped closed ownership: %+v", diagnostics)
			}
		})
	}

	root := candidateReviewFixture(
		t,
		map[string]string{
			CandidateCredentialSeam: "package credentials\n\n" +
				"import _ \"unsafe\"\n\n" +
				"//go:linkname issue example.test/candidate/internal/app.x\n" +
				"func issue()\n",
		},
	)
	diagnostics, _ := inspectCandidateBoundaries(
		root,
		candidateReviewModule,
		[]byte(candidateCredentialMetadata(t, true)),
	)

	if !candidateReviewHas(diagnostics, "ARCH605", CandidateCredentialSeam) {
		t.Fatalf("seam linkname escaped package privacy: %+v", diagnostics)
	}
}
