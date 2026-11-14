package project

import (
	"strings"
	"testing"
)

func standaloneManifest() Manifest {
	m := NewStandaloneFrontend("orbital", FrontendAPI{
		OpenAPI:          "../orbital/openapi/openapi.json",
		EnvPrefix:        "ORBITAL_",
		LocalCredentials: true,
	})
	m.Frontend = &Frontend{
		Framework: "react", Directory: ".", UI: "native", Version: 1,
		API: &FrontendAPI{
			OpenAPI:          "../orbital/openapi/openapi.json",
			EnvPrefix:        "ORBITAL_",
			LocalCredentials: true,
		},
	}

	return m
}

func TestStandaloneFrontendRoundTrip(t *testing.T) {
	data, err := Encode(standaloneManifest())
	if err != nil {
		t.Fatal(err)
	}

	text := string(data)
	for _, unwanted := range []string{"[database]", "[boundary]", "module"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("frontend manifest leaks %s:\n%s", unwanted, text)
		}
	}

	m, err := Decode(data)
	if err != nil {
		t.Fatal(err, text)
	}

	if !m.Standalone() || m.Project.EnvPrefix != "ORBITAL_" ||
		!m.Capabilities.LocalCredentials {
		t.Fatalf("round trip lost settings: %+v", m)
	}
}

func TestStandaloneFrontendValidation(t *testing.T) {
	for name, mutate := range map[string]func(*Manifest){
		"subdirectory": func(m *Manifest) {
			m.Frontend.Directory = "web"
		},
		"absolute openapi": func(m *Manifest) {
			m.Frontend.API.OpenAPI = "/srv/openapi.json"
		},
		"plain http openapi": func(m *Manifest) {
			m.Frontend.API.OpenAPI = "http://api.example.com/openapi.json"
		},
		"unclean openapi": func(m *Manifest) {
			m.Frontend.API.OpenAPI = "../a/../b/openapi.json"
		},
		"remote dev target": func(m *Manifest) {
			m.Frontend.API.DevTarget = "http://10.0.0.1:8080"
		},
		"oidc without credentials": func(m *Manifest) {
			m.Frontend.API.LocalCredentials = false
			m.Frontend.API.OIDC = true
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := standaloneManifest()
			mutate(&m)

			if err := m.Validate(); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}

	for _, source := range []string{
		"https://api.example.com/openapi.json",
		"http://127.0.0.1:8080/openapi.json",
		"vendor/openapi.json",
	} {
		if err := ValidateOpenAPISource(source); err != nil {
			t.Fatalf("%s: %v", source, err)
		}
	}
}

func TestEmbeddedFrontendRejectsAPI(t *testing.T) {
	m := Manifest{Frontend: &Frontend{
		Framework: "react", Directory: "web", UI: "native", Version: 1,
		API: &FrontendAPI{},
	}}

	err := m.ValidateFrontend()
	if err == nil || !strings.Contains(err.Error(), "frontend-only") {
		t.Fatalf("expected refusal, got %v", err)
	}
}
