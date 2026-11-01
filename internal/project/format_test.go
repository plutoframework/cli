package project

import (
	"strings"
	"testing"
)

func TestGeneratedImportGroupsPreserveAliases(t *testing.T) {
	source := []byte(`package sample
import (
 tool "github.com/vendor/tool"
 "strings"
 local "example.test/app/internal/value"
 "fmt"
)
`)

	formatted, err := formatGenerated(source, "example.test/app")
	if err != nil {
		t.Fatal(err)
	}

	expected := `import (
 "fmt"
 "strings"

 local "example.test/app/internal/value"

 tool "github.com/vendor/tool"
)`

	expected = strings.ReplaceAll(expected, "\n ", "\n\t")
	if !strings.Contains(string(formatted), expected) {
		t.Fatal(string(formatted))
	}

	second, err := formatGenerated(formatted, "example.test/app")
	if err != nil ||
		string(
			second,
		) != string(
			formatted,
		) {
		t.Fatal(
			"formatting is not idempotent",
			err,
		)
	}
}
