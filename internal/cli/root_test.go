package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/plutoframework/cli/internal/project"
)

func temporary(t *testing.T) string {
	t.Helper()

	path, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return path
}

func invoke(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	var output bytes.Buffer

	command := New(
		Options{
			Directory: dir,
			Out:       &output,
			Err:       &output,
			In:        strings.NewReader(""),
			Version:   "test",
		},
	)
	command.SetArgs(args)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	err := command.ExecuteContext(ctx)

	return output.String(), err
}

func mustInvoke(t *testing.T, dir string, args ...string) string {
	t.Helper()

	output, err := invoke(t, dir, args...)
	if err != nil {
		t.Fatalf("pluto %v: %v\n%s", args, err, output)
	}

	return output
}

func external(t *testing.T, dir, name string, args ...string) string {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	command := exec.CommandContext(ctx, name, args...)
	command.Dir = dir

	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, output)
	}

	return string(output)
}

func snapshot(t *testing.T, root string) map[string][32]byte {
	t.Helper()

	files := map[string][32]byte{}

	err := filepath.WalkDir(
		root,
		func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				if entry.Name() == ".git" {
					return filepath.SkipDir
				}

				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			files[strings.TrimPrefix(path, root)] = sha256.Sum256(data)

			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	return files
}

func assertUnchanged(t *testing.T, before, after map[string][32]byte) {
	t.Helper()

	if len(before) != len(after) {
		t.Fatal("failed command changed file count")
	}

	for path, hash := range before {
		if after[path] != hash {
			t.Fatalf("changed %s", path)
		}
	}
}

func TestHelpVersionAndExit(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"init", "--help"}, {"add", "endpoint", "--help"}, {"version"}, {"--version"}} {
		if output := mustInvoke(
			t,
			temporary(t),
			args...); !strings.Contains(output, "pluto") &&
			!strings.Contains(output, "Pluto") {
			t.Fatal(output)
		}
	}

	var output bytes.Buffer
	if code := Execute(
		context.Background(),
		[]string{"missing"},
		nil,
		&output,
		&output,
		"test",
	); code != 1 ||
		!strings.Contains(output.String(), "unknown command") {
		t.Fatalf("code=%d output=%s", code, output.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if code := Execute(
		ctx,
		[]string{"init", "/should-not-create"},
		nil,
		&output,
		&output,
		"test",
	); code != 1 {
		t.Fatal("cancelled context accepted")
	}
}

func TestGeneratedApplications(t *testing.T) {
	for _, multi := range []bool{false, true} {
		name := "single"
		if multi {
			name = "multi"
		}

		t.Run(name, func(t *testing.T) {
			root := temporary(t)

			args := []string{
				"init",
				".",
				"--name",
				"demo",
				"--module",
				"example.com/demo",
				"--env-prefix",
				"DEMO_",
			}
			if multi {
				args = append(args, "--multi-tenant")
			}

			mustInvoke(t, root, args...)

			if !project.InWorktree(context.Background(), root) {
				t.Fatal("git not initialized")
			}

			external(t, root, "go", "test", "./...")

			for _, endpoint := range [][]string{
				{"widgets", "Create", "--method", "POST", "--path", "/api/widgets/{id}", "--permission", "widgets.write", "--feature", "widgets-create", "--feature-client-visible"},
				{"widgets", "Get", "--method", "GET", "--path", "/api/widgets/{id}", "--permission", "widgets.write"},
				{"reports", "List", "--method", "GET", "--path", "/api/reports", "--feature", "private-report"},
			} {
				mustInvoke(
					t,
					root,
					append(
						append([]string{"add", "endpoint"}, endpoint...),
						"--name",
						"Endpoint name",
						"--description",
						"An original synthetic endpoint.",
					)...)
			}

			rolePath := filepath.Join(root, "internal/access/roles.go")

			roleSource, err := os.ReadFile(rolePath)
			if err != nil {
				t.Fatal(err)
			}

			if !bytes.Contains(roleSource, []byte("return []rbac.Role{}")) {
				t.Fatal("generated roles grant capabilities automatically")
			}

			roleSource = bytes.Replace(
				roleSource,
				[]byte("return []rbac.Role{}"),
				[]byte(`return []rbac.Role{
                {
                    ID: "writer",
                    Name: "Writer",
                    Description: "Can write widgets.",
                    Permissions: []rbac.PermissionID{WidgetsCreatePermission},
                },
                {
                    ID: "fixture",
                    Name: "Test reviewer",
                    Description: "Inherits widget writing for integration tests.",
                    Inherits: []rbac.RoleID{"writer"},
                },
                {
                    ID: "viewer",
                    Name: "Viewer",
                    Description: "No write grants.",
                },
            }`),
				1,
			)
			if err := os.WriteFile(rolePath, roleSource, 0o644); err != nil {
				t.Fatal(err)
			}

			authored := filepath.Join(
				root,
				"internal/operations/widgets/create.go",
			)

			original, err := os.ReadFile(authored)
			if err != nil {
				t.Fatal(err)
			}

			original = bytes.Replace(
				original,
				[]byte("NewCreate(policy *rbac.Policy)"),
				[]byte("NewCreate(policy *rbac.Policy, _ string)"),
				1,
			)

			original = append(
				original,
				[]byte(
					"\n// Author-owned annotation survives subsequent group generation.\n",
				)...)
			if err := os.WriteFile(authored, original, 0o644); err != nil {
				t.Fatal(err)
			}

			bindingPath := filepath.Join(root, "internal/app/widgets_create.go")
			bindingOriginal := replaceSource(
				t,
				bindingPath,
				"NewCreate(dependencies.Policy)",
				`NewCreate(dependencies.Policy, "authored-dependency")`,
			)
			replaceSource(
				t,
				filepath.Join(
					root,
					"internal/operations/widgets/create_test.go",
				),
				"NewCreate(policy)",
				`NewCreate(policy, "unit-fixture")`,
			)

			mustInvoke(
				t,
				root,
				"add",
				"endpoint",
				"reports",
				"Get",
				"--method",
				"GET",
				"--path",
				"/api/reports/{id}",
				"--permission",
				"reports:read",
				"--name",
				"Read report",
				"--description",
				"Read an authorized report",
			)

			preserved, err := os.ReadFile(authored)
			if err != nil || !bytes.Equal(preserved, original) {
				t.Fatal("authored operation overwritten")
			}

			bindingPreserved, err := os.ReadFile(bindingPath)
			if err != nil || !bytes.Equal(bindingPreserved, bindingOriginal) {
				t.Fatal("authored constructor binding overwritten")
			}

			retainedRoles, err := os.ReadFile(rolePath)
			if err != nil || !bytes.Equal(roleSource, retainedRoles) {
				t.Fatal("endpoint addition overwrote authored RBAC roles")
			}

			if !multi {
				mustInvoke(
					t,
					root,
					"add",
					"endpoint",
					"publicapi",
					"Read",
					"--method",
					"GET",
					"--path",
					"/api/public",
					"--public",
					"--name",
					"Public",
					"--description",
					"Explicit public endpoint",
				)
			}

			_, manifest, err := project.Discover(
				filepath.Join(root, "internal", "handlers", "widgets"),
			)
			if err != nil {
				t.Fatal(err)
			}

			if manifest.Project.EnvPrefix != "DEMO_" ||
				(manifest.Capabilities.Tenancy == "multi") != multi {
				t.Fatal("saved choices lost")
			}

			source, err := os.ReadFile("testdata/behavior.go.tmpl")
			if err != nil {
				t.Fatal(err)
			}

			tmpl, err := template.New("behavior").Parse(string(source))
			if err != nil {
				t.Fatal(err)
			}

			var behavior bytes.Buffer
			if err := tmpl.Execute(&behavior, struct {
				Module string
				Multi  bool
			}{"example.com/demo", multi}); err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				filepath.Join(root, "internal/app/independent_test.go"),
				behavior.Bytes(),
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			external(t, root, "go", "test", "-race", "./...")
			external(t, root, "go", "vet", "./...")
			external(
				t,
				root,
				"go",
				"build",
				"-o",
				filepath.Join(t.TempDir(), "api"),
				"./cmd/api",
			)
			mustInvoke(t, root, "openapi", "--check")

			artifact := filepath.Join(root, "openapi/openapi.json")

			document, err := os.ReadFile(artifact)
			if err != nil {
				t.Fatal(err)
			}

			var openapi struct {
				Paths map[string]map[string]struct {
					ID         string `json:"operationId"`
					Feature    string `json:"x-feature"`
					Permission string `json:"x-permission"`
					Security   []any  `json:"security"`
					Parameters []struct {
						Name string `json:"name"`
					} `json:"parameters"`
				} `json:"paths"`
			}
			if err := json.Unmarshal(document, &openapi); err != nil {
				t.Fatal(err)
			}

			spec := openapi.Paths["/api/widgets/{id}"]["post"]
			if spec.ID != "widgets.Create" ||
				spec.Feature != "widgets-create" ||
				spec.Permission != "widgets.write" ||
				len(spec.Security) != 2 ||
				len(spec.Parameters) != 2 ||
				spec.Parameters[1].Name != "Idempotency-Key" ||
				spec.Parameters[0].Name != "id" {
				t.Fatalf("OpenAPI metadata lost: %+v", spec)
			}

			list := openapi.Paths["/api/reports"]["get"]
			if len(list.Parameters) != 2 ||
				list.Parameters[0].Name != "limit" ||
				list.Parameters[1].Name != "cursor" {
				t.Fatal("collection pagination missing", list)
			}

			if len(openapi.Paths["/api/reports/{id}"]["get"].Parameters) != 1 {
				t.Fatal("detail unexpectedly paginated")
			}

			if openapi.Paths["/api/reports/{id}"]["get"].Permission != "reports:read" {
				t.Fatal("colon-separated RBAC capability missing from OpenAPI")
			}

			if err := os.WriteFile(
				artifact,
				[]byte("{}\n"),
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			if _, err := invoke(t, root, "openapi", "--check"); err == nil {
				t.Fatal("stale artifact accepted")
			}

			stale, err := os.ReadFile(artifact)
			if err != nil || string(stale) != "{}\n" {
				t.Fatal("OpenAPI check mutated artifact")
			}

			mustInvoke(t, root, "openapi")

			if err := project.Contracts(root, manifest); err != nil {
				t.Fatal(err)
			}

			t.Run("style-and-vet", func(t *testing.T) {
				if _, err := exec.LookPath("golangci-lint"); err != nil {
					t.Skip(
						"put pinned golangci-lint 2.15.0 on PATH to exercise fmt/vet",
					)
				}

				mustInvoke(t, root, "fmt")

				before := snapshot(t, root)
				mustInvoke(t, root, "fmt")
				assertUnchanged(t, before, snapshot(t, root))
				mustInvoke(t, root, "vet")
				assertUnchanged(t, before, snapshot(t, root))
			})

			// Repeat/conflicting/invalid operations must leave every authored file intact.
			before := snapshot(t, root)

			failures := [][]string{
				args,
				{
					"add",
					"endpoint",
					"widgets",
					"Create",
					"--path",
					"/api/other",
					"--name",
					"Duplicate",
					"--description",
					"Duplicate identifier",
				},
				{
					"add",
					"endpoint",
					"widgets",
					"Other",
					"--path",
					"/api/widgets/{other}",
					"--name",
					"Duplicate",
					"--description",
					"Duplicate route shape",
				},
				{
					"add",
					"endpoint",
					"../escape",
					"Bad",
					"--path",
					"/bad",
					"--name",
					"Invalid",
					"--description",
					"Traversal",
				},
				{
					"add",
					"endpoint",
					"context",
					"Bad",
					"--path",
					"/bad",
					"--name",
					"Invalid",
					"--description",
					"Reserved package",
				},
				{
					"add",
					"endpoint",
					"widgets",
					"Bad",
					"--path",
					"/api/{id}/{id}",
					"--name",
					"Invalid",
					"--description",
					"Duplicate parameters",
				},
			}
			if multi {
				failures = append(
					failures,
					[]string{
						"add",
						"endpoint",
						"widgets",
						"Public",
						"--path",
						"/public",
						"--public",
						"--name",
						"Public",
						"--description",
						"No scope",
					},
				)
			}

			for _, args := range failures {
				if output, err := invoke(t, root, args...); err == nil {
					t.Fatalf("accepted %v: %s", args, output)
				}

				assertUnchanged(t, before, snapshot(t, root))
			}

			// Managed semantic tampering is detected; no subsequent generation occurs.
			path := filepath.Join(root, "internal/access/permissions_gen.go")

			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(
				path,
				bytes.Replace(
					content,
					[]byte("widgets.write"),
					[]byte("widgets.admin"),
					1,
				),
				0o644,
			); err != nil {
				t.Fatal(err)
			}

			if err := project.Contracts(root, manifest); err == nil {
				t.Fatal("managed tamper accepted")
			}
		})
	}
}

func replaceSource(t *testing.T, path, old, new string) []byte {
	t.Helper()

	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(source), old) != 1 {
		t.Fatalf("source fixture drifted: %s", path)
	}

	source = bytes.Replace(source, []byte(old), []byte(new), 1)
	if err := os.WriteFile(path, source, 0o644); err != nil {
		t.Fatal(err)
	}

	return source
}
