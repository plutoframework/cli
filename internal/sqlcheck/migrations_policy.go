package sqlcheck

// tenantExpression reports whether a policy expression is exactly
// `<column> = current_setting('<setting>'[, true|false])`, in either order,
// ignoring parentheses, `::type` casts and a scalar SELECT wrapper.
func (s *migrationState) tenantExpression(tokens []token, column string) bool {
	return settingExpression(tokens, column, s.setting)
}

// settingExpression is tenantExpression for an arbitrary setting name.
func settingExpression(tokens []token, column, setting string) bool {
	if column == "" {
		return false
	}

	var flat []token

	for i := 0; i < len(tokens); i++ {
		switch {
		case tokens[i].punct("("), tokens[i].punct(")"), tokens[i].is("select"):
			continue
		case tokens[i].punct("::"):

			// Skip the cast target, including a schema-qualified name.
			i++
			for i+1 < len(tokens) && tokens[i+1].punct(".") {
				i += 2
			}

			continue
		}

		flat = append(flat, tokens[i])
	}

	for i, tok := range flat {
		if !tok.punct("=") {
			continue
		}

		left, right := flat[:i], flat[i+1:]

		return columnOnly(left, column) && settingCall(right, setting) ||
			columnOnly(right, column) && settingCall(left, setting)
	}

	return false
}

func columnOnly(tokens []token, column string) bool {
	switch len(tokens) {
	case 1:
		name, ok := identName(tokens[0])

		return ok && tableKey(name) == column
	case 3:
		name, ok := identName(tokens[2])

		return tokens[1].punct(".") && ok && tableKey(name) == column
	}

	return false
}

// settingCall matches current_setting('<setting>'[, true|false]).
func settingCall(tokens []token, setting string) bool {
	if len(tokens) < 2 || !tokens[0].is("current_setting") ||
		tokens[1].Kind != tokString {
		return false
	}

	name := tokens[1].Text
	if len(name) < 2 || name[0] != '\'' ||
		tableKey(name[1:len(name)-1]) != setting {
		return false
	}

	switch len(tokens) {
	case 2:
		return true
	case 4:
		return tokens[2].punct(",") &&
			(tokens[3].is("true") || tokens[3].is("false"))
	}

	return false
}

// switchExpression reports whether a policy expression is exactly
// `current_setting('<setting>'[, true|false]) = 'on'`, in either order,
// ignoring parentheses and casts: the tenant index switch.
func switchExpression(tokens []token, setting string) bool {
	var flat []token

	for i := 0; i < len(tokens); i++ {
		switch {
		case tokens[i].punct("("), tokens[i].punct(")"):
			continue
		case tokens[i].punct("::"):
			i++
			for i+1 < len(tokens) && tokens[i+1].punct(".") {
				i += 2
			}

			continue
		}

		flat = append(flat, tokens[i])
	}

	for i, tok := range flat {
		if !tok.punct("=") {
			continue
		}

		left, right := flat[:i], flat[i+1:]

		return settingCall(left, setting) && onLiteral(right) ||
			settingCall(right, setting) && onLiteral(left)
	}

	return false
}

func onLiteral(tokens []token) bool {
	return len(tokens) == 1 && tokens[0].Kind == tokString &&
		tokens[0].Text == "'on'"
}
