package sqlcheck

import (
	"errors"
	"fmt"
)

// statement is the structure extracted from a supported statement.
type statement struct {
	Kind string
	// Refs are the table instances the statement reads or writes.
	Refs []tableRef
	// Where is the depth-0 WHERE clause, without the keyword.
	Where []token
	// InsertColumns and InsertRows describe INSERT ... VALUES.
	InsertColumns []string
	InsertRows    [][][]token
	// Set is the UPDATE SET clause, without the keyword.
	Set []token
}

var fromTerminators = map[string]bool{
	"where":     true,
	"group":     true,
	"having":    true,
	"order":     true,
	"limit":     true,
	"offset":    true,
	"for":       true,
	"window":    true,
	"fetch":     true,
	"returning": true,
}

var aliasStoppers = map[string]bool{
	"on":          true,
	"using":       true,
	"join":        true,
	"inner":       true,
	"left":        true,
	"right":       true,
	"full":        true,
	"cross":       true,
	"natural":     true,
	"outer":       true,
	"set":         true,
	"where":       true,
	"values":      true,
	"tablesample": true,
}

var errUnsupported = errors.New("unsupported construct")

func parseStatement(tokens []token) (*statement, error) {
	first := tokens[0]
	if first.Kind != tokIdent {
		return nil, fmt.Errorf("statement starts with %q", first.Text)
	}

	stmt := &statement{Kind: first.Text}
	switch first.Text {
	case "select":
		return stmt, parseSelect(stmt, tokens)
	case "insert":
		return stmt, parseInsert(stmt, tokens)
	case "update":
		return stmt, parseUpdate(stmt, tokens)
	case "delete":
		return stmt, parseDelete(stmt, tokens)
	case "with":
		return nil, errors.New("common table expressions (WITH)")
	default:
		return nil, fmt.Errorf(
			"%s statements are not allowed",
			first.Text,
		)
	}
}

// depthZero returns the index of the first depth-0 token satisfying match at
// or after from, or -1.
func depthZero(
	tokens []token,
	from int,
	match func(i int) bool,
) int {
	depth := 0
	for i := from; i < len(tokens); i++ {
		switch {
		case tokens[i].punct("("):
			depth++
		case tokens[i].punct(")"):
			depth--
		case depth == 0 && match(i):
			return i
		}
	}

	return -1
}

func isKeyword(tokens []token, i int, word string) bool {
	return i >= 0 && i < len(tokens) && tokens[i].is(word)
}

func parseSelect(stmt *statement, tokens []token) error {
	from := depthZero(tokens, 1, func(i int) bool {
		return tokens[i].is("from") && !isKeyword(tokens, i-1, "distinct")
	})
	if from < 0 {
		// A table-less select touches nothing.
		return nil
	}

	end := clauseEnd(tokens, from+1)
	refs, err := parseFromList(tokens[from+1 : end])
	if err != nil {
		return err
	}

	stmt.Refs = refs
	stmt.Where = whereClause(tokens, end)

	return nil
}

// clauseEnd returns the index of the next depth-0 clause terminator.
func clauseEnd(tokens []token, from int) int {
	end := depthZero(tokens, from, func(i int) bool {
		return tokens[i].Kind == tokIdent && fromTerminators[tokens[i].Text]
	})
	if end < 0 {
		return len(tokens)
	}

	return end
}

// whereClause extracts the WHERE tokens that start at index at, if any.
func whereClause(tokens []token, at int) []token {
	if at >= len(tokens) || !tokens[at].is("where") {
		return nil
	}

	end := depthZero(tokens, at+1, func(i int) bool {
		return tokens[i].Kind == tokIdent && fromTerminators[tokens[i].Text] &&
			tokens[i].Text != "where"
	})
	if end < 0 {
		end = len(tokens)
	}

	return tokens[at+1 : end]
}

func isJoinStart(tokens []token, i int) bool {
	tok := tokens[i]
	if tok.Kind != tokIdent {
		return false
	}

	switch tok.Text {
	case "join", "inner", "cross", "natural":
		return true
	case "left", "right", "full":
		return isKeyword(tokens, i+1, "join") ||
			isKeyword(tokens, i+1, "outer")
	}

	return false
}

// parseFromList reads table references joined by commas and JOIN keywords.
// Join conditions are skipped; tenant binding is only read from WHERE.
func parseFromList(tokens []token) ([]tableRef, error) {
	var refs []tableRef
	i := 0
	for i < len(tokens) {
		for i < len(tokens) && (isJoinStart(tokens, i) ||
			tokens[i].is("outer") || tokens[i].is("join")) {
			i++
		}
		if i < len(tokens) && tokens[i].is("lateral") {
			return nil, errors.New("LATERAL joins")
		}

		ref, next, err := parseTableRef(tokens, i)
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
		i = next

		for i < len(tokens) && !tokens[i].punct(",") &&
			!isJoinStart(tokens, i) {
			if tokens[i].punct("(") {
				i = skipParens(tokens, i)

				continue
			}
			i++
		}
		if i < len(tokens) && tokens[i].punct(",") {
			i++
		}
	}

	return refs, nil
}

func skipParens(tokens []token, i int) int {
	depth := 0
	for ; i < len(tokens); i++ {
		if tokens[i].punct("(") {
			depth++
		}
		if tokens[i].punct(")") {
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}

	return i
}

func identName(tok token) (string, bool) {
	switch tok.Kind {
	case tokIdent:
		return tok.Text, true
	case tokQuotedIdent:
		return tok.Text[1 : len(tok.Text)-1], true
	}

	return "", false
}

// parseTableRef reads [schema.]table [[AS] alias] starting at i.
func parseTableRef(tokens []token, i int) (tableRef, int, error) {
	return parseTableRefAt(tokens, i, false)
}

// parseTableRefAt allows a following parenthesis when it is a column list.
func parseTableRefAt(
	tokens []token,
	i int,
	columnList bool,
) (tableRef, int, error) {
	if i >= len(tokens) {
		return tableRef{}, i, errors.New("missing table name")
	}

	if tokens[i].punct("(") {
		return tableRef{}, i, errors.New("parenthesised FROM item")
	}

	if tokens[i].is("only") {
		i++
	}

	name, ok := identName(tokens[i])
	if !ok {
		return tableRef{}, i, fmt.Errorf(
			"unexpected %q where a table name belongs",
			tokens[i].Text,
		)
	}
	i++

	if i < len(tokens) && tokens[i].punct(".") {
		if i+1 >= len(tokens) {
			return tableRef{}, i, errors.New("dangling qualified name")
		}
		qualified, ok := identName(tokens[i+1])
		if !ok {
			return tableRef{}, i, errors.New("malformed qualified name")
		}
		name = qualified
		i += 2
	}

	if !columnList && i < len(tokens) && tokens[i].punct("(") {
		return tableRef{}, i, fmt.Errorf(
			"function or subquery %q in FROM",
			name,
		)
	}

	ref := tableRef{Name: tableKey(name)}
	if i < len(tokens) && tokens[i].is("as") {
		i++
	}
	if i < len(tokens) {
		alias, ok := identName(tokens[i])
		isStopper := tokens[i].Kind == tokIdent &&
			(aliasStoppers[tokens[i].Text] ||
				fromTerminators[tokens[i].Text])
		if ok && !isStopper {
			ref.Alias = tableKey(alias)
			i++
		}
	}

	return ref, i, nil
}

func parseUpdate(stmt *statement, tokens []token) error {
	ref, next, err := parseTableRef(tokens, 1)
	if err != nil {
		return err
	}
	stmt.Refs = []tableRef{ref}

	if next >= len(tokens) || !tokens[next].is("set") {
		return errors.New("UPDATE without SET")
	}

	end := depthZero(tokens, next+1, func(i int) bool {
		return tokens[i].is("from") || tokens[i].is("where") ||
			tokens[i].is("returning")
	})
	if end < 0 {
		end = len(tokens)
	}
	stmt.Set = tokens[next+1 : end]

	if end < len(tokens) && tokens[end].is("from") {
		return errors.New("UPDATE ... FROM")
	}

	stmt.Where = whereClause(tokens, end)

	return nil
}

func parseDelete(stmt *statement, tokens []token) error {
	if !isKeyword(tokens, 1, "from") {
		return errors.New("DELETE without FROM")
	}

	ref, next, err := parseTableRef(tokens, 2)
	if err != nil {
		return err
	}
	stmt.Refs = []tableRef{ref}

	if next < len(tokens) && tokens[next].is("using") {
		return errors.New("DELETE ... USING")
	}

	stmt.Where = whereClause(tokens, next)

	return nil
}

func parseInsert(stmt *statement, tokens []token) error {
	if !isKeyword(tokens, 1, "into") {
		return errors.New("INSERT without INTO")
	}

	ref, next, err := parseTableRefAt(tokens, 2, true)
	if err != nil {
		return err
	}
	stmt.Refs = []tableRef{ref}

	if next >= len(tokens) || !tokens[next].punct("(") {
		return errors.New("INSERT without an explicit column list")
	}

	closing := skipParens(tokens, next)
	for _, part := range splitTop(tokens[next+1 : closing-1]) {
		if len(part) != 1 {
			return errors.New("malformed INSERT column list")
		}
		name, ok := identName(part[0])
		if !ok {
			return errors.New("malformed INSERT column list")
		}
		stmt.InsertColumns = append(stmt.InsertColumns, tableKey(name))
	}

	if closing >= len(tokens) || !tokens[closing].is("values") {
		return errors.New("INSERT that is not INSERT ... VALUES")
	}

	return parseInsertRows(stmt, tokens, closing+1)
}

func parseInsertRows(stmt *statement, tokens []token, i int) error {
	for {
		if i >= len(tokens) || !tokens[i].punct("(") {
			return errors.New("malformed VALUES list")
		}

		closing := skipParens(tokens, i)
		stmt.InsertRows = append(
			stmt.InsertRows,
			splitTop(tokens[i+1:closing-1]),
		)
		i = closing

		if i < len(tokens) && tokens[i].punct(",") {
			i++

			continue
		}

		break
	}

	if i < len(tokens) && tokens[i].is("on") {
		if depthZero(tokens, i, func(j int) bool {
			return tokens[j].is("update")
		}) >= 0 {
			return errors.New("ON CONFLICT ... DO UPDATE")
		}
	}

	return nil
}

// splitTop splits tokens on depth-0 commas.
func splitTop(tokens []token) [][]token {
	var parts [][]token
	depth, start := 0, 0
	for i, tok := range tokens {
		switch {
		case tok.punct("("):
			depth++
		case tok.punct(")"):
			depth--
		case depth == 0 && tok.punct(","):
			parts = append(parts, tokens[start:i])
			start = i + 1
		}
	}

	return append(parts, tokens[start:])
}
