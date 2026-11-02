package readability

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/scanner"
	"go/token"
	"sort"
	"strings"
)

type replacement struct {
	start, end int
	text       string
}

func Check(name string, source []byte) []string {
	positions := token.NewFileSet()

	tree, err := parser.ParseFile(positions, name, source, parser.ParseComments)
	if err != nil {
		return []string{err.Error()}
	}

	var found []string

	report := func(pos token.Pos, message string) {
		found = append(
			found,
			fmt.Sprintf("%s: %s", positions.Position(pos), message),
		)
	}

	lines := strings.Split(string(source), "\n")
	wide := func(pos token.Pos) bool {
		line := lines[positions.Position(pos).Line-1]
		return len(strings.ReplaceAll(line, "\t", "    ")) > 80
	}
	checkLayout := func(open, close token.Pos, elements []ast.Expr) {
		if len(elements) == 0 {
			return
		}

		previous := positions.Position(open).Line
		for _, element := range elements {
			line := positions.Position(element.Pos()).Line
			if line == previous && wide(element.Pos()) {
				report(
					element.Pos(),
					"expand wide arguments or elements onto separate lines",
				)
			}

			previous = positions.Position(element.End()).Line
		}

		if previous == positions.Position(close).Line && wide(close) {
			report(close, "place closing delimiter on a separate line")
		}
	}

	ast.Inspect(tree, func(node ast.Node) bool {
		switch value := node.(type) {
		case *ast.CallExpr:
			checkLayout(value.Lparen, value.Rparen, value.Args)
		case *ast.CompositeLit:
			checkLayout(value.Lbrace, value.Rbrace, value.Elts)
		case *ast.FuncDecl:
			if value.Body != nil &&
				len(
					value.Body.List,
				) > 0 &&
				positions.Position(
					value.Body.Lbrace,
				).Line == positions.Position(
					value.Body.Rbrace,
				).Line {
				report(value.Pos(), "expand function body onto separate lines")
			}
		case *ast.FuncLit:
			if len(
				value.Body.List,
			) > 0 && positions.Position(
				value.Body.Lbrace,
			).Line == positions.Position(
				value.Body.Rbrace,
			).Line {
				report(value.Pos(), "expand callback body onto separate lines")
			}
		case *ast.StructType:
			if len(
				value.Fields.List,
			) > 0 && positions.Position(
				value.Fields.Opening,
			).Line == positions.Position(
				value.Fields.Closing,
			).Line {
				report(value.Pos(), "expand struct fields onto separate lines")
			}
		}

		return true
	})

	// Literal data and comments have no byte-length policy. Remove their
	// contents
	// before measuring code, rather than waiving individual files or generated
	// code.
	var (
		edits []replacement
		lexer scanner.Scanner
	)

	file := positions.AddFile(name, positions.Base(), len(source))
	lexer.Init(file, source, nil, scanner.ScanComments)

	for {
		pos, kind, literal := lexer.Scan()
		if kind == token.EOF {
			break
		}

		if kind != token.STRING && kind != token.CHAR && kind != token.COMMENT {
			continue
		}

		text := ""
		if kind == token.STRING || kind == token.CHAR {
			text = "\"\""
		}

		text += strings.Repeat("\n", strings.Count(literal, "\n"))
		start := file.Offset(pos)
		edits = append(edits, replacement{start, start + len(literal), text})
	}

	code := string(source)

	sort.Slice(edits, func(i, j int) bool {
		return edits[i].start > edits[j].start
	})

	for _, edit := range edits {
		code = code[:edit.start] + edit.text + code[edit.end:]
	}

	for index, line := range strings.Split(code, "\n") {
		width := 0

		for _, r := range line {
			if r == '\t' {
				width += 4 - width%4
			} else {
				width++
			}
		}

		if width > 80 {
			found = append(
				found,
				fmt.Sprintf(
					"%s:%d: code exceeds 80 columns (%d)",
					name,
					index+1,
					width,
				),
			)
		}
	}

	return found
}
