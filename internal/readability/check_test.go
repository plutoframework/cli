package readability

import (
	"strings"
	"testing"
)

func TestCheckFormatterBlindSpots(t *testing.T) {
	for _, source := range []string{
		"package sample\n// Code generated; DO NOT EDIT.\nvar " +
			"callback=func(){first();second()}\n",
		"package sample\ntype fields struct{first,second string}\n",
		"package sample\nfunc read(){\n" + strings.Repeat(
			"\t",
			21,
		) + "call()\n}\n",
	} {
		if len(Check("fixture.go", []byte(source))) == 0 {
			t.Fatal("unreadable code accepted", source)
		}
	}

	source := "package sample\n// " + strings.Repeat(
		"comment",
		30,
	) + "\nconst message=\"" + strings.Repeat(
		"data",
		40,
	) + "\"\n"
	if found := Check("fixture.go", []byte(source)); len(found) != 0 {
		t.Fatal(found)
	}
}
