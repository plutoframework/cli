package project

import (
	"bytes"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strconv"
	"strings"
)

// formatGenerated applies the configured import groups before gofmt. Managed
// source must already be canonical when formatter exclusions leave it alone.
func formatGenerated(source []byte, module string) ([]byte, error) {
	files := token.NewFileSet()

	tree, err := parser.ParseFile(
		files,
		"generated.go",
		source,
		parser.ParseComments,
	)
	if err != nil {
		return nil, err
	}

	type imported struct {
		path, text string
	}

	groups := make([][]imported, 3)
	start, end := -1, 0

	for _, declaration := range tree.Decls {
		block, ok := declaration.(*ast.GenDecl)
		if !ok || block.Tok != token.IMPORT {
			continue
		}

		if start < 0 {
			start = files.Position(block.Pos()).Offset
		}

		end = files.Position(block.End()).Offset
		for _, item := range block.Specs {
			spec := item.(*ast.ImportSpec)

			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				return nil, err
			}

			group := 2
			if !strings.Contains(strings.Split(path, "/")[0], ".") {
				group = 0
			} else if module != "" &&
				(path == module ||
					strings.HasPrefix(
						path,
						module+"/",
					)) {
				group = 1
			}

			text := spec.Path.Value
			if spec.Name != nil {
				text = spec.Name.Name + " " + text
			}

			groups[group] = append(groups[group], imported{path, text})
		}
	}

	if start >= 0 {
		var imports bytes.Buffer
		imports.WriteString("import (\n")

		previous := false

		for _, group := range groups {
			if len(group) == 0 {
				continue
			}

			if previous {
				imports.WriteByte('\n')
			}

			sort.SliceStable(
				group,
				func(
					i, j int,
				) bool {
					return group[i].path < group[j].path
				},
			)

			for _, item := range group {
				imports.WriteString(item.text)
				imports.WriteByte('\n')
			}

			previous = true
		}

		imports.WriteString(")")

		var output bytes.Buffer
		output.Write(source[:start])
		output.Write(imports.Bytes())
		output.Write(source[end:])

		source = output.Bytes()
	}

	return format.Source(source)
}
