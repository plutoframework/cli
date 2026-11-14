package project

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	pinAuth     = "v0.4.1-0.20261102065147-632011b2bd3c"
	pinAuthPG   = "v0.5.1-0.20261102065147-632011b2bd3c"
	pinRBAC     = "v0.1.1-0.20261102064956-ad4dc810cc64"
	pinIdentity = "v0.0.0-20261102064956-ad4dc810cc64"
	pinCore     = "v0.4.1-0.20261102070227-3449390f91e7"
	pinCorePG   = "v0.1.1-0.20261102070227-3449390f91e7"
	pinJobs     = "v0.1.2-0.20261102064703-75661f11d328"
	pinJobsPG   = "v0.1.1-0.20261102064703-75661f11d328"
)

func pinnedProfileFixture() BoundaryProfile {
	return BoundaryProfile{
		Mode: "candidate",
		Pins: map[string]string{
			"github.com/plutoframework/pluto-auth":                  pinAuth,
			"github.com/plutoframework/pluto-auth/stores/postgres":  pinAuthPG,
			"github.com/plutoframework/pluto-auth/methods/password": pinAuth,
			"github.com/plutoframework/pluto-rbac":                  pinRBAC,
			boundaryIdentityModule:                                  pinIdentity,
			"github.com/plutoframework/pluto-core":                  pinCore,
			"github.com/plutoframework/pluto-core/stores/postgres":  pinCorePG,
			"github.com/plutoframework/pluto-jobs":                  pinJobs,
			"github.com/plutoframework/pluto-jobs/stores/postgres":  pinJobsPG,
		},
	}
}

func clonePins(profile BoundaryProfile) BoundaryProfile {
	pins := map[string]string{}
	for module, version := range profile.Pins {
		pins[module] = version
	}

	profile.Pins = pins

	return profile
}

func TestCandidatePinsValidateOffline(t *testing.T) {
	profile := pinnedProfileFixture()
	if err := ValidateBoundaryProfile(profile, "multi"); err != nil {
		t.Fatal(err)
	}

	replacements, err := CandidateReplacements(profile)
	if err != nil || len(replacements) != 0 {
		t.Fatalf("pinned libraries have replacements: %v %v", replacements, err)
	}

	mixed := clonePins(profile)
	source := boundaryProfileFixture(t)
	mixed.AuthSource = source.AuthSource

	for _, suffix := range []string{"", "/stores/postgres", "/methods/password"} {
		delete(mixed.Pins, "github.com/plutoframework/pluto-auth"+suffix)
	}

	if err := ValidateBoundaryProfile(mixed, "multi"); err != nil {
		t.Fatal("mixed source and pinned libraries rejected:", err)
	}

	for name, mutate := range map[string]func(*BoundaryProfile){
		"missing module": func(p *BoundaryProfile) {
			delete(p.Pins, "github.com/plutoframework/pluto-core/stores/postgres")
		},
		"branch name": func(p *BoundaryProfile) {
			p.Pins["github.com/plutoframework/pluto-core"] = "main"
		},
		"non canonical": func(p *BoundaryProfile) {
			p.Pins["github.com/plutoframework/pluto-core"] = "v0.4"
		},
		"major version path": func(p *BoundaryProfile) {
			p.Pins["github.com/plutoframework/pluto-core"] = "v2.0.0"
		},
		"build metadata": func(p *BoundaryProfile) {
			p.Pins["github.com/plutoframework/pluto-core"] = "v0.4.0+dirty"
		},
		"mixed commits": func(p *BoundaryProfile) {
			p.Pins["github.com/plutoframework/pluto-jobs/stores/postgres"] =
				"v0.1.1-0.20261102064703-000000000000"
		},
		"unknown module": func(p *BoundaryProfile) {
			p.Pins["github.com/plutoframework/pluto-agent"] = "v0.1.3"
		},
		"source and pins": func(p *BoundaryProfile) {
			p.CoreSource = boundaryProfileFixture(t).CoreSource
		},
		"unresolved revision": func(p *BoundaryProfile) {
			p.JobsRevision = strings.Repeat("a", 40)
		},
		"published with pins": func(p *BoundaryProfile) {
			p.Mode = "published"
		},
		"neither source nor pins": func(p *BoundaryProfile) {
			for _, suffix := range []string{"", "/stores/postgres"} {
				delete(p.Pins, "github.com/plutoframework/pluto-jobs"+suffix)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := clonePins(profile)
			mutate(&candidate)

			if err := ValidateBoundaryProfile(candidate, "multi"); err == nil {
				t.Fatal("invalid pins accepted")
			}
		})
	}
}

// Adoption moves published requirements to the verified pins without any
// replacement; the strict configuration then holds the result unchanged.
func TestCandidatePinsAdoptPublishedModule(t *testing.T) {
	profile := pinnedProfileFixture()
	published := ToolFiles(Manifest{
		Project:      Project{Module: "example.test/pinned"},
		Capabilities: Capabilities{Tenancy: "multi", LocalCredentials: true},
	})["go.mod"]

	if _, err := ConfigureBoundaryModules(
		profile,
		false,
		published,
	); err == nil || !strings.Contains(err.Error(), "adopt pins explicitly") {
		t.Fatal("published requirement silently rewritten:", err)
	}

	adopted, err := AdoptBoundaryModules(profile, false, published)
	if err != nil {
		t.Fatal(err)
	}

	requires, replacements, err := boundaryModuleInventory(adopted)
	if err != nil {
		t.Fatal(err)
	}

	if len(replacements) != 0 {
		t.Fatalf("pinned adoption wrote replacements: %v", replacements)
	}

	for module, version := range profile.Pins {
		if requires[module] != version {
			t.Fatalf("%s requires %q, want %s", module, requires[module], version)
		}
	}

	if requires["github.com/jackc/pgx/v5"] == "" ||
		!bytes.Contains(adopted, []byte("golang.org/x/crypto v0.55.0 // indirect")) {
		t.Fatal("unrelated requirements were not preserved")
	}

	again, err := ConfigureBoundaryModules(profile, false, adopted)
	if err != nil || !bytes.Equal(again, adopted) {
		t.Fatalf("adopted pins drift under strict configuration: %v", err)
	}

	readopted, err := AdoptBoundaryModules(profile, false, adopted)
	if err != nil || !bytes.Equal(readopted, adopted) {
		t.Fatalf("adoption is not idempotent: %v", err)
	}
}

func TestCandidatePinsRejectReplacementsAndDrift(t *testing.T) {
	profile := pinnedProfileFixture()

	base, err := AdoptBoundaryModules(
		profile,
		false,
		[]byte("module example.test/pinned\n\ngo 1.25.5\n"),
	)
	if err != nil {
		t.Fatal(err)
	}

	for name, suffix := range map[string]string{
		"local replacement": "\nreplace github.com/plutoframework/pluto-core " +
			"=> /src/pluto-core\n",
		"version replacement": "\nreplace github.com/plutoframework/pluto-rbac " +
			"=> example.test/fork v1.0.0\n",
		"identity replacement": "\nreplace " + boundaryIdentityModule +
			" => ../identity\n",
	} {
		t.Run(name, func(t *testing.T) {
			current := append(append([]byte(nil), base...), suffix...)
			if _, err := ConfigureBoundaryModules(
				profile,
				false,
				current,
			); err == nil {
				t.Fatal("pinned module replacement accepted")
			}

			if _, err := AdoptBoundaryModules(
				profile,
				false,
				current,
			); err == nil {
				t.Fatal("adoption retained a pinned module replacement")
			}
		})
	}

	drifted := strings.Replace(string(base), pinCorePG, "v0.1.0", 1)
	if _, err := ConfigureBoundaryModules(
		profile,
		false,
		[]byte(drifted),
	); err == nil {
		t.Fatal("drifted pin accepted")
	}

	m := Manifest{Boundary: profile}

	metadata, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}

	root := t.TempDir()
	for source, want := range map[string]int{string(base): 0, drifted: 1} {
		if err := os.WriteFile(
			filepath.Join(root, "go.mod"),
			[]byte(source),
			0o644,
		); err != nil {
			t.Fatal(err)
		}

		if got := inspectCandidateModules(root, metadata); len(got) != want {
			t.Fatalf("pinned module diagnostics: %v, want %d", got, want)
		}
	}

	var decoded Manifest
	if err := strictDecode(metadata, &decoded); err != nil ||
		len(decoded.Boundary.Pins) != len(profile.Pins) ||
		!bytes.Contains(metadata, []byte("[boundary.pins]")) {
		t.Fatalf("pins do not round-trip through pluto.toml: %v\n%s", err, metadata)
	}
}

func TestMergeSumsAppendsOnlyMissingLines(t *testing.T) {
	merged := MergeSums(
		[]byte("a v1 h1:x=\nb v1 h1:y="),
		[]byte("b v1 h1:y=\nc v1 h1:z=\n\n"),
	)
	if string(merged) != "a v1 h1:x=\nb v1 h1:y=\nc v1 h1:z=\n" {
		t.Fatalf("unexpected merged sums: %q", merged)
	}
}

// go mod tidy prunes pinned modules no package imports; vet accepts that, but
// an imported pinned module must be required at exactly its pin.
func TestCandidatePinsAllowOnlyUnimportedPruning(t *testing.T) {
	profile := pinnedProfileFixture()

	metadata, err := Encode(Manifest{Boundary: profile})
	if err != nil {
		t.Fatal(err)
	}

	full, err := AdoptBoundaryModules(
		profile,
		false,
		[]byte("module example.test/pinned\n\ngo 1.25.5\n"),
	)
	if err != nil {
		t.Fatal(err)
	}

	const store = "github.com/plutoframework/pluto-core/stores/postgres"

	pruned := strings.Replace(string(full), "\t"+store+" "+pinCorePG+"\n", "", 1)
	if pruned == string(full) {
		t.Fatal("fixture did not prune the store requirement")
	}

	root := t.TempDir()
	write := func(name, source string) {
		t.Helper()

		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("go.mod", pruned)
	write("internal/app/app.go", "package app\n\nimport _ \""+
		"github.com/plutoframework/pluto-core/state\"\n")

	if got := inspectCandidateModules(root, metadata); len(got) != 0 {
		t.Fatalf("pruned unimported pin rejected: %v", got)
	}

	for name, module := range map[string]string{
		"imported but missing": pruned,
		"imported at another version": strings.Replace(
			string(full),
			store+" "+pinCorePG,
			store+" v0.1.0",
			1,
		),
	} {
		t.Run(name, func(t *testing.T) {
			write("go.mod", module)
			write("internal/storage/store.go", "package storage\n\nimport _ \""+
				store+"\"\n")

			if got := inspectCandidateModules(root, metadata); len(got) != 1 {
				t.Fatalf("imported pin drift accepted: %v", got)
			}
		})
	}

	write("go.mod", strings.Replace(
		string(full),
		"\t"+boundaryIdentityModule+" "+pinIdentity+"\n",
		"",
		1,
	))

	if got := inspectCandidateModules(root, metadata); len(got) != 1 {
		t.Fatalf("missing identity pin accepted: %v", got)
	}
}
