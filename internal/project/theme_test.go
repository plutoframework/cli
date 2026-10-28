package project

import "testing"

func TestThemeValidation(t *testing.T) {
	valid := Frontend{Directory: "web", Theme: &Theme{
		Mode:       "system",
		Radius:     "0.75rem",
		FontFamily: "Manrope, sans-serif",
		Overrides:  "web/src/styles/overrides.css",
		Dark: map[string]string{
			"accent":     "#338EF7",
			"background": "#09090B",
		},
	}}
	if err := valid.ValidateTheme(); err != nil {
		t.Fatal(err)
	}

	for _, theme := range []Theme{
		{Mode: "auto"},
		{Radius: "1rem; color:red"},
		{FontFamily: "url(https://example.test)"},
		{Overrides: "../outside.css"},
		{Overrides: "web/src/generated/theme.css"},
		{Overrides: "web/src/styles/../theme.css"},
		{Dark: map[string]string{"unknown": "#123456"}},
		{Light: map[string]string{"accent": "red; } body {}"}},
	} {
		valid.Theme = &theme
		if err := valid.ValidateTheme(); err == nil {
			t.Fatalf("accepted invalid theme: %+v", theme)
		}
	}
}
