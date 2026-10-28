package project

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

type ComponentRegistry struct {
	Path string `toml:"path"`
}

var (
	packagePin = regexp.MustCompile(
		`^[0-9]+\.[0-9]+\.[0-9]+(-[A-Za-z0-9.-]+)?$`,
	)
	packageID      = regexp.MustCompile(`^(@[a-z0-9-]+/)?[a-z0-9][a-z0-9._-]*$`)
	registryDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
)

func FrontendName(value string) bool { return appName.MatchString(value) }

func RegistryPath(value string) bool {
	return value != "" && !strings.ContainsAny(value, "\\\x00\r\n") &&
		!path.IsAbs(value) &&
		path.Clean(value) == value &&
		value != "." &&
		value != ".." &&
		!strings.HasPrefix(value, "../")
}

func PackageDependency(name, pin string) bool {
	return packageID.MatchString(name) && packagePin.MatchString(pin)
}

func (f Frontend) ValidateRegistries() error {
	for name, registry := range f.Registries {
		if !FrontendName(name) || name == "pluto" ||
			!RegistryPath(registry.Path) {
			return fmt.Errorf(
				"registry requires a kebab-case namespace and a clean project-relative path",
			)
		}
	}

	pins := map[string]string{}

	for _, c := range f.Components {
		for _, primitive := range c.Primitives {
			switch primitive {
			case "button", "input", "label", "card", "dialog":
			default:
				return fmt.Errorf("unknown registry primitive %s", primitive)
			}
		}

		if c.From != "" {
			parts := strings.Split(c.From, "/")
			if len(parts) != 2 || !FrontendName(parts[0]) ||
				!FrontendName(parts[1]) ||
				!registryDigest.MatchString(c.Digest) ||
				c.Kind != "registry" {
				return fmt.Errorf("invalid registry component provenance")
			}
		} else if c.Kind == "registry" || c.Digest != "" || len(c.Packages)+len(c.DevPackages) > 0 {
			return fmt.Errorf("custom dependencies require registry provenance")
		}

		for section, values := range map[string]map[string]string{"dependencies": c.Packages, "devDependencies": c.DevPackages} {
			for name, pin := range values {
				if !PackageDependency(name, pin) {
					return fmt.Errorf(
						"registry dependency %q requires an exact semver pin",
						name,
					)
				}

				value := section + ":" + pin
				if previous, exists := pins[name]; exists && previous != value {
					return fmt.Errorf(
						"conflicting registry dependency %s",
						name,
					)
				}

				pins[name] = value
			}
		}
	}

	return nil
}
