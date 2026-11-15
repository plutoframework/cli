package sqlcheck

import "strings"

// words returns the lower-cased identifier/keyword text of tokens.
func words(tokens []token) []string {
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		out = append(out, tok.Text)
	}

	return out
}

// skipWords advances past each optional keyword in order.
func skipWords(tokens []token, at int, optional ...string) int {
	for _, word := range optional {
		if at < len(tokens) && tokens[at].is(word) {
			at++
		}
	}

	return at
}

// qualifiedName reads [schema.]name and returns the bare lower-cased name.
func qualifiedName(tokens []token, at int) (string, int) {
	if at >= len(tokens) {
		return "", at
	}

	name, _ := identName(tokens[at])

	at++
	for at+1 < len(tokens) && tokens[at].punct(".") {
		name, _ = identName(tokens[at+1])
		at += 2
	}

	return tableKey(name), at
}

func (s *migrationState) apply(file string, st []token) {
	if len(st) < 2 || st[0].Kind != tokIdent {
		return
	}

	line := st[0].Line
	switch {
	case st[0].is("create") && st[1].is("table"):
		s.applyCreateTable(file, st, line)
	case st[0].is("create") && st[1].is("policy"):
		s.applyPolicy(file, st, 2, line, true)
	case st[0].is("alter") && st[1].is("policy"):
		s.applyPolicy(file, st, 2, line, false)
	case st[0].is("alter") && st[1].is("table"):
		s.applyAlterTable(st)
	case st[0].is("drop"):
		s.applyDrop(st)
	case st[0].is("create"):
		s.applyCreateOther(file, st, line)
	}
}

func (s *migrationState) applyCreateTable(file string, st []token, line int) {
	at := skipWords(st, 2, "if", "not", "exists")

	name, at := qualifiedName(st, at)
	if name == "" || at >= len(st) || !st[at].punct("(") {
		return
	}

	state := s.create(name, file, line)

	closing := skipParens(st, at)
	for _, part := range splitTop(st[at+1 : closing-1]) {
		if len(part) == 0 {
			continue
		}

		switch part[0].Text {
		case "constraint", "primary", "unique", "check", "foreign", "like",
			"exclude":
			continue
		}

		if column, ok := identName(part[0]); ok {
			state.Columns[tableKey(column)] = true
		}
	}
}

func (s *migrationState) applyAlterTable(st []token) {
	at := skipWords(st, 2, "if", "exists", "only")
	name, at := qualifiedName(st, at)

	state := s.table(name)
	if state == nil {
		return
	}

	for _, action := range splitTop(st[at:]) {
		s.applyTableAction(name, state, words(action), action)
	}
}

func hasSeq(have []string, want ...string) bool {
	for i := 0; i+len(want) <= len(have); i++ {
		match := true

		for j, word := range want {
			if have[i+j] != word {
				match = false

				break
			}
		}

		if match {
			return true
		}
	}

	return false
}

func (s *migrationState) applyTableAction(
	name string,
	state *tableState,
	action []string,
	tokens []token,
) {
	switch {
	case hasSeq(action, "no", "force", "row", "level", "security"):
		state.Forced = false
	case hasSeq(action, "force", "row", "level", "security"):
		state.Forced = true
	case hasSeq(action, "disable", "row", "level", "security"):
		state.Enabled = false
	case hasSeq(action, "enable", "row", "level", "security"):
		state.Enabled = true
	case len(tokens) >= 3 && tokens[0].is("add") && !tokens[1].is("constraint"):
		at := skipWords(tokens, 1, "column", "if", "not", "exists")
		if column, ok := identName(tokens[at]); ok {
			state.Columns[tableKey(column)] = true
		}
	case len(tokens) >= 3 && tokens[0].is("drop") &&
		!tokens[1].is("constraint"):
		at := skipWords(tokens, 1, "column", "if", "exists")
		if column, ok := identName(tokens[at]); ok {
			delete(state.Columns, tableKey(column))
		}
	case hasSeq(action, "rename", "to"):
		delete(s.tables, name)
	case hasSeq(action, "rename", "column") ||
		len(action) >= 5 && action[0] == "rename" && action[2] == "to":
		s.renameColumn(state, tokens)
	case hasSeq(action, "trigger"):
		s.applyTriggerState(state, action, tokens)
	}
}

func (s *migrationState) renameColumn(state *tableState, tokens []token) {
	at := skipWords(tokens, 1, "column")
	if at+2 >= len(tokens) {
		return
	}

	from, _ := identName(tokens[at])
	to, _ := identName(tokens[at+2])

	delete(state.Columns, tableKey(from))

	state.Columns[tableKey(to)] = true
}

// applyTriggerState handles ENABLE/DISABLE [REPLICA|ALWAYS] TRIGGER.
func (s *migrationState) applyTriggerState(
	state *tableState,
	action []string,
	tokens []token,
) {
	disabled := action[0] == "disable" || hasSeq(action, "enable", "replica")
	at := skipWords(tokens, 1, "replica", "always")

	at = skipWords(tokens, at, "trigger")
	if at >= len(tokens) {
		return
	}

	target, _ := identName(tokens[at])

	target = tableKey(target)
	for name, trigger := range state.Triggers {
		if target == "all" || target == "user" || target == name {
			trigger.Disabled = disabled
		}
	}
}

func (s *migrationState) applyDrop(st []token) {
	switch {
	case st[1].is("table"):
		at := skipWords(st, 2, "if", "exists")
		for _, part := range splitTop(st[at:]) {
			name, _ := qualifiedName(part, 0)
			delete(s.tables, name)
		}
	case st[1].is("policy"), st[1].is("trigger"):
		at := skipWords(st, 2, "if", "exists")

		name, at := qualifiedName(st, at)
		if at >= len(st) || !st[at].is("on") {
			return
		}

		table, _ := qualifiedName(st, at+1)

		state := s.table(table)
		if state == nil {
			return
		}

		if st[1].is("policy") {
			delete(state.Policies, name)
		} else {
			delete(state.Triggers, name)
		}
	case st[1].is("function"):
		at := skipWords(st, 2, "if", "exists")
		name, _ := qualifiedName(st, at)
		delete(s.functions, name)
	}
}

func (s *migrationState) applyCreateOther(file string, st []token, line int) {
	at := skipWords(st, 1, "or", "replace", "constraint")
	if at >= len(st) {
		return
	}

	switch {
	case st[at].is("function"):
		s.applyFunction(st, at+1)
	case st[at].is("trigger"):
		s.applyTrigger(file, st, at+1, line)
	}
}

func (s *migrationState) applyFunction(st []token, at int) {
	name, at := qualifiedName(st, at)
	if name == "" {
		return
	}

	for i := at; i+1 < len(st); i++ {
		if !st[i].is("as") || st[i+1].Kind != tokString {
			continue
		}

		body := st[i+1].Text
		if strings.HasPrefix(body, "$") {
			end := strings.Index(body[1:], "$") + 2
			body = body[end : len(body)-end]
		} else {
			body = strings.Trim(body, "'")
		}

		s.functions[name] = alwaysRaises(body)

		return
	}
}

// alwaysRaises accepts a body that raises an exception and never returns a
// row, so the guarded statement cannot succeed.
func alwaysRaises(body string) bool {
	tokens, ok := tokenize(body)
	if !ok {
		return false
	}

	raises := false

	for i, tok := range tokens {
		if tok.is("raise") && i+1 < len(tokens) &&
			tokens[i+1].is("exception") {
			raises = true
		}

		if tok.is("return") && i+1 < len(tokens) &&
			(tokens[i+1].is("new") || tokens[i+1].is("old")) {
			return false
		}
	}

	return raises
}

func (s *migrationState) applyTrigger(
	file string,
	st []token,
	at int,
	line int,
) {
	name, at := qualifiedName(st, at)
	trigger := &triggerState{
		File:   file,
		Line:   line,
		Events: map[string]bool{},
	}

	trigger.Before = at < len(st) && st[at].is("before")
	for ; at < len(st) && !st[at].is("on"); at++ {
		switch st[at].Text {
		case "update", "delete", "insert", "truncate":
			trigger.Events[st[at].Text] = true
		}
	}

	table, at := qualifiedName(st, at+1)
	for ; at+1 < len(st); at++ {
		if st[at].is("execute") {
			trigger.Function, _ = qualifiedName(st, at+2)

			break
		}
	}

	if state := s.table(table); state != nil {
		state.Triggers[name] = trigger
	}
}

// applyPolicy handles CREATE POLICY and ALTER POLICY.
func (s *migrationState) applyPolicy(
	file string,
	st []token,
	at int,
	line int,
	create bool,
) {
	name, at := qualifiedName(st, at)
	if at >= len(st) || !st[at].is("on") {
		return
	}

	table, at := qualifiedName(st, at+1)

	state := s.table(table)
	if state == nil {
		return
	}

	policy := state.Policies[name]
	if create || policy == nil {
		policy = &policyState{File: file, Line: line, Command: "all"}
		state.Policies[name] = policy
	}

	lookup, isLookup := s.lookups[strings.ToLower(name)]
	if isLookup && tableKey(lookup.Table) != tableKey(table) {
		isLookup = false
	}

	for ; at < len(st); at++ {
		switch {
		case st[at].is("as") && at+1 < len(st):
			policy.Restrictive = st[at+1].is("restrictive")
		case st[at].is("for") && at+1 < len(st):
			policy.Command = st[at+1].Text
		case st[at].is("using") && at+1 < len(st) && st[at+1].punct("("):
			policy.HasUsing = true
			policy.UsingOK = s.tenantExpression(
				st[at+2:skipParens(st, at+1)-1],
				tableKey(s.columns[table]),
			)

			if isLookup {
				policy.LookupOK = settingExpression(
					st[at+2:skipParens(st, at+1)-1],
					tableKey(lookup.Key),
					strings.ToLower(lookup.Setting),
				)
			}
		case st[at].is("check") && at > 0 && st[at-1].is("with") &&
			at+1 < len(st) && st[at+1].punct("("):
			policy.HasCheck = true
			policy.CheckOK = s.tenantExpression(
				st[at+2:skipParens(st, at+1)-1],
				tableKey(s.columns[table]),
			)
		}
	}
}
