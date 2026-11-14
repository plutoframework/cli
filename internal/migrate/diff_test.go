package migrate

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteDiffShowsAddsDeletesAndHunks(t *testing.T) {
	before := strings.Repeat("same\n", 10) + "old\n" +
		strings.Repeat("tail\n", 10)
	after := strings.Repeat("same\n", 10) + "new\n" +
		strings.Repeat("tail\n", 10)
	plan := &Plan{
		Writes: map[string][]byte{
			"a.txt": []byte(after),
			"b.txt": []byte("created"),
		},
		Deletes: map[string]bool{"c.txt": true},
		Original: map[string][]byte{
			"a.txt": []byte(before),
			"c.txt": []byte("removed\n"),
		},
	}

	var out bytes.Buffer
	if err := WriteDiff(&out, plan); err != nil {
		t.Fatal(err)
	}

	want := "--- a/a.txt\n+++ b/a.txt\n@@ -8,7 +8,7 @@\n same\n same\n " +
		"same\n-old\n+new\n tail\n tail\n tail\n" +
		"--- /dev/null\n+++ b/b.txt\n@@ -0,0 +1,1 @@\n+created\n" +
		"\\ No newline at end of file\n" +
		"--- a/c.txt\n+++ /dev/null\n@@ -1,1 +0,0 @@\n-removed\n"
	if out.String() != want {
		t.Fatalf("diff:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestDiffFallsBackForHugeInputs(t *testing.T) {
	a := strings.Split(strings.Repeat("a\n", 2100), "\n")
	b := strings.Split(strings.Repeat("b\n", 2100), "\n")

	edits := middle(a, b)
	if len(edits) != len(a)+len(b) {
		t.Fatalf("fallback edits %d", len(edits))
	}
}
