package project

import (
	"strings"
	"testing"
)

func deviceFixture(t *testing.T) Manifest {
	t.Helper()

	m := generatableSeamFixture(t)
	m.Boundary.Mode = "candidate"
	m.Capabilities.Tenancy = "multi"
	m.Capabilities.LocalCredentials = true
	m.Auth = &AuthSection{Device: &AuthDevice{
		VerificationURI: "https://app.example.test/device",
		Scopes: []DeviceScope{{
			Name: "audit.read", Permissions: []string{"audit.read"},
		}},
		Clients: []DeviceClient{{
			ID: "cli", Name: "CLI", ActorType: "agent",
			Scopes: []string{"audit.read"},
		}},
	}}

	return m
}

func TestAuthDeviceValidation(t *testing.T) {
	if err := validateAuthDevice(deviceFixture(t)); err != nil {
		t.Fatal(err)
	}

	cases := map[string]func(*Manifest){
		"published profile": func(m *Manifest) {
			m.Boundary.Mode = ""
		},
		"single tenant": func(m *Manifest) {
			m.Capabilities.Tenancy = "single"
		},
		"no credentials": func(m *Manifest) {
			m.Capabilities.LocalCredentials = false
		},
		"plain http uri": func(m *Manifest) {
			m.Auth.Device.VerificationURI = "http://app.example.test/device"
		},
		"uri with query": func(m *Manifest) {
			m.Auth.Device.VerificationURI = "https://a.example.test/d?x=1"
		},
		"undeclared permission": func(m *Manifest) {
			m.Auth.Device.Scopes[0].Permissions = []string{"nope.write"}
		},
		"bad scope name": func(m *Manifest) {
			m.Auth.Device.Scopes[0].Name = "Audit Read"
		},
		"client with undeclared scope": func(m *Manifest) {
			m.Auth.Device.Clients[0].Scopes = []string{"admin"}
		},
		"client without scopes": func(m *Manifest) {
			m.Auth.Device.Clients[0].Scopes = nil
		},
		"user actor type": func(m *Manifest) {
			m.Auth.Device.Clients[0].ActorType = "user"
		},
		"duplicate client": func(m *Manifest) {
			m.Auth.Device.Clients = append(
				m.Auth.Device.Clients, m.Auth.Device.Clients[0])
		},
		"refresh outlives the grant": func(m *Manifest) {
			m.Auth.Device.RefreshTTL = "100h"
			m.Auth.Device.MaxLifetime = "50h"
		},
		"unbounded refresh": func(m *Manifest) {
			m.Auth.Device.RefreshTTL = "9000h"
		},
		"bad duration": func(m *Manifest) {
			m.Auth.Device.CodeTTL = "soon"
		},
	}

	for name, mutate := range cases {
		m := deviceFixture(t)
		mutate(&m)

		if err := validateAuthDevice(m); err == nil {
			t.Fatalf("%s was accepted", name)
		} else if !strings.Contains(err.Error(), "auth.device") {
			t.Fatalf("%s: unexpected error %v", name, err)
		}
	}
}

func TestCandidateDeviceGeneration(t *testing.T) {
	m := systemFixture(t)
	m.Boundary.Mode = "candidate"
	m.Auth = deviceFixture(t).Auth

	if err := m.Validate(); err != nil {
		t.Fatal(err)
	}

	files, err := candidateManaged(m)
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{
		"internal/app/candidate_device_gen.go",
		"internal/access/delegation_gen.go",
		"internal/paths/device.go",
		"db/migrations/090_device_grants.sql",
	} {
		if len(files[path]) == 0 {
			t.Fatalf("missing %s", path)
		}
	}

	checks := map[string]string{
		"internal/access/delegation_gen.go":         `"audit.read": {`,
		"internal/access/caller_gen.go":             "func (c Caller) Permits(",
		"internal/app/candidate_device_gen.go":      "service.RevokeSubject(ctx, account.ID)",
		"internal/app/candidate_credentials_gen.go": "newCandidateDevice(",
	}

	for path, want := range checks {
		source, ok := files[path]
		if !ok {

			// The caller lives under another generated name.
			continue
		}

		if !strings.Contains(string(source), want) {
			t.Fatalf("%s lacks %q", path, want)
		}
	}

	var gates, provisions int

	for path, source := range files {
		if !strings.HasSuffix(path, "_gate_gen.go") {
			continue
		}

		text := string(source)

		switch {
		case strings.Contains(text, "Gate.Within("):
			gates++

			if !strings.Contains(text, "caller.Permits(") {
				t.Fatalf("%s skips the delegation scope check", path)
			}
		case strings.Contains(text, "UseIdentity("):
			provisions++

			if !strings.Contains(text, "identity.Delegation()") {
				t.Fatalf("%s lets a delegate provision a tenant", path)
			}
		}
	}

	if gates == 0 || provisions == 0 {
		t.Fatalf("expected gated (%d) and provisioning (%d) operations",
			gates, provisions)
	}

	// Without [auth.device] nothing delegation-related is generated.
	plain := systemFixture(t)
	plain.Boundary.Mode = "candidate"

	files, err = candidateManaged(plain)
	if err != nil {
		t.Fatal(err)
	}

	for path, source := range files {
		if strings.Contains(string(source), "Permits(") ||
			strings.Contains(string(source), "Delegation") ||
			strings.Contains(path, "delegation") ||
			strings.Contains(path, "device") {
			t.Fatalf("device output leaked into %s", path)
		}
	}
}

func TestDeviceGrantIsCriticalForAuthoredCode(t *testing.T) {
	if !candidateCriticalPackage(
		"github.com/plutoframework/pluto-auth/device") ||
		!candidateCriticalPackage(
			"github.com/plutoframework/pluto-auth/device/devicehttp") {
		t.Fatal("authored code may import the device grant")
	}
}
