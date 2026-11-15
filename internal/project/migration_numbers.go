package project

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
)

// Migrations records numbers for framework-owned SQL migrations. A new
// application keeps the default numbers and records nothing; an adopted
// application records the numbers it was allocated, because migrations that
// were already applied are never renamed.
type Migrations struct {
	Framework map[string]string `toml:"framework,omitempty"`
}

// FrameworkMigration is one framework-owned migration at its effective path.
type FrameworkMigration struct {
	Key    string
	Number string
	Path   string
}

const migrationDirectory = "db/migrations/"

var (
	migrationName = regexp.MustCompile(
		`^db/migrations/(\d+)_([a-z0-9_]+)\.sql$`,
	)
	migrationNumber = regexp.MustCompile(`^\d{3}$`)
)

// frameworkMigrationKeys are the candidate migrations whose numbers may be
// recorded. 001 is the fixed foundation; 099 and 100 are generated
// memberships and resource tables and stay where they are.
var frameworkMigrationKeys = map[string]string{
	"auth_sessions":       "002",
	"idempotency":         "003",
	"local_credentials":   "004",
	"jobs":                "005",
	"authentication":      "006",
	"external_accounts":   "007",
	"credential_attempts": "008",
	"device_grants":       "090",
}

// ReservedMigrationNumber reports numbers never allocated to framework migrations.
func ReservedMigrationNumber(number string) bool {
	return number == "001" || number >= "099"
}

// ParseMigrationPath splits db/migrations/NNN_key.sql.
func ParseMigrationPath(path string) (number, key string, ok bool) {
	match := migrationName.FindStringSubmatch(path)
	if match == nil {
		return "", "", false
	}

	return match[1], match[2], true
}

// FrameworkMigrations lists the framework-owned migrations present in a
// rendered file set, in number order.
func FrameworkMigrations(files map[string][]byte) []FrameworkMigration {
	result := []FrameworkMigration{}

	for path := range files {
		number, key, ok := ParseMigrationPath(path)
		if !ok {
			continue
		}

		if _, framework := frameworkMigrationKeys[key]; framework {
			result = append(result, FrameworkMigration{key, number, path})
		}
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Path < result[j].Path
	})

	return result
}

func (m Manifest) validateMigrations() error {
	if len(m.Migrations.Framework) == 0 {
		return nil
	}

	if m.Boundary.Mode != "candidate" {
		return fmt.Errorf(
			"[migrations.framework] requires the candidate boundary profile",
		)
	}

	keys := make([]string, 0, len(m.Migrations.Framework))
	for key := range m.Migrations.Framework {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	effective := map[string]string{}
	for key, number := range frameworkMigrationKeys {
		effective[key] = number
	}

	for _, key := range keys {
		number := m.Migrations.Framework[key]

		if _, known := frameworkMigrationKeys[key]; !known {
			return fmt.Errorf(
				"[migrations.framework] unknown framework migration %q",
				key,
			)
		}

		if !migrationNumber.MatchString(number) || ReservedMigrationNumber(
			number,
		) {
			return fmt.Errorf(
				"[migrations.framework] %s = %q must be a three digit "+
					"number from 002 to 098",
				key,
				number,
			)
		}

		effective[key] = number
	}

	owner := map[string]string{}

	for _, key := range sortedKeys(effective) {
		number := effective[key]
		if other, taken := owner[number]; taken {
			return fmt.Errorf(
				"[migrations.framework] number %s is used by both %s and %s",
				number,
				other,
				key,
			)
		}

		owner[number] = key
	}

	return nil
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// applyFrameworkMigrations moves rendered framework migrations to their
// recorded numbers. It is idempotent: InitialFiles re-adds default paths for
// anything Managed did not render, and those duplicates are removed here.
func applyFrameworkMigrations(m Manifest, files map[string][]byte) {
	for key, number := range m.Migrations.Framework {
		target := migrationDirectory + number + "_" + key + ".sql"

		var content []byte

		found := false

		for path, source := range files {
			_, name, ok := ParseMigrationPath(path)
			if !ok || name != key {
				continue
			}

			if path == target {
				content, found = source, true

				continue
			}

			if !found {
				content = source
			}

			found = true

			delete(files, path)
		}

		if found {
			files[target] = content
		}
	}
}

// SameSQL compares migrations ignoring line-ending and trailing whitespace
// differences.
func SameSQL(a, b []byte) bool {
	return string(
		bytes.TrimSpace(bytes.ReplaceAll(a, []byte("\r\n"), []byte("\n"))),
	) ==
		string(
			bytes.TrimSpace(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))),
		)
}
