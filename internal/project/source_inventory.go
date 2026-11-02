package project

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func localImportDirectory(module, imported string) string {
	if module == "" {
		return ""
	}

	if imported == module {
		return "."
	}

	if strings.HasPrefix(imported, module+"/") {
		return strings.TrimPrefix(imported, module+"/")
	}

	return ""
}

func skipInspectionDirectory(
	relative, _ string,
	included map[string]bool,
) bool {
	if relative == "." {
		return false
	}

	for _, component := range strings.Split(relative, "/") {
		switch component {
		case ".git", "node_modules", "vendor", "testdata", "dist":
			return !included[relative]
		}
	}

	return false
}

// Include declared and explicitly imported production packages even when their
// directory names conventionally identify fixtures or build outputs. Unimported
// fixture trees remain excluded. Project code is parsed, never executed.
func inspectionDirectories(
	root, module string,
	owned map[string]bool,
) map[string]bool {
	included := map[string]bool{".": true}
	pending := []string{}
	queued := map[string]bool{}
	include := func(directory string) {
		if directory == "" || filepath.IsAbs(directory) ||
			filepath.ToSlash(filepath.Clean(directory)) != directory ||
			directory == ".." || strings.HasPrefix(directory, "../") {
			return
		}

		if !queued[directory] {
			queued[directory] = true
			pending = append(pending, directory)
		}

		for current := directory; ; current = filepath.ToSlash(
			filepath.Dir(current),
		) {
			included[current] = true
			if current == "." {
				break
			}
		}
	}

	for path := range owned {
		if strings.HasSuffix(path, ".go") {
			include(filepath.ToSlash(filepath.Dir(path)))
		}
	}

	seen := map[string]bool{}
	inspect := func(path string) {
		if seen[path] || !strings.HasSuffix(path, ".go") ||
			strings.HasSuffix(path, "_test.go") {
			return
		}

		seen[path] = true

		tree, _ := parser.ParseFile(
			token.NewFileSet(),
			path,
			nil,
			parser.ImportsOnly,
		)
		if tree == nil {
			return
		}

		for _, spec := range tree.Imports {
			imported, err := strconv.Unquote(spec.Path.Value)
			if err == nil {
				include(localImportDirectory(module, imported))
			}
		}
	}
	_ = filepath.WalkDir(root, func(
		path string,
		entry fs.DirEntry,
		err error,
	) error {
		if err != nil {
			return nil
		}

		relative, err := filepath.Rel(root, path)
		if err != nil {
			return nil
		}

		relative = filepath.ToSlash(relative)
		if entry.IsDir() {
			if skipInspectionDirectory(relative, entry.Name(), included) {
				return filepath.SkipDir
			}

			if relative != "." {
				if _, err := os.Stat(
					filepath.Join(path, "go.mod"),
				); err == nil {
					return filepath.SkipDir
				}
			}

			return nil
		}

		if entry.Type()&fs.ModeSymlink == 0 {
			inspect(path)
		}

		return nil
	})

	for index := 0; index < len(pending); index++ {
		directory := pending[index]
		path := root
		safe := true

		for _, component := range strings.Split(directory, "/") {
			path = filepath.Join(path, component)

			info, err := os.Lstat(path)
			if err != nil || info.Mode()&fs.ModeSymlink != 0 {
				safe = false
				break
			}
		}

		if !safe {
			continue
		}

		if directory != "." {
			if _, err := os.Stat(filepath.Join(path, "go.mod")); err == nil {
				continue
			}
		}

		entries, err := os.ReadDir(path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() && entry.Type()&fs.ModeSymlink == 0 {
				inspect(filepath.Join(path, entry.Name()))
			}
		}
	}

	return included
}
