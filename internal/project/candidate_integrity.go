package project

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

func candidateIntegrity(root string, m Manifest) []ArchitectureDiagnostic {
	diagnostics := []ArchitectureDiagnostic{}

	files, err := candidateManaged(m)
	if err != nil {
		return []ArchitectureDiagnostic{
			{
				ID:       "ARCH601",
				File:     "pluto.toml",
				Line:     1,
				Severity: "error",
				Message:  "candidate managed contract cannot be derived: " + err.Error(),
				Repair:   "Repair complete supported declarations before relying on candidate boundaries.",
			},
		}
	}

	paths := []string{}
	for path := range files {
		paths = append(paths, path)
	}

	sort.Strings(paths)

	for _, path := range paths {
		actual, err := os.ReadFile(
			filepath.Join(root, filepath.FromSlash(path)),
		)

		diagnostic := ArchitectureDiagnostic{
			ID:       "ARCH202",
			File:     path,
			Line:     1,
			Severity: "error",
			Message:  "candidate managed boundary differs from its declared contract",
			Repair:   "Preserve authored source separately, migrate explicitly, and regenerate the exact supported contract; do not edit managed implementation.",
		}
		if err != nil {
			diagnostic.ID = "ARCH201"
			diagnostic.File = "pluto.toml"
			diagnostic.Message = fmt.Sprintf(
				"required candidate managed boundary is absent: %s",
				path,
			)
		}

		matches := bytes.Equal(actual, files[path])
		if strings.HasSuffix(path, ".go") {
			matches = candidateSourceEquivalent(actual, files[path])
		}

		if err != nil || !matches {
			diagnostics = append(diagnostics, diagnostic)
		}
	}

	return diagnostics
}

type candidateDirective struct {
	text, prefix, following string
	before, end             int
}

// Equivalent intentionally ignores ordinary comments. Compiler directives are
// semantic input: preserve their text and attachment to the same code tokens.
func candidateSourceEquivalent(actual, expected []byte) bool {
	if !Equivalent(actual, expected) {
		return false
	}

	a, ok := candidateDirectives(actual)
	if !ok {
		return false
	}

	b, ok := candidateDirectives(expected)

	return ok && slices.Equal(a, b)
}

func candidateDirectives(source []byte) ([]candidateDirective, bool) {
	file := token.NewFileSet().AddFile("managed.go", -1, len(source))

	var scan scanner.Scanner

	valid := true

	scan.Init(
		file,
		source,
		func(token.Position, string) { valid = false },
		scanner.ScanComments,
	)

	var directives []candidateDirective

	pending := []int{}
	ordinal := 0

	for {
		position, kind, literal := scan.Scan()

		offset := file.Offset(position)
		if kind == token.EOF {
			offset = len(source)
		}

		if kind == token.COMMENT {
			if !candidateCompilerComment(literal) {
				continue
			}

			end := offset + len(literal)
			if strings.HasPrefix(literal, "//") {
				if newline := bytes.IndexByte(
					source[offset:],
					'\n',
				); newline >= 0 {
					end = offset + newline
				} else {
					end = len(source)
				}
			} else if close := bytes.Index(source[offset:], []byte("*/")); close >= 0 {
				end = offset + close + 2
			}

			lineStart := bytes.LastIndexByte(source[:offset], '\n') + 1
			directives = append(
				directives,
				candidateDirective{
					text:   literal,
					prefix: string(source[lineStart:offset]),
					before: ordinal,
					end:    end,
				},
			)
			pending = append(pending, len(directives)-1)

			continue
		}

		if kind == token.SEMICOLON || kind == token.COMMA {
			continue
		}

		for _, index := range pending {
			directives[index].following = strings.ReplaceAll(
				string(source[directives[index].end:offset]),
				"\r\n",
				"\n",
			)

			// Absolute byte positions may change with harmless formatting before
			// the directive; only its code attachment and local gap are semantic.
			directives[index].end = 0
		}

		pending = nil

		if kind == token.EOF {
			break
		}

		ordinal++
	}

	return directives, valid
}

func candidateCompilerComment(comment string) bool {
	body := strings.TrimSpace(
		strings.TrimSuffix(
			strings.TrimPrefix(strings.TrimPrefix(comment, "//"), "/*"),
			"*/",
		),
	)
	if strings.HasPrefix(body, "go:") || strings.HasPrefix(body, "+build ") ||
		strings.HasPrefix(body, "line ") {
		return true
	}

	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#cgo ") {
			return true
		}
	}

	return false
}
