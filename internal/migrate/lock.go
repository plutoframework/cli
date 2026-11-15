package migrate

import (
	"sort"

	"github.com/plutoframework/cli/internal/project"
)

// recordLock plans the managed lock for the files as the plan leaves them.
// A managed file that matches the current render is recorded with its
// digest. Anything else keeps its previous entry, so an applied migration or
// sqlc output that intentionally differs never becomes "pristine" by being
// recorded. Entries for paths that are no longer managed are dropped.
func recordLock(w *Workspace) error {
	managed, err := project.Managed(w.Manifest)
	if err != nil {

		// Not renderable yet; the lock is recorded by a later generate.
		return nil
	}

	documents, err := project.Documents(w.Manifest)
	if err != nil {
		return err
	}

	for name, data := range documents {
		managed[name] = data
	}

	next := project.Lock{}

	names := make([]string, 0, len(managed))
	for name := range managed {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		if reconcileSkipped[name] {
			continue
		}

		current, exists, err := w.Read(name)
		if err != nil {
			return err
		}

		switch {
		case exists && sameSource(name, current, managed[name]):
			next[name] = project.SourceDigest(name, current)
		case exists && w.lock[name] != "":
			next[name] = w.lock[name]
		}
	}

	return w.Write(project.LockPath, next.Render())
}
