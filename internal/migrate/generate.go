package migrate

import (
	"bytes"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/plutoframework/cli/internal/project"
)

// Generate plans the rewrite of every CLI-owned file from pluto.toml with
// the installed CLI. A managed file is replaced only while it is pristine
// relative to a render this CLI knows: the render of an earlier generator
// recorded in the migrate history. Drift of that kind is a CLI upgrade, not
// an edit. Any other difference is a hand edit; it is reported with its
// first differing line and blocks the whole plan. Required proposals (table
// RLS migrations, the runtime role migration) are added with framework
// numbering. Authored adapter stubs are created only while missing.
// Nothing is written; Write applies the plan.
func Generate(root string) (*Plan, error) {
	return generate(root, false)
}

// Adopt plans generate for an application that has no managed lock, treating
// every managed file as pristine so one run can bring apps scaffolded by an
// unrecorded CLI version onto the lock. Hand edits to managed files are
// overwritten too; the caller must show the plan diff first, and the clean
// Git tree makes the result reviewable and reversible.
func Adopt(root string) (*Plan, error) {
	return generate(root, true)
}

func generate(root string, adopt bool) (*Plan, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	_, manifest, err := project.Discover(root)
	if err != nil {
		return nil, err
	}

	state := State{
		Schema:  manifest.SchemaVersion,
		Profile: manifest.Boundary.Mode,
	}
	if state.Profile == "" {
		state.Profile = ProfilePublished
	}

	plan := &Plan{
		From:     state,
		To:       state,
		Root:     root,
		Writes:   map[string][]byte{},
		Deletes:  map[string]bool{},
		Original: map[string][]byte{},
		Manifest: manifest,
	}

	lock, locked, err := project.ReadLock(root)
	if err != nil {
		return nil, err
	}

	if adopt && locked {
		return nil, fmt.Errorf(
			"%s exists; --adopt is only for applications without a "+
				"managed lock (use pluto generate)",
			project.LockPath,
		)
	}

	workspace := &Workspace{
		Root:          root,
		Manifest:      manifest,
		plan:          plan,
		lock:          lock,
		adopt:         adopt,
		pristineTests: map[string]bool{},
	}

	managed, err := project.Managed(manifest)
	if err != nil {
		return nil, err
	}

	if err := regenerateManaged(workspace, managed); err != nil {
		return nil, err
	}

	if err := refreshDocuments(workspace, manifest); err != nil {
		return nil, err
	}

	if err := proposeRequired(workspace, manifest); err != nil {
		return nil, err
	}

	plan.Blocked = len(plan.Manual) > 0

	if !plan.Blocked {
		if err := recordLock(workspace); err != nil {
			return nil, err
		}
	}

	sortItems(plan.Manual)
	sortItems(plan.Notes)

	return plan, nil
}

func regenerateManaged(w *Workspace, managed map[string][]byte) error {
	names := make([]string, 0, len(managed))

	for name := range managed {
		if !reconcileSkipped[name] {
			names = append(names, name)
		}
	}

	sort.Strings(names)

	for _, name := range names {
		want := managed[name]

		current, exists, err := w.Read(name)
		if err != nil {
			return err
		}

		if !exists {
			if err := w.Write(name, want); err != nil {
				return err
			}

			continue
		}

		if sameSource(name, current, want) {
			continue
		}

		if isSQLCOutput(current) {
			w.Note(Item{
				Path: name,
				Line: 1,
				Reason: "sqlc output kept unchanged; it is regenerated " +
					"by sqlc, not by pluto generate",
				Action: "run sqlc generate once migrations and queries " +
					"are in place",
			})

			continue
		}

		known := historicalRenders(w.Manifest, name)

		pristine := w.adopt || w.lock.Matches(name, current)

		for _, base := range known {
			pristine = pristine || sameSource(name, current, base)
		}

		switch {
		case isSQLMigration(name) && pristine:
			w.Note(Item{
				Path: name,
				Line: firstDifference(current, want),
				Reason: "applied migration differs from the current " +
					"render; applied migrations are never rewritten",
				Action: "add a new numbered migration if the schema " +
					"change is required",
			})
		case pristine:
			if err := w.Write(name, want); err != nil {
				return err
			}
		default:
			w.Manual(handEdit(name, current, want, len(w.lock) == 0))
		}
	}

	return nil
}

// handEdit explains a managed file that matches no known render.
func handEdit(name string, current, want []byte, noLock bool) Item {
	line := firstDifference(current, want)
	have := lineText(current, line)
	expect := lineText(want, line)

	action := "move the change into pluto.toml or an authored file, " +
		"restore this file (git checkout) and rerun pluto generate"
	if noLock {
		action += "; if it was rendered by an older pluto, run " +
			"pluto generate --adopt --dry-run, review, then --adopt"
	}

	return Item{
		Path: name,
		Line: line,
		Reason: "managed file was edited by hand; managed files are " +
			"CLI-owned\n    - " + have + "\n    + " + expect,
		Action: action,
	}
}

func lineText(data []byte, line int) string {
	lines := strings.Split(string(data), "\n")
	if line < 1 || line > len(lines) {
		return "<end of file>"
	}

	return lines[line-1]
}

// proposeRequired adds migrations the manifest now needs and authored
// adapter stubs that are missing. Existing authored files stay untouched.
func proposeRequired(w *Workspace, m project.Manifest) error {
	if m.Boundary.Mode != ProfileCandidate {
		return nil
	}

	proposals, err := project.ProposeTableMigrations(w.Root, m)
	if err != nil {
		w.Manual(Item{
			Path:   "pluto.toml",
			Line:   1,
			Reason: err.Error(),
			Action: "create the table in a migration, then rerun " +
				"pluto generate",
		})

		return nil
	}

	role, err := project.ProposeRuntimeRoleMigration(w.Root, m, proposals)
	if err != nil {
		return fmt.Errorf("runtime role migration: %w", err)
	}

	stubs, err := project.AuthoredAdapterStubs(m)
	if err != nil {
		return err
	}

	for _, group := range []map[string][]byte{proposals, role, stubs} {
		for name, source := range group {
			if _, exists, err := w.Read(name); err != nil {
				return err
			} else if exists {
				continue
			}

			if err := w.Write(name, source); err != nil {
				return err
			}
		}
	}

	return nil
}

// refreshDocuments updates README.md and AGENTS.md to the current templates
// while they are still pristine: identical to the current render or to a
// render of a template recorded from an earlier CLI. Edited documents are
// kept and reported as a note; documentation never blocks generation.
func refreshDocuments(w *Workspace, m project.Manifest) error {
	want, err := project.Documents(m)
	if err != nil {
		return err
	}

	older, err := historicalDocuments(m)
	if err != nil {
		return err
	}

	for _, name := range []string{"AGENTS.md", "README.md"} {
		current, exists, err := w.Read(name)
		if err != nil {
			return err
		}

		if !exists {
			continue
		}

		if sameSource(name, current, want[name]) {
			continue
		}

		pristine := w.adopt || w.lock.Matches(name, current)

		for _, render := range older {
			pristine = pristine || bytes.Equal(current, render[name])
		}

		if pristine {
			if err := w.Write(name, want[name]); err != nil {
				return err
			}

			continue
		}

		w.Note(Item{
			Path: name,
			Line: firstDifference(current, want[name]),
			Reason: "documentation edited or from an unrecorded older " +
				"CLI; kept unchanged",
			Action: "compare with the current template text (authored " +
				"seams, pluto generate) and update by hand",
		})
	}

	return nil
}
