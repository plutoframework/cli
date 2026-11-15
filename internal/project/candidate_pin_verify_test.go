package project

import (
	"archive/zip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// fakeCandidateModule is one module version served by the hermetic proxy.
type fakeCandidateModule struct {
	path, version, declared, requires string
	files                             map[string]string
}

func candidateProxyModules() []fakeCandidateModule {
	const base = "github.com/plutoframework/"

	rbac := map[string]string{"rbac.go": "package rbac\n\n" +
		"type Authorizer struct{}\n\nfunc (*Authorizer) Require() {}\n\n" +
		"type Requirement struct{}\n\nfunc (*Requirement) Execute() {}\n\n" +
		"func (*Requirement) WithResourcePolicy() {}\n\n" +
		"type ResourcePolicyFunc func()\n"}

	published := map[string]string{"rbac.go": "package rbac\n\n" +
		"type Authorizer struct{}\n\nfunc (*Authorizer) Require() {}\n\n" +
		"type Requirement struct{}\n\nfunc (*Requirement) Execute() {}\n\n" +
		"type ResourcePolicyFunc func()\n"}

	core := map[string]string{
		"cache/cache.go": "package cache\n\nfunc NewLoader[T any]() {}\n",
		"idempotency/idempotency.go": "package idempotency\n\n" +
			"func NewClaim() {}\n",
		"pagination/pagination.go": "package pagination\n\n" +
			"func Finish[T any]() {}\n",
		"state/state.go": "package state\n\n" +
			"type Machine[S ~string, E ~string, F any] struct{}\n\n" +
			"func New[S ~string, E ~string, F any]() {}\n",
	}

	// Published Core lacks evaluated transitions; candidate Core adds them.
	publishedCore := map[string]string{}
	for name, content := range core {
		publishedCore[name] = content
	}

	core["state/evaluated.go"] = "package state\n\n" +
		"func (*Machine[S, E, F]) Evaluate() {}\n\n" +
		"type Evaluated[S ~string, E ~string] struct{}\n\n" +
		"type Binding[S ~string] struct{}\n"

	return []fakeCandidateModule{
		{path: base + "pluto-auth", version: "v0.9.0", files: map[string]string{
			"auth.go": "package auth\n\ntype Manager struct{}\n\n" +
				"func (*Manager) UseIdentity() {}\n\n" +
				"func (*Manager) WithIdentity() {}\n\ntype Identity struct{}\n",
		}},
		{
			path:    base + "pluto-auth/methods/password",
			version: "v0.9.0",
			files: map[string]string{"password.go": "package password\n\n" +
				"type AttemptLimiter interface{}\n\ntype Sender interface{}\n"},
		},
		{
			path:    base + "pluto-auth/stores/postgres",
			version: "v0.9.0",
			files: map[string]string{
				"local.go": "package postgres\n\nfunc NewLocal() {}\n",
			},
		},
		{path: base + "pluto-rbac", version: "v0.9.0", files: rbac},
		{path: base + "pluto-rbac", version: "v0.8.0", files: published},
		{
			path:    base + "pluto-rbac/identity",
			version: "v0.9.0",
			files: map[string]string{
				"gate.go": "package identitygate\n\n" +
					"func New() {}\n\ntype Gate struct{}\n\n" +
					"func (*Gate) Within() {}\n\nfunc (*Gate) Use() {}\n",
				"identitytest/harness.go": "package identitytest\n\n" +
					"func New() {}\n\ntype Options struct{}\n\n" +
					"type Harness struct{}\n\n" +
					"func (*Harness) Within() {}\n",
			},
		},
		{path: base + "pluto-core", version: "v0.9.0", files: core},
		{path: base + "pluto-core", version: "v0.9.1", files: core},
		{path: base + "pluto-core", version: "v0.8.0", files: publishedCore},
		{
			path:    base + "pluto-core/stores/postgres",
			version: "v0.9.0",
			files: map[string]string{
				"store.go": "package postgres\n\nfunc New() {}\n",
			},
		},
		{
			path:     base + "pluto-core/stores/postgres",
			version:  "v0.7.0",
			declared: "example.test/impostor",
			files: map[string]string{
				"store.go": "package postgres\n\nfunc New() {}\n",
			},
		},
		{path: base + "pluto-jobs", version: "v0.9.0", files: map[string]string{
			"jobs.go": "package jobs\n\ntype Definition[T any] struct{}\n\n" +
				"func NewDefinition[T any]() {}\n\n" +
				"func (*Definition[T]) Bind() {}\n\ntype Delivery struct{}\n",
		}},
		{
			path:     base + "pluto-jobs",
			version:  "v0.9.2",
			requires: base + "pluto-core v0.9.1",
			files: map[string]string{
				"jobs.go": "package jobs\n\ntype Definition[T any] struct{}\n\n" +
					"func NewDefinition[T any]() {}\n\n" +
					"func (*Definition[T]) Bind() {}\n\ntype Delivery struct{}\n",
			},
		},
		{
			path:    base + "pluto-jobs/stores/postgres",
			version: "v0.9.0",
			files: map[string]string{
				"store.go": "package postgres\n\nfunc New() {}\n",
			},
		},
	}
}

// writeCandidateProxy serves the fake modules from a file:// GOPROXY and
// isolates the module cache, so verification runs without network access.
func writeCandidateProxy(t *testing.T) {
	t.Helper()

	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go command unavailable")
	}

	proxy := t.TempDir()
	versions := map[string][]string{}

	for _, module := range candidateProxyModules() {
		declared := module.declared
		if declared == "" {
			declared = module.path
		}

		mod := "module " + declared + "\n\ngo 1.25.5\n"
		if module.requires != "" {
			mod += "\nrequire " + module.requires + "\n"
		}

		directory := filepath.Join(proxy, filepath.FromSlash(module.path), "@v")
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}

		prefix := filepath.Join(directory, module.version)
		for name, content := range map[string]string{
			".info": `{"Version":"` + module.version +
				`","Time":"2026-11-01T00:00:00Z"}`,
			".mod": mod,
		} {
			if err := os.WriteFile(
				prefix+name,
				[]byte(content),
				0o644,
			); err != nil {
				t.Fatal(err)
			}
		}

		archive, err := os.Create(prefix + ".zip")
		if err != nil {
			t.Fatal(err)
		}

		writer := zip.NewWriter(archive)
		files := map[string]string{"go.mod": mod}

		for name, content := range module.files {
			files[name] = content
		}

		for name, content := range files {
			entry, err := writer.Create(
				module.path + "@" + module.version + "/" + name,
			)
			if err != nil {
				t.Fatal(err)
			}

			if _, err := entry.Write([]byte(content)); err != nil {
				t.Fatal(err)
			}
		}

		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}

		if err := archive.Close(); err != nil {
			t.Fatal(err)
		}

		versions[directory] = append(versions[directory], module.version)
	}

	for directory, list := range versions {
		if err := os.WriteFile(
			filepath.Join(directory, "list"),
			[]byte(strings.Join(list, "\n")+"\n"),
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	cache := t.TempDir()

	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOSUMDB", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOTOOLCHAIN", "local")
	t.Setenv("GOMODCACHE", cache)
	t.Cleanup(func() {
		command := exec.Command("go", "clean", "-modcache")

		command.Env = append(os.Environ(), "GOMODCACHE="+cache)
		_ = command.Run()
	})
}

func proxyPinnedProfile() BoundaryProfile {
	profile := pinnedProfileFixture()
	for module := range profile.Pins {
		profile.Pins[module] = "v0.9.0"
	}

	return profile
}

func TestVerifyCandidatePinsAgainstModuleProxy(t *testing.T) {
	writeCandidateProxy(t)

	ctx := context.Background()
	profile := proxyPinnedProfile()

	sums, err := VerifyCandidatePins(ctx, profile)
	if err != nil {
		t.Fatal(err)
	}

	for module := range profile.Pins {
		for _, suffix := range []string{" v0.9.0 h1:", " v0.9.0/go.mod h1:"} {
			if !strings.Contains(string(sums), module+suffix) {
				t.Fatalf(
					"missing verified checksum %s%s:\n%s",
					module,
					suffix,
					sums,
				)
			}
		}
	}

	if strings.Contains(string(sums), "v0.9.1") {
		t.Fatal("checksums of unpinned versions returned")
	}

	for name, test := range map[string]struct {
		module, version, want string
	}{
		"published api only": {
			"github.com/plutoframework/pluto-rbac",
			"v0.8.0",
			"candidate APIs",
		},
		"published core state": {
			"github.com/plutoframework/pluto-core",
			"v0.8.0",
			"Evaluate",
		},
		"impostor module path": {
			"github.com/plutoframework/pluto-core/stores/postgres",
			"v0.7.0",
			"example.test/impostor",
		},
		"inconsistent pins": {
			"github.com/plutoframework/pluto-jobs",
			"v0.9.2",
			"inconsistent",
		},
		"missing version": {
			"github.com/plutoframework/pluto-jobs",
			"v0.9.9",
			"download candidate",
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidate := clonePins(profile)
			candidate.Pins[test.module] = test.version

			_, err := VerifyCandidatePins(ctx, candidate)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want error containing %q", err, test.want)
			}
		})
	}
}

func TestResolveCandidatePinsRejectsMutableRevisions(t *testing.T) {
	ctx := context.Background()

	for _, revision := range []string{
		"main",
		"632011b2bd3c",
		"v0.4.0",
		strings.ToUpper(strings.Repeat("a", 40)),
	} {
		profile := BoundaryProfile{Mode: "candidate", AuthRevision: revision}
		if _, err := ResolveCandidatePins(ctx, profile); err == nil ||
			!strings.Contains(err.Error(), "full 40-character") {
			t.Fatalf("revision %q accepted: %v", revision, err)
		}
	}

	published := BoundaryProfile{AuthRevision: strings.Repeat("a", 40)}
	if _, err := ResolveCandidatePins(ctx, published); err == nil {
		t.Fatal("published profile resolved a candidate revision")
	}

	source := boundaryProfileFixture(t)
	source.AuthRevision = strings.Repeat("a", 40)

	if _, err := ResolveCandidatePins(ctx, source); err == nil {
		t.Fatal("library resolved with both a source and a revision")
	}
}
