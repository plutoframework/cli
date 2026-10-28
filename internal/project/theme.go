package project

import (
	"fmt"
	"regexp"
	"strings"
)

// Theme contains build-time presentation settings, never runtime secrets.
type Theme struct {
	Mode       string            `toml:"mode,omitempty"`
	Radius     string            `toml:"radius,omitempty"`
	FontFamily string            `toml:"font_family,omitempty"`
	Overrides  string            `toml:"overrides,omitempty"`
	Light      map[string]string `toml:"light,omitempty"`
	Dark       map[string]string `toml:"dark,omitempty"`
}

var (
	themeColor   = regexp.MustCompile(`^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$`)
	themeRadius  = regexp.MustCompile(`^(0|[0-9]{1,2}(\.[0-9]{1,3})?(px|rem))$`)
	themeFont    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 ,\-]{0,119}$`)
	themeSegment = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)
)

func (f Frontend) ValidateTheme() error {
	t := f.Theme
	if t == nil {
		return nil
	}

	switch t.Mode {
	case "", "system", "light", "dark":
	default:
		return fmt.Errorf("frontend.theme.mode must be system, light or dark")
	}

	if t.Radius != "" && !themeRadius.MatchString(t.Radius) {
		return fmt.Errorf(
			"frontend.theme.radius must be 0 or a nonnegative px/rem length (up to 99.999)",
		)
	}

	if t.FontFamily != "" && !themeFont.MatchString(t.FontFamily) {
		return fmt.Errorf(
			"frontend.theme.font_family must be a plain CSS family list without quotes or URLs",
		)
	}

	if t.Overrides != "" {
		prefix := f.Directory + "/src/"
		if !strings.HasPrefix(t.Overrides, prefix) ||
			!strings.HasSuffix(t.Overrides, ".css") ||
			strings.Contains(t.Overrides, "/generated/") {
			return fmt.Errorf(
				"frontend.theme.overrides must be a CSS file below %ssrc outside generated directories",
				f.Directory+"/",
			)
		}

		parts := strings.Split(
			strings.TrimSuffix(strings.TrimPrefix(t.Overrides, prefix), ".css"),
			"/",
		)
		for _, part := range parts {
			if !themeSegment.MatchString(part) {
				return fmt.Errorf(
					"frontend.theme.overrides requires safe lowercase path segments",
				)
			}
		}
	}

	allowed := map[string]bool{
		"accent":             true,
		"accent_foreground":  true,
		"background":         true,
		"foreground":         true,
		"surface":            true,
		"surface_foreground": true,
		"muted":              true,
		"border":             true,
		"success":            true,
		"warning":            true,
		"danger":             true,
	}

	for _, colors := range []map[string]string{t.Light, t.Dark} {
		for key, value := range colors {
			if !allowed[key] || !themeColor.MatchString(value) {
				return fmt.Errorf(
					"invalid frontend theme token %q: use a supported token with #RRGGBB or #RRGGBBAA",
					key,
				)
			}
		}
	}

	return nil
}
