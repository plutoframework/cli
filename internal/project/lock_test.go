package project

import (
	"strings"
	"testing"
)

func TestLockRoundTripAndGoFormatting(t *testing.T) {
	source := []byte("package a\n\nfunc F(a, b int) int { return a + b }\n")
	formatted := []byte(
		"package a\n\nfunc F(a, b int) int {\n\treturn a + b\n}\n",
	)
	lock := Lock{"a.go": SourceDigest("a.go", source)}

	parsed, err := ParseLock(lock.Render())
	if err != nil {
		t.Fatal(err)
	}

	if !parsed.Matches("a.go", formatted) {
		t.Fatal("formatter-only change must keep the file pristine")
	}

	if parsed.Matches("a.go", append(source, []byte("var x = 1\n")...)) {
		t.Fatal("token change must not match")
	}

	if _, err := ParseLock([]byte("nothex a.go\n")); err == nil {
		t.Fatal("malformed lock accepted")
	}
}

func TestContractDiffersDistinguishesStaleFromHandEdit(t *testing.T) {
	data := []byte("package a\n")
	lock := Lock{"a.go": SourceDigest("a.go", data)}

	stale := contractDiffers("a.go", data, lock, true).Error()
	if !strings.Contains(stale, "run pluto generate") ||
		strings.Contains(stale, "restore") {
		t.Fatalf("stale message: %s", stale)
	}

	edited := contractDiffers("a.go", []byte("package b\n"), lock, true)
	if !strings.Contains(edited.Error(), "restore generated content") {
		t.Fatalf("edit message: %v", edited)
	}

	unlocked := contractDiffers("a.go", data, Lock{}, false)
	if !strings.Contains(unlocked.Error(), "--adopt") {
		t.Fatalf("unlocked message: %v", unlocked)
	}
}
