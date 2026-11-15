package migrate

import (
	"fmt"

	"github.com/plutoframework/cli/internal/project"
)

// maxFrameworkNumber is the highest number allocated to a framework
// migration; 099 and above belong to generated memberships and resources.
const maxFrameworkNumber = 98

// allocateMigrations decides the numbers of framework migrations at adoption.
// Existing files are never renamed: a framework migration keeps its default
// number when that number is free or already holds identical content, reuses
// the number of a content-identical file, and otherwise takes max(existing)+1
// below the generated range. Only numbers that differ from the defaults are
// returned, so a new application records nothing.
func allocateMigrations(
	w *Workspace,
	rendered map[string][]byte,
) (map[string]string, error) {
	existing, err := w.List("db/migrations")
	if err != nil {
		return nil, err
	}

	taken := map[string]string{}
	highest := 0

	for _, name := range existing {
		number, _, ok := project.ParseMigrationPath(name)
		if !ok {
			continue
		}

		taken[number] = name

		var value int

		if _, err := fmt.Sscanf(number, "%d", &value); err == nil &&
			value <= maxFrameworkNumber && value > highest {
			highest = value
		}
	}

	recorded := map[string]string{}

	for _, item := range project.FrameworkMigrations(rendered) {
		resolved, err := resolveMigration(
			w,
			item,
			rendered[item.Path],
			existing,
			taken,
		)
		if err != nil {
			return nil, err
		}

		if resolved != "" {
			if resolved != item.Number {
				recorded[item.Key] = resolved
			}

			taken[resolved] = item.Key

			continue
		}

		highest++
		if highest > maxFrameworkNumber {
			return nil, fmt.Errorf(
				"no migration number left for framework migration %s",
				item.Key,
			)
		}

		number := fmt.Sprintf("%03d", highest)
		recorded[item.Key] = number
		taken[number] = item.Key
	}

	return recorded, nil
}

// resolveMigration returns the number an existing or free slot gives a
// framework migration, or "" when a new number must be allocated. Published
// migrations are matched by content, never by number.
func resolveMigration(
	w *Workspace,
	item project.FrameworkMigration,
	want []byte,
	existing []string,
	taken map[string]string,
) (string, error) {
	_, exists, err := w.Read(item.Path)
	if err != nil {
		return "", err
	}

	// A file at the default path stays put, identical or not: reconcile
	// reports a differing applied migration instead of rewriting it.
	if exists {
		return item.Number, nil
	}

	for _, name := range existing {
		other, _, err := w.Read(name)
		if err != nil {
			return "", err
		}

		if !project.SameSQL(other, want) {
			continue
		}

		number, key, ok := project.ParseMigrationPath(name)
		if ok && key == item.Key {
			return number, nil
		}

		w.Block(Item{
			Path: name,
			Line: 1,
			Reason: "identical to framework migration " + item.Key +
				" but not named NNN_" + item.Key + ".sql; applied " +
				"migrations are never renamed",
			Action: "rename the file to <number>_" + item.Key + ".sql " +
				"if it was never applied, or keep it and remove the " +
				"capability that generates " + item.Key,
		})

		return item.Number, nil
	}

	if _, occupied := taken[item.Number]; !occupied {
		return item.Number, nil
	}

	return "", nil
}
