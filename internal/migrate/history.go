package migrate

import (
	"embed"
	"io/fs"
	"sort"
	"strings"

	"github.com/plutoframework/cli/internal/project"
)

// historyFS holds scaffold files as earlier generators rendered them, one
// directory per generator era (named by date, oldest first). Files are stored
// with a .txt suffix so Go tooling ignores them, and with @@MODULE@@ and
// @@NAME@@ in place of the project module and name. Only files whose text
// later changed are recorded; unchanged files match the current render.
//
//go:embed history
var historyFS embed.FS

// historicalRenders returns every recorded earlier scaffold of a path for the
// manifest, newest era first.
func historicalRenders(m project.Manifest, name string) [][]byte {
	eras, err := fs.ReadDir(historyFS, "history")
	if err != nil {
		return nil
	}

	names := []string{}
	for _, era := range eras {
		names = append(names, era.Name())
	}

	sort.Sort(sort.Reverse(sort.StringSlice(names)))

	renders := [][]byte{}

	for _, era := range names {
		data, err := historyFS.ReadFile("history/" + era + "/" + name + ".txt")
		if err != nil {
			continue
		}

		text := strings.ReplaceAll(string(data), "@@MODULE@@", m.Project.Module)
		text = strings.ReplaceAll(text, "@@NAME@@", m.Project.Name)
		renders = append(renders, []byte(text))
	}

	return renders
}

// historicalDocuments renders the document templates recorded under
// history/docs, one set per era, for the manifest. Era directories hold
// README.md.tmpl.txt and AGENTS.md.tmpl.txt as earlier CLIs embedded them.
// Record a new era whenever those templates change.
func historicalDocuments(m project.Manifest) ([]map[string][]byte, error) {
	eras, err := fs.ReadDir(historyFS, "history/docs")
	if err != nil {
		return nil, nil
	}

	renders := []map[string][]byte{}

	for _, era := range eras {
		sources := map[string][]byte{}

		for _, name := range []string{"README.md", "AGENTS.md"} {
			data, err := historyFS.ReadFile(
				"history/docs/" + era.Name() + "/" + name + ".tmpl.txt",
			)
			if err == nil {
				sources[name] = data
			}
		}

		rendered, err := project.DocumentsFromTemplates(m, sources)
		if err != nil {
			return nil, err
		}

		renders = append(renders, rendered)
	}

	return renders, nil
}
