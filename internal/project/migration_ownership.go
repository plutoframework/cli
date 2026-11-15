package project

import (
	"os"
	"path/filepath"
	"sort"
)

// migrationOwnership is ARCH613. Framework-owned migrations must sit at
// exactly the paths [migrations.framework] records (or the defaults), and no
// two migration files may share a number. Content of applied files is held by
// the managed-boundary comparison (ARCH202).
func migrationOwnership(
	root string,
	m Manifest,
	managed map[string][]byte,
) []ArchitectureDiagnostic {
	diagnostics := []ArchitectureDiagnostic{}

	add := func(path, message, repair string) {
		diagnostics = append(diagnostics, ArchitectureDiagnostic{
			ID:       "ARCH613",
			File:     path,
			Line:     1,
			Severity: "error",
			Message:  message,
			Repair:   repair,
		})
	}

	entries, err := os.ReadDir(filepath.Join(root, "db", "migrations"))
	if err != nil {
		return diagnostics
	}

	byNumber := map[string][]string{}
	present := map[string]bool{}

	for _, entry := range entries {
		path := migrationDirectory + entry.Name()

		number, _, ok := ParseMigrationPath(path)
		if !ok {
			continue
		}

		present[path] = true
		byNumber[number] = append(byNumber[number], path)
	}

	framework := FrameworkMigrations(managed)
	for _, item := range framework {
		for path := range present {
			number, key, ok := ParseMigrationPath(path)
			if !ok || key != item.Key || path == item.Path {
				continue
			}

			add(
				path,
				"framework migration "+item.Key+" is at number "+number+
					" but pluto.toml records "+item.Number,
				"Never rename an applied migration: record its number "+
					"under [migrations.framework] "+item.Key+" or move "+
					"the unapplied file to "+item.Path+".",
			)
		}
	}

	numbers := make([]string, 0, len(byNumber))
	for number := range byNumber {
		numbers = append(numbers, number)
	}

	sort.Strings(numbers)

	for _, number := range numbers {
		paths := byNumber[number]
		if len(paths) < 2 || allManaged(paths, managed) {
			continue
		}

		sort.Strings(paths)

		for _, path := range paths[1:] {
			add(
				path,
				"migration number "+number+" is shared with "+paths[0],
				"Give each migration a unique number; allocate new ones "+
					"after the highest applied migration.",
			)
		}
	}

	return diagnostics
}

// allManaged tolerates numbers the generator itself shares, such as one
// resource migration per resource at 100.
func allManaged(paths []string, managed map[string][]byte) bool {
	for _, path := range paths {
		if _, ok := managed[path]; !ok {
			return false
		}
	}

	return true
}
