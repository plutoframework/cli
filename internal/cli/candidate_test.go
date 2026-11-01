package cli

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/plutoframework/cli/internal/project"

	"github.com/pelletier/go-toml/v2"
)

func candidateBlueprint() operationBlueprint {
	resource := project.ResourceContract{
		Group:        "documents",
		Name:         "Record",
		Table:        "records",
		Namespace:    "document-records",
		Version:      1,
		Visibility:   "tenant",
		InitialState: "draft",
		States:       []string{"draft", "approved"},
		Transitions: []project.ResourceTransition{
			{
				From:  "draft",
				Event: "submit",
				To:    "approved",
				Guard: "not_owner",
			},
		},
		Normalize:       "trim",
		MaxValueBytes:   8192,
		PageLimit:       2,
		ExportLimit:     4,
		CacheTTLSeconds: 30,
	}
	blueprint := operationBlueprint{
		Resources: []project.ResourceContract{
			resource,
			{
				Group:           "archive",
				Name:            "Note",
				Table:           "notes",
				Namespace:       "archive-notes",
				Version:         1,
				Visibility:      "owner",
				InitialState:    "draft",
				States:          []string{"draft"},
				Normalize:       "none",
				MaxValueBytes:   128,
				PageLimit:       1,
				ExportLimit:     2,
				CacheTTLSeconds: 0,
			},
		},
	}

	for _, entry := range []struct{ action, kind, permission, policy, event, idem string }{{"Create", "create", "documents.create", "create", "", "required"}, {"Load", "load", "documents.load", "tenant", "", "none"}, {"List", "list", "documents.list", "tenant", "", "none"}, {"Export", "export", "documents.export", "tenant", "", "none"}, {"Approve", "transition", "documents.approve", "tenant", "submit", "required"}, {"Deliver", "effect", "documents.deliver", "tenant", "", "required"}} {
		id := "documents." + entry.action
		blueprint.Operations = append(
			blueprint.Operations,
			project.Operation{
				Group:      "documents",
				Action:     entry.action,
				Permission: entry.permission,
				Write: entry.kind == "create" || entry.kind == "transition" ||
					entry.kind == "effect",
				Collection:  entry.kind == "list",
				Idempotency: entry.idem,
			},
		)

		contract := project.OperationContract{
			ID:             id,
			Resource:       resource.ID(),
			Kind:           entry.kind,
			Permission:     entry.permission,
			ResourcePolicy: entry.policy,
			Event:          entry.event,
			Idempotency:    entry.idem,
		}
		if entry.action == "Approve" {
			contract.Effects = []string{"approved-notice"}
		}

		blueprint.OperationContracts = append(
			blueprint.OperationContracts,
			contract,
		)
	}

	blueprint.Effects = []project.EffectContract{
		{
			Name:        "approved-notice",
			Operation:   "documents.Deliver",
			Kind:        "document.approved",
			MaxAttempts: 3,
		},
	}
	blueprint.Jobs = []project.Job{
		{
			Group:       "deliveries",
			Action:      "Approve",
			Operation:   "documents.Approve",
			Kind:        "document.approve",
			MaxAttempts: 5,
		},
		{
			Group:       "deliveries",
			Action:      "Deliver",
			Operation:   "documents.Deliver",
			Kind:        "document.approved",
			MaxAttempts: 3,
		},
	}

	blueprint.Agents = []project.Agent{
		{
			Group:      "approvals",
			Action:     "Approve",
			Operation:  "documents.Approve",
			Permission: "documents.approve",
			Provider:   "scripted",
			Approval:   "host",
		},
	}
	for _, entry := range []struct{ action, method, path, source string }{{"Create", "POST", "/records", "body"}, {"Approve", "POST", "/records/approve", "body"}, {"Load", "GET", "/records/{id}", "path:id"}, {"List", "GET", "/records", "collection"}, {"Export", "GET", "/records/export", "collection"}} {
		blueprint.Endpoints = append(
			blueprint.Endpoints,
			project.Endpoint{
				Group:          "documents",
				Action:         entry.action,
				Operation:      "documents." + entry.action,
				Name:           entry.action + " record",
				Description:    "Declared bounded record operation.",
				Method:         entry.method,
				Path:           entry.path,
				ResourceSource: entry.source,
			},
		)
	}

	permissions := []string{}
	for _, operation := range blueprint.Operations {
		permissions = append(permissions, operation.Permission)
	}

	blueprint.Roles = []project.RoleContract{
		{
			ID:          "operator",
			Name:        "Operator",
			Description: "Explicit record capability grants.",
			Permissions: permissions,
		},
	}

	return blueprint
}

// This is an explicit unreleased-candidate profile. The normal published suite
// remains usable without sibling repositories; strict candidate CI enables it.
func TestCandidateGeneratedConsumer(t *testing.T) {
	if os.Getenv("PLUTO_CLI_CANDIDATE_PROFILE") != "1" {
		t.Skip("explicit local candidate profile disabled")
	}

	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		if os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE") == "" {
			t.Fatal("missing explicit candidate source", name)
		}
	}

	for _, profile := range []struct{ guard, visibility, table string }{{"not_owner", "tenant", "records"}, {"owner", "tenant", "select"}, {"none", "owner", "order"}} {
		t.Run(profile.guard, func(t *testing.T) {
			t.Setenv("PLUTO_CLI_TEST_GUARD_MODE", profile.guard)
			t.Setenv("PLUTO_CLI_TEST_VISIBILITY", profile.visibility)
			t.Setenv("PLUTO_CLI_TEST_RESOURCE_TABLE", profile.table)
			runCandidateGeneratedConsumer(
				t,
				profile.guard,
				profile.visibility,
				profile.table,
			)
		})
	}
}

func runCandidateGeneratedConsumer(
	t *testing.T,
	guard, visibility, table string,
) {
	root := temporary(t)

	if os.Getenv("PLUTO_CLI_CANDIDATE_KEEP") == "1" {
		kept, err := os.MkdirTemp("", "pluto-cli-candidate-review-")
		if err != nil {
			t.Fatal(err)
		}

		root, err = filepath.EvalSymlinks(kept)
		if err != nil {
			t.Fatal(err)
		}

		t.Log("retained candidate consumer", root)
	}

	args := []string{
		"--no-input",
		"init",
		".",
		"--name",
		"candidate",
		"--module",
		"example.test/candidate",
		"--env-prefix",
		"CANDIDATE_",
		"--multi-tenant",
		"--cache",
		"memory",
		"--no-mise",
		"--no-fnox",
		"--no-docker",
		"--boundary-profile",
		"candidate",
	}
	for _, name := range []string{"AUTH", "RBAC", "CORE", "JOBS"} {
		args = append(
			args,
			"--candidate-"+strings.ToLower(name)+"-source",
			os.Getenv("PLUTO_CLI_CANDIDATE_"+name+"_SOURCE"),
		)
	}

	mustInvoke(t, root, args...)

	blueprint := candidateBlueprint()
	blueprint.Resources[0].Transitions[0].Guard = guard
	blueprint.Resources[0].Visibility = visibility

	blueprint.Resources[0].Table = table
	for index := range blueprint.OperationContracts {
		contract := &blueprint.OperationContracts[index]
		if contract.Kind == "load" || contract.Kind == "list" ||
			contract.Kind == "export" ||
			contract.Kind == "transition" {
			contract.ResourcePolicy = visibility
		}
	}

	data, err := toml.Marshal(blueprint)
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(root, "record-blueprint.toml")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	mustInvoke(t, root, "--no-input", "add", "blueprint", "--file", path)

	if report := project.InspectArchitecture(root); !report.Passed {
		t.Fatalf("candidate architecture: %+v", report.Diagnostics)
	}

	external(t, root, "go", "mod", "tidy")
	external(t, root, "go", "vet", "./...")

	if guard == "not_owner" {
		mustInvoke(t, root, "fmt")
		mustInvoke(t, root, "openapi")
		mustInvoke(t, root, "vet")
	}

	external(t, root, "go", "test", "./...")

	fixture, err := os.ReadFile("testdata/candidate_sql.go.tmpl")
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(
		filepath.Join(root, "internal/app/candidate_sql_test.go"),
		fixture,
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	if guard == "not_owner" {
		runtimeFixture, err := os.ReadFile("testdata/candidate_runtime.go.tmpl")
		if err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(
			filepath.Join(root, "internal/app/candidate_runtime_test.go"),
			runtimeFixture,
			0o644,
		); err != nil {
			t.Fatal(err)
		}
	}

	output := external(
		t,
		root,
		"go",
		"test",
		"-race",
		"-json",
		"-count=1",
		"-run",
		"^TestCandidate(SQL|Runtime)",
		"./internal/app",
	)
	assertCandidateCases(t, output, guard == "not_owner")
	t.Log("candidate consumer", root)
}

func assertCandidateCases(t *testing.T, output string, runtime bool) {
	t.Helper()

	expected := []string{
		"TestCandidateSQLCreateNormalizeReplay",
		"TestCandidateSQLAuthoritativeGuardAndCache",
		"TestCandidateSQLAtomicTransitionAndReplay",
		"TestCandidateSQLVisiblePagesAndExport",
		"TestCandidateSQLConcurrentLogicalCommand",
		"TestCandidateSQLForgedActorFactDenied",
	}
	if runtime {
		expected = append(
			expected,
			"TestCandidateRuntimeSameWriteAcrossAllEntries",
			"TestCandidateRuntimeDeniedBeforeDataOrProvider",
			"TestCandidateRuntimeModelCannotReplaceHostIntent",
			"TestCandidateRuntimeFinalWithoutToolIsNotCompletion",
		)
	}

	passed := map[string]bool{}
	decoder := json.NewDecoder(strings.NewReader(output))

	for {
		var event struct{ Action, Test string }

		err := decoder.Decode(&event)
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			t.Fatalf("invalid candidate test evidence: %v\n%s", err, output)
		}

		if event.Action == "skip" || event.Action == "fail" {
			t.Fatalf("candidate case did not execute successfully: %+v", event)
		}

		if event.Action == "pass" {
			passed[event.Test] = true
		}
	}

	for _, name := range expected {
		if !passed[name] {
			t.Fatalf(
				"required candidate case did not pass: %s\n%s",
				name,
				output,
			)
		}
	}

	t.Logf(
		"verified %d required candidate SQL/runtime cases without skips",
		len(expected),
	)
}
