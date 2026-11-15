package sqlcheck

import (
	"fmt"
	"strings"
)

// queryParam is the sqlc view of one query parameter.
type queryParam struct {
	Number  int
	NotNull bool
	Slice   bool
}

// query is one sqlc query as captured from the plugin request. Text is the
// sqlc-normalised statement: macros are resolved and parameters are $n.
type query struct {
	Name   string
	File   string
	Line   int
	Text   string
	Params []queryParam
	// Tables are the tables sqlc resolved for columns and parameters.
	Tables []string
}

// tableRef is one table instance in a statement.
type tableRef struct {
	Name  string
	Alias string
}

func (r tableRef) qualifier() string {
	if r.Alias != "" {
		return r.Alias
	}

	return r.Name
}

// queryEnv is what a query is judged against.
type queryEnv struct {
	tables  map[string]Table
	allowed map[string]bool
	set     string
}

// bannedWords are statements and functions that make SQL dynamic or let a
// query change or leave the tenant context.
var bannedWords = map[string]bool{
	"execute":                       true,
	"prepare":                       true,
	"copy":                          true,
	"call":                          true,
	"set_config":                    true,
	"dblink":                        true,
	"dblink_exec":                   true,
	"query_to_xml":                  true,
	"query_to_xml_and_xmlschema":    true,
	"query_to_json":                 true,
	"cursor_to_xml":                 true,
	"table_to_xml":                  true,
	"database_to_xml":               true,
	"schema_to_xml":                 true,
	"pg_read_file":                  true,
	"pg_read_binary_file":           true,
	"pg_ls_dir":                     true,
	"lo_import":                     true,
	"lo_export":                     true,
	"pg_terminate_backend":          true,
	"pg_cancel_backend":             true,
	"pg_reload_conf":                true,
	"pg_switch_wal":                 true,
	"pg_create_restore_point":       true,
	"database_to_xml_and_xmlschema": true,
}

var subqueryStarters = map[string]bool{
	"select": true,
	"with":   true,
	"values": true,
	"table":  true,
}

var setOperators = map[string]bool{
	"union":     true,
	"intersect": true,
	"except":    true,
}

// analyzeQuery checks one query and returns its diagnostics.
func analyzeQuery(q query, env queryEnv) []Diagnostic {
	reject := func(message, repair string) []Diagnostic {
		return []Diagnostic{
			newDiagnostic(
				q.File,
				q.Line,
				fmt.Sprintf("query %s %s", q.Name, message),
				repair,
			),
		}
	}

	tokens, ok := tokenize(q.Text)
	if !ok {
		return reject(
			"contains an unterminated string, identifier or comment.",
			"Fix the SQL so it tokenizes cleanly.",
		)
	}

	tokens = trimTrailingSemicolon(tokens)
	if len(tokens) == 0 {
		return reject("is empty.", "Write exactly one statement.")
	}

	if message, repair := rejectDynamic(tokens); message != "" {
		return reject(message, repair)
	}

	stmt, err := parseStatement(tokens)
	if err != nil {
		return reject("is outside the supported SQL subset: "+
			err.Error(), "Rewrite it as a plain single-table-"+
			"scope SELECT, INSERT, UPDATE or DELETE with no CTE, "+
			"subquery or set operation.")
	}

	var diagnostics []Diagnostic
	diagnostics = append(diagnostics, checkDeclared(q, stmt, env)...)
	if len(diagnostics) > 0 {
		return diagnostics
	}

	return append(diagnostics, checkTenantBinding(q, stmt, env)...)
}

func trimTrailingSemicolon(tokens []token) []token {
	for len(tokens) > 0 && tokens[len(tokens)-1].punct(";") {
		tokens = tokens[:len(tokens)-1]
	}

	return tokens
}

// rejectDynamic returns a message for constructs that cannot be reasoned
// about statically.
func rejectDynamic(tokens []token) (string, string) {
	if tokens[0].is("with") {
		return "uses a common table expression (WITH).",
			"Replace the CTE with a join or separate tenant-bound " +
				"queries."
	}

	for i, tok := range tokens {
		if tok.punct(";") {
			return "contains more than one statement.",
				"Write exactly one statement per sqlc query."
		}

		if tok.Kind != tokIdent {
			continue
		}

		if bannedWords[tok.Text] {
			return "uses " + tok.Text + ", which makes SQL dynamic " +
					"or escapes the tenant context.",
				"Express the behaviour as a static declared-table " +
					"query."
		}

		if setOperators[tok.Text] {
			return "uses " + strings.ToUpper(tok.Text) + ".",
				"Run one tenant-bound query per table and combine " +
					"results in authored Go."
		}

		if i > 0 && tokens[i-1].punct("(") && subqueryStarters[tok.Text] {
			return "contains a subquery.",
				"Replace the subquery with a join or a second " +
					"tenant-bound query."
		}
	}

	return "", ""
}

func tableKey(name string) string {
	return strings.ToLower(name)
}

// checkDeclared enforces that every touched table is declared and belongs to
// the query set.
func checkDeclared(q query, stmt *statement, env queryEnv) []Diagnostic {
	seen := map[string]bool{}
	var names []string
	for _, ref := range stmt.Refs {
		if !seen[ref.Name] {
			seen[ref.Name] = true
			names = append(names, ref.Name)
		}
	}
	for _, name := range q.Tables {
		key := tableKey(name)
		if key != "" && !seen[key] {
			seen[key] = true
			names = append(names, key)
		}
	}

	var diagnostics []Diagnostic
	for _, name := range names {
		if _, declared := env.tables[name]; !declared {
			diagnostics = append(diagnostics, newDiagnostic(
				q.File,
				q.Line,
				fmt.Sprintf(
					"query %s touches table %q, which is not declared.",
					q.Name,
					name,
				),
				"Declare the table in [[tables]] with its tenant_column "+
					"or stop querying it.",
			))

			continue
		}

		if !env.allowed[name] {
			diagnostics = append(diagnostics, newDiagnostic(
				q.File,
				q.Line,
				fmt.Sprintf(
					"query %s touches table %q, which query set %q "+
						"does not list.",
					q.Name,
					name,
					env.set,
				),
				"Add the table to the [[queries]] tables list or move "+
					"the query to the owning group.",
			))
		}
	}

	return diagnostics
}
