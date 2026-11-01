package project

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const boundaryBaseModule = "module example.test/app\n\ngo 1.25.5\n\nrequire (\n github.com/plutoframework/pluto-auth v0.1.0 // retain pin\n example.test/other v1.2.3\n)\n\nreplace example.test/other => ../other\n"

func TestBoundaryModulesPreservePinsAndAreIdempotent(t *testing.T) {
	profile := boundaryProfileFixture(t)

	output, err := ConfigureBoundaryModules(
		profile,
		true,
		[]byte(boundaryBaseModule),
	)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.HasPrefix(output, []byte(boundaryBaseModule)) {
		t.Fatal("existing requirements or authored comments rewritten")
	}

	requires, replacements, err := boundaryModuleInventory(output)
	if err != nil {
		t.Fatal(err)
	}

	if requires["github.com/plutoframework/pluto-auth"] != "v0.1.0" ||
		requires["example.test/other"] != "v1.2.3" ||
		requires["github.com/plutoframework/pluto-agent"] != AgentVersion ||
		requires[boundaryIdentityModule] != boundaryIdentityVersion ||
		len(replacements) != 10 {
		t.Fatalf("unexpected dependency graph: %v %v", requires, replacements)
	}

	again, err := ConfigureBoundaryModules(profile, true, output)
	if err != nil || !bytes.Equal(output, again) {
		t.Fatalf("repeated configuration drifted: %v", err)
	}

	paths, err := CandidateReplacements(profile)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(output),
		strconv.Quote(paths["github.com/plutoframework/pluto-auth"]),
	) {
		t.Fatal("candidate source was not quoted")
	}
}

func TestBoundaryModulesPublishedCannotRetainCandidates(t *testing.T) {
	for _, suffix := range []string{
		"require " + boundaryIdentityModule + " " + boundaryIdentityVersion + "\n",
		"replace github.com/plutoframework/pluto-auth => ../pluto-auth\n",
		"replace(\ngithub.com/plutoframework/pluto-core=>\"/tmp/source path\"\n)\n",
		"replace " + boundaryIdentityModule + " => example.test/identity v1.0.0\n",
	} {
		if _, err := ConfigureBoundaryModules(
			BoundaryProfile{},
			false,
			[]byte(boundaryBaseModule+suffix),
		); err == nil {
			t.Fatalf(
				"candidate dependency accepted in published profile: %s",
				suffix,
			)
		}
	}

	output, err := ConfigureBoundaryModules(
		BoundaryProfile{},
		false,
		[]byte(boundaryBaseModule),
	)
	if err != nil || string(output) != boundaryBaseModule {
		t.Fatalf("published module changed: %v", err)
	}
}

func TestBoundaryModulesRejectConflictsWithoutRewriting(t *testing.T) {
	for _, test := range []struct{ name, suffix, want string }{
		{"agent pin", "require github.com/plutoframework/pluto-agent v9.0.0\n", "explicit upgrade"},
		{"identity pin", "require " + boundaryIdentityModule + " v1.0.0\n", "explicit candidate"},
		{"duplicate identity", "require " + boundaryIdentityModule + " " + boundaryIdentityVersion + "\nrequire " + boundaryIdentityModule + " " + boundaryIdentityVersion + "\n", "duplicate"},
		{"different source", "replace github.com/plutoframework/pluto-core => /not/the/source\n", "conflicting"},
		{"relative source", "replace github.com/plutoframework/pluto-core => ../pluto-core\n", "conflicting"},
		{"versioned source", "replace github.com/plutoframework/pluto-core v0.1.0 => /not/the/source\n", "conflicting"},
		{"remote replacement", "replace github.com/plutoframework/pluto-core => example.test/core v1.0.0\n", "conflicting"},
		{"undeclared source", "replace github.com/plutoframework/pluto-auth/methods/oidc => /tmp/oidc\n", "no declared source"},
		{"inline block", "replace (github.com/plutoframework/pluto-core => /tmp/core)\n", "inline"},
		{"unfinished block", "replace (\n", "unterminated"},
		{"hidden spacing", "require github.com/plutoframework/ /*gap*/ pluto-agent v1.0.0\n", "require syntax"},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := []byte(boundaryBaseModule + test.suffix)

			before := append([]byte(nil), input...)
			if output, err := ConfigureBoundaryModules(
				boundaryProfileFixture(t),
				true,
				input,
			); err == nil || !strings.Contains(err.Error(), test.want) ||
				output != nil {
				t.Fatalf("expected %s rejection: %v", test.want, err)
			}

			if !bytes.Equal(before, input) {
				t.Fatal("rejected configuration mutated input")
			}
		})
	}
}

func TestBoundaryModulesRetainMatchingSourcesAndQuotedPaths(t *testing.T) {
	profile := boundaryProfileFixture(t)

	paths, err := CandidateReplacements(profile)
	if err != nil {
		t.Fatal(err)
	}

	corePath := paths["github.com/plutoframework/pluto-core"]
	input := boundaryBaseModule + fmt.Sprintf(
		"replace (\n github.com/plutoframework/pluto-core => %s // explicit local source\n)\n",
		strconv.Quote(corePath),
	)

	output, err := ConfigureBoundaryModules(profile, false, []byte(input))
	if err != nil {
		t.Fatal(err)
	}

	if bytes.Contains(
		output,
		[]byte("require github.com/plutoframework/pluto-agent"),
	) {
		t.Fatal("agent dependency added without declared agents")
	}

	if strings.Count(
		string(output),
		"github.com/plutoframework/pluto-core =>",
	) != 1 {
		t.Fatal("matching replacement duplicated")
	}

	duplicate := string(
		output,
	) + fmt.Sprintf(
		"replace github.com/plutoframework/pluto-core => %s\n",
		strconv.Quote(corePath),
	)
	if _, err := ConfigureBoundaryModules(
		profile,
		false,
		[]byte(duplicate),
	); err == nil ||
		!strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("duplicate replacement accepted: %v", err)
	}

	quoted := filepath.Join(t.TempDir(), "space and (parentheses) => quotes")

	_, replacements, err := boundaryModuleInventory(
		[]byte(
			"module example.test/app\nreplace example.test/other => " + strconv.Quote(
				quoted,
			) + "\n",
		),
	)
	if err != nil || len(replacements) != 1 ||
		replacements[0].target != quoted {
		t.Fatalf("quoted path damaged: %v %v", replacements, err)
	}
}
