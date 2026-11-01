package readability

import "testing"

func TestWideLiteralContainers(t *testing.T) {
	for _, source := range []string{
		"package fixture\n" +
			`var names = []string{"first long descriptive argument", ` +
			`"second long descriptive argument", "third"}` + "\n",
		"package fixture\nfunc example() {\n" +
			` println("first long descriptive argument", ` +
			`"second long descriptive argument", "third")` + "\n}\n",
	} {
		if len(Check("fixture.go", []byte(source))) == 0 {
			t.Fatal("wide literal container accepted")
		}
	}
}

func TestExpandedLiteralContainers(t *testing.T) {
	source := `package fixture
var names = []string{
 "first long descriptive argument",
 "second long descriptive argument",
 "third",
}
`
	if issues := Check("fixture.go", []byte(source)); len(issues) != 0 {
		t.Fatal(issues)
	}
}
