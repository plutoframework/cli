package migrate

import (
	"bytes"
	"fmt"
	"go/scanner"
	"go/token"
	"path"
	"sort"
	"strings"
)

// Paths the reconcile pass never touches: the manifest and module files are
// transformed explicitly, and OpenAPI is regenerated from the application.
var reconcileSkipped = map[string]bool{
	"pluto.toml":           true,
	"go.mod":               true,
	"go.sum":               true,
	"openapi/openapi.json": true,
}

// reconcile moves files from the previous generator output to the next one.
// A file is replaced or removed only while it is still the pristine previous
// render (token-identical, comments included); otherwise it is left alone and
// reported. Applied SQL migrations are never rewritten or removed.
func reconcile(
	w *Workspace,
	previous, next map[string][]byte,
	profile string,
) error {
	names := map[string]bool{}
	for name := range previous {
		names[name] = true
	}

	for name := range next {
		names[name] = true
	}

	ordered := make([]string, 0, len(names))
	for name := range names {
		if !reconcileSkipped[name] {
			ordered = append(ordered, name)
		}
	}

	sort.Strings(ordered)

	for _, name := range ordered {
		current, exists, err := w.Read(name)
		if err != nil {
			return err
		}

		old, inPrevious := previous[name]
		want, inNext := next[name]
		pristine := exists && inPrevious && sameSource(name, current, old)

		// Anchor reports where the user diverged from the scaffold.
		line := firstDifference(current, want)
		if inPrevious {
			line = firstDifference(current, old)
		}

		switch {
		case inNext && !exists:
			if err := w.Write(name, want); err != nil {
				return err
			}
		case inNext && sameSource(name, current, want):
		case isSQLMigration(name):
			w.Manual(Item{
				Path:   name,
				Line:   line,
				Reason: "existing SQL migration differs from the " + profile +
					" profile; applied migrations are never rewritten",
				Action: "add a new numbered migration that reaches the " +
					profile + " schema, then reconcile this file by hand",
			})
		case inNext && pristine:
			if err := w.Write(name, want); err != nil {
				return err
			}
		case inNext && isDocumentation(name):
			w.Note(Item{
				Path: name,
				Line: line,
				Reason: "documentation edited since scaffold; kept " +
					"unchanged",
				Action: "compare with the " + profile + " scaffold text " +
					"and update by hand if useful",
			})
		case inNext:
			w.Manual(Item{
				Path:   name,
				Line:   line,
				Reason: "modified since scaffold; the " + profile +
					" profile generates this path",
				Action: "move your changes out of this file (or port them " +
					"to the " + profile + " seams), restore the scaffold " +
					"text and rerun pluto migrate",
			})
		case !exists:
		case pristine:
			if err := w.Delete(name); err != nil {
				return err
			}
		case isDocumentation(name):
			w.Note(Item{
				Path: name,
				Line: 1,
				Reason: "edited published documentation has no " +
					profile + " counterpart; kept unchanged",
				Action: "review for statements that no longer apply",
			})
		default:
			w.Manual(Item{
				Path:   name,
				Line:   line,
				Reason: "modified published scaffold file has no " +
					profile + " counterpart",
				Action: "port the changes to the " + profile + " seams, " +
					"then delete the file",
			})
		}
	}

	return nil
}

func isSQLMigration(name string) bool {
	return strings.HasPrefix(name, "db/migrations/") &&
		strings.HasSuffix(name, ".sql")
}

func isDocumentation(name string) bool {
	return name == "README.md" || name == "AGENTS.md" ||
		strings.HasPrefix(name, "docs/")
}

// sameSource compares Go by tokens and comment text, so formatter-only edits
// stay pristine while any semantic or comment change counts as authored.
func sameSource(name string, a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}

	if path.Ext(name) != ".go" {
		return false
	}

	left, ok := goTokens(a)
	if !ok {
		return false
	}

	right, ok := goTokens(b)

	return ok && left == right
}

// goTokens lists tokens with comment text. Automatic semicolons and trailing
// commas before a closing bracket are dropped: formatters add or remove them.
func goTokens(source []byte) (string, bool) {
	files := token.NewFileSet()
	failed := false

	var lexer scanner.Scanner

	lexer.Init(
		files.AddFile("", files.Base(), len(source)),
		source,
		func(token.Position, string) { failed = true },
		scanner.ScanComments,
	)

	type item struct {
		tok     token.Token
		literal string
	}

	items := []item{}

	for {
		_, tok, literal := lexer.Scan()
		if tok == token.EOF {
			break
		}

		if tok == token.SEMICOLON && literal == "\n" {
			continue
		}

		if tok == token.COMMENT {
			literal = strings.TrimSpace(literal)
		}

		items = append(items, item{tok, literal})
	}

	var result strings.Builder

	for i, current := range items {
		if current.tok == token.COMMA && i+1 < len(items) {
			switch items[i+1].tok {
			case token.RPAREN, token.RBRACE, token.RBRACK:
				continue
			}
		}

		fmt.Fprintf(&result, "%d:%q;", current.tok, current.literal)
	}

	return result.String(), !failed
}

func firstDifference(a, b []byte) int {
	left := bytes.Split(a, []byte("\n"))
	right := bytes.Split(b, []byte("\n"))

	for i := range left {
		if i >= len(right) || !bytes.Equal(left[i], right[i]) {
			return i + 1
		}
	}

	if len(right) > len(left) {
		return len(left)
	}

	return 1
}
