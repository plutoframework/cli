package migrate

import (
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"
)

const pinnedCandidateSum = "github.com/plutoframework/pluto-core " +
	"v0.4.1-0.20261102070227-3449390f91e7 h1:verified=\n"

func pinnedCandidateSources() project.BoundaryProfile {
	auth := "v0.4.1-0.20261102065147-632011b2bd3c"
	core := "v0.4.1-0.20261102070227-3449390f91e7"
	rbac := "v0.1.1-0.20261102064956-ad4dc810cc64"
	jobs := "v0.1.2-0.20261102064703-75661f11d328"

	return project.BoundaryProfile{
		Pins: map[string]string{
			"github.com/plutoframework/pluto-auth":                  auth,
			"github.com/plutoframework/pluto-auth/methods/password": auth,
			"github.com/plutoframework/pluto-auth/stores/postgres": "v0.5.1-0." +
				"20261102065147-632011b2bd3c",
			"github.com/plutoframework/pluto-rbac": rbac,
			"github.com/plutoframework/pluto-rbac/identity": "v0.0.0-" +
				"20261102064956-ad4dc810cc64",
			"github.com/plutoframework/pluto-core": core,
			"github.com/plutoframework/pluto-core/stores/postgres": "v0.1.1-0." +
				"20261102070227-3449390f91e7",
			"github.com/plutoframework/pluto-jobs": jobs,
			"github.com/plutoframework/pluto-jobs/stores/postgres": "v0.1.1-0." +
				"20261102064703-75661f11d328",
		},
	}
}

// Pinned adoption moves published requirements to the verified pins, adds
// their verified checksums and writes no replacement directive.
func TestPublishedToCandidateWithPins(t *testing.T) {
	root := publishedFixture(t, publishedManifest("multi"))
	sources := pinnedCandidateSources()
	options := Options{Sources: sources, Sums: []byte(pinnedCandidateSum)}

	plan, err := Build(root, ProfileCandidate, options)
	if err != nil {
		t.Fatal(err)
	}

	if plan.Blocked || len(scaffoldTestManual(plan)) != 0 {
		t.Fatalf("pinned migration blocked: %+v", plan.Manual)
	}

	if err := Write(plan); err != nil {
		t.Fatal(err)
	}

	module := readTree(t, root, "go.mod")
	if strings.Contains(module, "replace ") ||
		strings.Contains(module, "v0.0.0-candidate") {
		t.Fatalf("pinned go.mod has local candidate bindings:\n%s", module)
	}

	for path, version := range sources.Pins {
		if strings.Count(module, path+" ") != 1 ||
			!strings.Contains(module, path+" "+version+"\n") {
			t.Fatalf("go.mod does not pin %s %s exactly once:\n%s", path,
				version, module)
		}
	}

	if !strings.Contains(readTree(t, root, "go.sum"), pinnedCandidateSum) {
		t.Fatal("verified pin checksums not added to go.sum")
	}

	_, manifest, err := project.Discover(root)
	if err != nil || len(manifest.Boundary.Pins) != len(sources.Pins) ||
		manifest.Boundary.AuthSource != "" {
		t.Fatalf("migrated manifest pins: %v %+v", err, manifest.Boundary)
	}

	again, err := Build(root, ProfileCandidate, options)
	if err != nil || len(again.Steps) != 0 || len(again.Paths()) != 0 {
		t.Fatalf("pinned migration not idempotent: %v %+v", err, again)
	}
}

func TestPublishedToCandidateRejectsIncompletePins(t *testing.T) {
	sources := pinnedCandidateSources()
	delete(sources.Pins, "github.com/plutoframework/pluto-jobs/stores/postgres")

	assertBlocked(
		t,
		publishedFixture(t, publishedManifest("multi")),
		Options{Sources: sources},
		"pluto.toml:",
		"pins require an exact version",
		"--candidate-<lib>-revision",
	)
}
