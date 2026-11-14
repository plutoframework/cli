package migrate

import (
	"strconv"
	"strings"
	"testing"
)

func TestDanglingReferencesReportsRemovedDeclarations(t *testing.T) {
	root := t.TempDir()
	writeTree(t, root, map[string]string{
		"internal/app/routes_gen.go": "package app\n\nfunc RegisterGenerated() " +
			"{}\n\nfunc helper() {}\n\nconst Keep = 1\n",
		"internal/app/app.go": "package app\n\nfunc Run() {\n\t" +
			"RegisterGenerated()\n\thelper()\n\t_ = Keep\n}\n",
		"cmd/api/main.go": "package main\n\nimport application " +
			"\"example.com/x/internal/app\"\n\nfunc main() {\n\t" +
			"application.RegisterGenerated()\n\t_ = application.Keep\n}\n",
		"cmd/tool/main.go": "package main\n\nimport \"example.com/x/" +
			"internal/app\"\n\nfunc main() {\n\tapp := struct{ " +
			"RegisterGenerated func() }{}\n\tapp.RegisterGenerated()\n}\n",
		".hidden/skip.go": "package app\n\nfunc x() { RegisterGenerated() }\n",
	})

	plan := &Plan{
		Root:     root,
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
	}
	workspace := &Workspace{Root: root, plan: plan}

	if err := workspace.Write(
		"internal/app/routes_gen.go",
		[]byte("package app\n\nconst Keep = 2\n"),
	); err != nil {
		t.Fatal(err)
	}

	if err := danglingReferences(
		workspace,
		"example.com/x",
		"candidate",
	); err != nil {
		t.Fatal(err)
	}

	got := []string{}
	for _, item := range plan.Manual {
		got = append(got, item.Path+":"+strconv.Itoa(item.Line)+" "+item.Reason)
	}

	want := []string{
		"internal/app/app.go:4 uses app.RegisterGenerated",
		"internal/app/app.go:5 uses app.helper",
		"cmd/api/main.go:6 uses application.RegisterGenerated",
	}

	joined := strings.Join(got, "\n")
	for _, item := range want {
		if !strings.Contains(joined, item) {
			t.Fatalf("missing %q in:\n%s", item, joined)
		}
	}

	if len(got) != len(want) {
		t.Fatalf("unexpected references:\n%s", joined)
	}
}
