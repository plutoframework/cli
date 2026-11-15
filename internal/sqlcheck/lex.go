package sqlcheck

import "strings"

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokQuotedIdent
	tokString
	tokNumber
	tokParam
	tokPunct
)

// token is one lexical element. Text is lower-cased for plain identifiers
// and keywords and verbatim for every other kind.
type token struct {
	Kind tokenKind
	Text string
	Line int
}

func (t token) is(text string) bool {
	return t.Kind == tokIdent && t.Text == text
}

func (t token) punct(text string) bool {
	return t.Kind == tokPunct && t.Text == text
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' ||
		c >= 0x80
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c) || c == '$'
}

// lexer is a small PostgreSQL tokenizer: comments are dropped, and string,
// quoted-identifier and dollar-quoted bodies are single tokens, so structure
// can never be hidden inside them.
type lexer struct {
	src    string
	pos    int
	line   int
	tokens []token
}

// tokenize splits PostgreSQL text. ok is false for unterminated comments,
// strings or quoted identifiers, which callers must treat as a rejection.
func tokenize(src string) (tokens []token, ok bool) {
	l := &lexer{src: src, line: 1}
	for l.pos < len(l.src) {
		if !l.next() {
			return l.tokens, false
		}
	}

	return l.tokens, true
}

func (l *lexer) emit(kind tokenKind, text string, line int) {
	l.tokens = append(l.tokens, token{Kind: kind, Text: text, Line: line})
}

func (l *lexer) next() bool {
	c := l.src[l.pos]
	switch {
	case c == '\n':
		l.line++
		l.pos++
	case c == ' ' || c == '\t' || c == '\r' || c == '\f':
		l.pos++
	case strings.HasPrefix(l.src[l.pos:], "--"):
		l.skipLineComment()
	case strings.HasPrefix(l.src[l.pos:], "/*"):
		return l.skipBlockComment()
	case c == '\'':
		return l.quoted('\'', tokString)
	case c == '"':
		return l.quoted('"', tokQuotedIdent)
	case c == '$':
		return l.dollar()
	case isDigit(c):
		l.number()
	case isIdentStart(c):
		l.word()
	default:
		l.punct()
	}

	return true
}

func (l *lexer) skipLineComment() {
	for l.pos < len(l.src) && l.src[l.pos] != '\n' {
		l.pos++
	}
}

func (l *lexer) skipBlockComment() bool {
	depth := 0
	for l.pos < len(l.src) {
		switch {
		case strings.HasPrefix(l.src[l.pos:], "/*"):
			depth++
			l.pos += 2
		case strings.HasPrefix(l.src[l.pos:], "*/"):
			depth--
			l.pos += 2
			if depth == 0 {
				return true
			}
		default:
			if l.src[l.pos] == '\n' {
				l.line++
			}
			l.pos++
		}
	}

	return false
}

// quoted reads a string or quoted identifier with doubled-quote escapes.
func (l *lexer) quoted(quote byte, kind tokenKind) bool {
	start, line := l.pos, l.line
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\n' {
			l.line++
		}
		if c == quote {
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == quote {
				l.pos += 2

				continue
			}
			l.pos++
			l.emit(kind, l.src[start:l.pos], line)

			return true
		}
		l.pos++
	}

	return false
}

// dollar reads $n parameters and $tag$ ... $tag$ quoted bodies.
func (l *lexer) dollar() bool {
	start, line := l.pos, l.line
	end := l.pos + 1
	if end < len(l.src) && isDigit(l.src[end]) {
		for end < len(l.src) && isDigit(l.src[end]) {
			end++
		}
		l.pos = end
		l.emit(tokParam, l.src[start:end], line)

		return true
	}

	for end < len(l.src) && (isIdentPart(l.src[end]) && l.src[end] != '$') {
		end++
	}
	if end >= len(l.src) || l.src[end] != '$' {
		l.pos++
		l.emit(tokPunct, "$", line)

		return true
	}

	tag := l.src[start : end+1]
	closing := strings.Index(l.src[end+1:], tag)
	if closing < 0 {
		return false
	}

	body := l.src[start : end+1+closing+len(tag)]
	l.line += strings.Count(body, "\n")
	l.pos = start + len(body)
	l.emit(tokString, body, line)

	return true
}

func (l *lexer) number() {
	start := l.pos
	for l.pos < len(l.src) &&
		(isDigit(l.src[l.pos]) || l.src[l.pos] == '.') {
		l.pos++
	}
	l.emit(tokNumber, l.src[start:l.pos], l.line)
}

func (l *lexer) word() {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	text := l.src[start:l.pos]

	// E'...' and similar prefixed strings keep their body as one token.
	if l.pos < len(l.src) && l.src[l.pos] == '\'' && len(text) == 1 &&
		strings.ContainsAny(text, "eEbBxXnNuU") {
		line := l.line
		if l.quoted('\'', tokString) {
			l.tokens[len(l.tokens)-1].Line = line
		}

		return
	}

	l.emit(tokIdent, strings.ToLower(text), l.line)
}

func (l *lexer) punct() {
	for _, op := range []string{"::", "<>", "!=", "<=", ">=", "||"} {
		if strings.HasPrefix(l.src[l.pos:], op) {
			l.emit(tokPunct, op, l.line)
			l.pos += len(op)

			return
		}
	}
	l.emit(tokPunct, string(l.src[l.pos]), l.line)
	l.pos++
}
