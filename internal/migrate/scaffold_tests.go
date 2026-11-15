package migrate

import (
	"go/parser"
	"go/token"
	"path"
	"sort"
	"strconv"
	"strings"
)

// scaffoldTests applies the test policy to pristine scaffold tests the next
// profile no longer generates. A test is never deleted silently:
//
//   - subject removed and a candidate replacement test exists: delete it and
//     note the replacement;
//   - imports (and its package) still resolve: keep it;
//   - otherwise: manual item.
//
// Edited tests never reach this function; reconcile reports them as manual.
func scaffoldTests(
	w *Workspace,
	tests []string,
	previous, next map[string][]byte,
	profile string,
) error {
	sort.Strings(tests)

	for _, name := range tests {
		directory := path.Dir(name)
		subject := strings.TrimSuffix(name, "_test.go") + ".go"

		_, subjectWasGenerated := previous[subject]
		_, subjectStays := next[subject]

		current, _, err := w.Read(subject)
		if err != nil {
			return err
		}

		subjectGone := subjectWasGenerated && !subjectStays && current == nil

		if subjectGone {
			if replacement := replacementTest(directory, name, next); replacement != "" {
				if err := w.Delete(name); err != nil {
					return err
				}

				w.Note(Item{
					Path: name,
					Line: 1,
					Reason: "pristine scaffold test removed: its subject " +
						subject + " is gone from the " + profile + " profile",
					Action: "its replacement is " + replacement,
				})

				continue
			}
		}

		resolves, err := testResolves(w, name, directory)
		if err != nil {
			return err
		}

		if resolves {
			continue
		}

		w.Manual(Item{
			Path: name,
			Line: 1,
			Reason: "pristine scaffold test cannot be kept: its subject " +
				"or imports no longer exist in the " + profile + " profile " +
				"and no replacement test is generated",
			Action: "delete the test or port its cases to a test for the " +
				"new seam, then rerun pluto migrate",
		})
	}

	return nil
}

// replacementTest finds a test the next profile generates beside a removed one.
func replacementTest(directory, name string, next map[string][]byte) string {
	candidates := []string{}

	for other := range next {
		if other != name && path.Dir(other) == directory &&
			strings.HasSuffix(other, "_test.go") {
			candidates = append(candidates, other)
		}
	}

	sort.Strings(candidates)

	if len(candidates) == 0 {
		return ""
	}

	return candidates[0]
}

// testResolves reports whether a kept test still compiles against existing
// packages: its own package keeps non-test source and every application import
// still names a package with source after the plan.
func testResolves(w *Workspace, name, directory string) (bool, error) {
	source, _, err := w.Read(name)
	if err != nil {
		return false, err
	}

	file, err := parser.ParseFile(
		token.NewFileSet(),
		name,
		source,
		parser.ImportsOnly,
	)
	if err != nil {
		return false, nil
	}

	hasSource := func(dir string) (bool, error) {
		names, err := w.List(dir)
		if err != nil {
			return false, err
		}

		for _, candidate := range names {
			if strings.HasSuffix(candidate, ".go") &&
				!strings.HasSuffix(candidate, "_test.go") {
				return true, nil
			}
		}

		return false, nil
	}

	if ok, err := hasSource(directory); err != nil || !ok {
		return false, err
	}

	module := w.Manifest.Project.Module

	for _, spec := range file.Imports {
		value, err := strconv.Unquote(spec.Path.Value)
		if err != nil || !strings.HasPrefix(value, module+"/") {
			continue
		}

		if ok, err := hasSource(strings.TrimPrefix(value, module+"/")); err != nil ||
			!ok {
			return false, err
		}
	}

	return true, nil
}
