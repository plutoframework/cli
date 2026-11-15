package sqlcheck

import "fmt"

// checkTenantBinding verifies every tenant-scoped table instance is bound to
// a sqlc parameter: in the top-level WHERE conjuncts for SELECT, UPDATE and
// DELETE, and in every VALUES row for INSERT.
func checkTenantBinding(
	q query,
	stmt *statement,
	env queryEnv,
) []Diagnostic {
	fail := func(message, repair string) []Diagnostic {
		return []Diagnostic{newDiagnostic(
			q.File,
			q.Line,
			fmt.Sprintf("query %s %s", q.Name, message),
			repair,
		)}
	}

	var diagnostics []Diagnostic

	for _, ref := range stmt.Refs {
		table := env.tables[ref.Name]
		if table.TenantColumn == "" {
			continue
		}

		var problem, repair string
		if stmt.Kind == "insert" {
			problem, repair = checkInsertBinding(q, stmt, table)
		} else {
			problem, repair = checkWhereBinding(q, stmt, ref, table)
		}

		if problem != "" {
			diagnostics = append(diagnostics, fail(problem, repair)...)
		}
	}

	if stmt.Kind == "update" && len(stmt.Refs) == 1 {
		table := env.tables[stmt.Refs[0].Name]
		if table.TenantColumn != "" &&
			setsColumn(stmt.Set, table.TenantColumn) {
			diagnostics = append(diagnostics, fail(
				fmt.Sprintf(
					"assigns tenant column %s in UPDATE SET.",
					table.TenantColumn,
				),
				"Tenant ownership is immutable; remove the assignment.",
			)...)
		}
	}

	return diagnostics
}

func bindingRepair(table Table) string {
	return fmt.Sprintf(
		"Add `%s = sqlc.arg(%s)` as a top-level AND condition (not "+
			"inside OR, sqlc.narg or sqlc.slice) and pass the verified "+
			"tenant from generated code.",
		table.TenantColumn,
		table.TenantColumn,
	)
}

func checkWhereBinding(
	q query,
	stmt *statement,
	ref tableRef,
	table Table,
) (string, string) {
	if depthZero(stmt.Where, 0, func(i int) bool {
		return stmt.Where[i].is("or")
	}) >= 0 {
		return fmt.Sprintf(
				"has an OR at the top level of WHERE for %s, so the "+
					"tenant condition does not bind every branch.",
				table.Name,
			),
			"Group the OR conditions in parentheses and AND them with " +
				"the tenant condition."
	}

	for _, conjunct := range splitConjuncts(stmt.Where) {
		number, ok := tenantComparison(
			conjunct,
			ref,
			table.TenantColumn,
			len(stmt.Refs) == 1,
		)
		if !ok {
			continue
		}

		if problem := paramProblem(q, number, table); problem != "" {
			return problem, bindingRepair(table)
		}

		return "", ""
	}

	return fmt.Sprintf(
		"does not bind %s.%s to a sqlc.arg in WHERE.",
		table.Name,
		table.TenantColumn,
	), bindingRepair(table)
}

func checkInsertBinding(
	q query,
	stmt *statement,
	table Table,
) (string, string) {
	index := -1

	for i, column := range stmt.InsertColumns {
		if column == tableKey(table.TenantColumn) {
			index = i
		}
	}

	if index < 0 {
		return fmt.Sprintf(
				"inserts into %s without the tenant column %s.",
				table.Name,
				table.TenantColumn,
			),
			bindingRepair(table)
	}

	for _, row := range stmt.InsertRows {
		if index >= len(row) {
			return "has a VALUES row shorter than its column list.",
				bindingRepair(table)
		}

		number, ok := paramExpression(row[index])
		if !ok {
			return fmt.Sprintf(
					"inserts a value for %s.%s that is not a sqlc.arg.",
					table.Name,
					table.TenantColumn,
				),
				bindingRepair(table)
		}

		if problem := paramProblem(q, number, table); problem != "" {
			return problem, bindingRepair(table)
		}
	}

	return "", ""
}

// paramProblem rejects tenant parameters that are nullable or slices.
func paramProblem(q query, number int, table Table) string {
	for _, param := range q.Params {
		if param.Number != number {
			continue
		}

		if param.Slice {
			return fmt.Sprintf(
				"binds %s.%s with sqlc.slice.",
				table.Name,
				table.TenantColumn,
			)
		}

		if !param.NotNull {
			return fmt.Sprintf(
				"binds %s.%s with a nullable argument (sqlc.narg) or "+
					"a nullable column.",
				table.Name,
				table.TenantColumn,
			)
		}

		return ""
	}

	return fmt.Sprintf(
		"binds %s.%s to parameter $%d that sqlc did not report.",
		table.Name,
		table.TenantColumn,
		number,
	)
}

// splitConjuncts splits a WHERE clause with no depth-0 OR on AND, keeping
// the AND that belongs to BETWEEN inside its conjunct.
func splitConjuncts(where []token) [][]token {
	var parts [][]token

	depth, start, between := 0, 0, 0

	for i, tok := range where {
		switch {
		case tok.punct("("):
			depth++
		case tok.punct(")"):
			depth--
		case depth == 0 && tok.is("between"):
			between++
		case depth == 0 && tok.is("and"):
			if between > 0 {
				between--

				continue
			}

			parts = append(parts, where[start:i])
			start = i + 1
		}
	}

	return append(parts, where[start:])
}

// stripParens removes parentheses that wrap the whole expression.
func stripParens(tokens []token) []token {
	for len(tokens) >= 2 && tokens[0].punct("(") &&
		skipParens(tokens, 0) == len(tokens) {
		tokens = tokens[1 : len(tokens)-1]
	}

	return tokens
}

// paramExpression matches $n or $n::type and returns n.
func paramExpression(tokens []token) (int, bool) {
	if len(tokens) == 0 || tokens[0].Kind != tokParam {
		return 0, false
	}

	number := 0
	if _, err := fmt.Sscanf(tokens[0].Text, "$%d", &number); err != nil {
		return 0, false
	}

	switch len(tokens) {
	case 1:
		return number, true
	case 3:
		if tokens[1].punct("::") && tokens[2].Kind == tokIdent {
			return number, true
		}
	}

	return 0, false
}

// columnExpression matches [qualifier.]column for the tenant column of ref.
func columnExpression(
	tokens []token,
	ref tableRef,
	column string,
	single bool,
) bool {
	name := func(tok token) string {
		text, _ := identName(tok)

		return tableKey(text)
	}

	switch len(tokens) {
	case 1:
		return single && name(tokens[0]) == tableKey(column)
	case 3:
		return tokens[1].punct(".") &&
			name(tokens[0]) == ref.qualifier() &&
			name(tokens[2]) == tableKey(column)
	}

	return false
}

// tenantComparison matches `column = $n` in either order.
func tenantComparison(
	conjunct []token,
	ref tableRef,
	column string,
	single bool,
) (int, bool) {
	conjunct = stripParens(conjunct)
	for i, tok := range conjunct {
		if !tok.punct("=") {
			continue
		}

		left, right := conjunct[:i], conjunct[i+1:]
		if columnExpression(left, ref, column, single) {
			return paramExpression(right)
		}

		if columnExpression(right, ref, column, single) {
			return paramExpression(left)
		}

		return 0, false
	}

	return 0, false
}

// setsColumn reports whether an UPDATE SET clause assigns the column.
func setsColumn(set []token, column string) bool {
	for _, assignment := range splitTop(set) {
		equals := depthZero(assignment, 0, func(i int) bool {
			return assignment[i].punct("=")
		})
		if equals < 0 {
			continue
		}

		for _, tok := range assignment[:equals] {
			name, ok := identName(tok)
			if ok && tableKey(name) == tableKey(column) {
				return true
			}
		}
	}

	return false
}
