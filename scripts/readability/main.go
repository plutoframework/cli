// Command readability checks layout that the configured formatter leaves
// intact.
package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/plutoframework/cli/internal/readability"
)

func fixtureSources(path string, source []byte) ([][]byte, error) {
	if !strings.HasSuffix(path, ".tmpl") {
		return [][]byte{source}, nil
	}

	text := string(source)

	interpreted := strings.Contains(text, "{{.Module}}") ||
		strings.Contains(
			text,
			"{{.Mode}}",
		) || strings.Contains(text, "{{if .Multi}}")
	if _, err := parser.ParseFile(
		token.NewFileSet(),
		path,
		source,
		0,
	); err == nil &&
		!interpreted {
		return [][]byte{source}, nil
	}

	// Generator branches have an explicit InitialFiles profile audit in
	// project.
	if strings.Contains(filepath.ToSlash(path), "internal/project/templates/") {
		return nil, nil
	}

	parsed, err := template.New(path).Parse(text)
	if err != nil {
		return nil, err
	}

	var sources [][]byte

	for _, multi := range []bool{false, true} {
		for _, mode := range []string{"none", "memory", "valkey"} {
			var output bytes.Buffer

			data := map[string]any{
				"Module": "example.test/readable",
				"Multi":  multi,
				"Mode":   mode,
			}
			if err := parsed.Execute(&output, data); err != nil {
				return nil, err
			}

			sources = append(sources, output.Bytes())
		}
	}

	return sources, nil
}

func main() {
	root := "."
	if len(os.Args) > 1 {
		root = os.Args[1]
	}

	failed := false

	err := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}

			if entry.IsDir() {
				switch entry.Name() {
				case ".git",
					"vendor",
					"node_modules":
					return filepath.SkipDir
				}

				return nil
			}

			if !strings.HasSuffix(path, ".go") &&
				!strings.HasSuffix(path, ".go.tmpl") {
				return nil
			}

			source, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			sources, err := fixtureSources(path, source)
			if err != nil {
				return err
			}

			for _, rendered := range sources {
				for _, message := range readability.Check(path, rendered) {
					fmt.Fprintln(os.Stderr, message)

					failed = true
				}
			}

			return nil
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)

		failed = true
	}

	if failed {
		os.Exit(1)
	}
}
