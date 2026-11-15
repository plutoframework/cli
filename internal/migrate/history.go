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
