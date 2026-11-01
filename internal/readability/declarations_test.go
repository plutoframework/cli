package readability

import (
	"strings"
	"testing"
)

func TestNamedFunctionBodiesRequireExpandedLayout(t *testing.T) {
	for _, declaration := range []string{
		"func compact(){first();second()}",
		"type receiver int\nfunc(receiver) compact(){first();second()}",
	} {
		if found := Check(
			"fixture.go",
			[]byte("package fixture\n"+declaration+"\n"),
		); !strings.Contains(
			strings.Join(found, "\n"),
			"expand function body onto separate lines",
		) {
			t.Fatalf("compact named function escaped: %s", declaration)
		}
	}

	for _, declaration := range []string{
		"func expanded() {\n    first()\n    second()\n}",
		"type receiver int\n" +
			"func(receiver) expanded() {\n    first()\n    second()\n}",
		"func empty() {}",
	} {
		if found := Check(
			"fixture.go",
			[]byte("package fixture\n"+declaration+"\n"),
		); len(found) != 0 {
			t.Fatalf("wrapped or empty named function rejected: %+v", found)
		}
	}
}
