package project

import (
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/readability"
)

// Audit canonical emitted Go before external formatter exclusions can hide it.
func TestGeneratedGoReadability(t *testing.T) {
	base := Manifest{
		SchemaVersion: SchemaVersion,
		Project: Project{
			Name:      "readable",
			Module:    "example.test/readable",
			EnvPrefix: "READABLE_",
		},
		Capabilities: Capabilities{
			Tenancy: "multi",
			Cache:   "memory",
		},
		Database: Database{
			Engine:  "postgres",
			Queries: "sqlc",
		},
	}

	base.Operations = []Operation{
		{
			Group:       "records",
			Action:      "Create",
			Permission:  "records.create",
			Write:       true,
			Idempotency: "required",
		},
		{
			Group:       "records",
			Action:      "List",
			Permission:  "records.list",
			Collection:  true,
			Idempotency: "none",
		},
		{
			Group:       "records",
			Action:      "Load",
			Permission:  "records.load",
			Idempotency: "none",
		},
	}
	for _, operation := range base.Operations {
		entry := operation.Endpoint()
		entry.Operation = operation.ID()
		entry.Name = operation.Action + " record"
		entry.Description = "Declared readable operation."
		entry.Path = "/records/" + strings.ToLower(operation.Action)
		base.Endpoints = append(base.Endpoints, entry)
	}

	base.Jobs = []Job{
		{
			Group:     "deliveries",
			Action:    "Create",
			Kind:      "records.create",
			Operation: "records.Create",
		},
	}
	base.Agents = []Agent{
		{
			Group:      "reviews",
			Action:     "Load",
			Operation:  "records.Load",
			Permission: "records.load",
			Provider:   "scripted",
		},
	}

	for _, profile := range []string{
		"single",
		"multi",
		"credentials",
		"oidc",
		"valkey",
		"candidate",
	} {
		t.Run(profile, func(t *testing.T) {
			manifest := base

			switch profile {
			case "single":
				manifest.Capabilities.Tenancy = "single"
			case "valkey":
				manifest.Capabilities.Cache = "valkey"
			case "credentials":
				manifest.Capabilities.LocalCredentials = true
			case "oidc":
				manifest.Capabilities.LocalCredentials = true
				manifest.Capabilities.OIDC = true
			case "candidate":
				manifest = manifestContractFixture(t)
				manifest.SchemaVersion = base.SchemaVersion
				manifest.Project = base.Project
				manifest.Database = base.Database
			}

			files, err := InitialFiles(manifest)
			if err != nil {
				t.Fatal(err)
			}

			for path, source := range files {
				if !strings.HasSuffix(path, ".go") {
					continue
				}

				for _, problem := range readability.Check(path, source) {
					t.Error(problem)
				}
			}
		})
	}
}
