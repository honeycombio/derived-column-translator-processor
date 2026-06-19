package hcdc

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type tokenKind int

const (
	tEOF     tokenKind = iota
	tNot               // !
	tStar              // *
	tSlash             // /
	tPercent           // %
	tPlus              // +
	tMinus             // -
	tLt                // <
	tLte               // <=
	tGt                // >
	tGte               // >=
	tEq                // =
	tNeq               // !=
	tAnd               // AND
	tOr                // OR
	tLParen            // (
	tRParen            // )
	tComma             // ,
	tIdent             // function name
	tColumn            // $name
	tString            // "..." or `...` (decoded value)
	tInt               // 123
	tFloat             // 1.5
	tTrue              // true
	tFalse             // false
	tNull              // null
)

type token struct {
	kind tokenKind
	text string // decoded value for string/column, raw lexeme otherwise
	pos  int
}

type lexer struct {
	src    string
	pos    int
	tokens []token
}

// lex tokenises an HCDC expression. It returns an error on the first invalid token.
func lex(src string) ([]token, error) {
	l := &lexer{src: src}
	if err := l.run(); err != nil {
		return nil, err
	}
	return l.tokens, nil
}

func (l *lexer) run() error {
	for l.pos < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		switch {
		case unicode.IsSpace(r):
			l.pos += size
		case r == '!':
			if l.peekAt(l.pos+1) == '=' {
				l.emit(tNeq, "!=", 2)
			} else {
				l.emit(tNot, "!", 1)
			}
		case r == '*':
			l.emit(tStar, "*", 1)
		case r == '/':
			l.emit(tSlash, "/", 1)
		case r == '%':
			l.emit(tPercent, "%", 1)
		case r == '+':
			l.emit(tPlus, "+", 1)
		case r == '-':
			l.emit(tMinus, "-", 1)
		case r == '<':
			if l.peekAt(l.pos+1) == '=' {
				l.emit(tLte, "<=", 2)
			} else {
				l.emit(tLt, "<", 1)
			}
		case r == '>':
			if l.peekAt(l.pos+1) == '=' {
				l.emit(tGte, ">=", 2)
			} else {
				l.emit(tGt, ">", 1)
			}
		case r == '=':
			l.emit(tEq, "=", 1)
		case r == '(':
			l.emit(tLParen, "(", 1)
		case r == ')':
			l.emit(tRParen, ")", 1)
		case r == ',':
			l.emit(tComma, ",", 1)
		case r == '$':
			if err := l.lexColumn(); err != nil {
				return err
			}
		case r == '`':
			if err := l.lexRawString(tString); err != nil {
				return err
			}
		case r == '"':
			if err := l.lexString(tString); err != nil {
				return err
			}
		case r >= '0' && r <= '9', r == '.':
			if err := l.lexNumber(); err != nil {
				return err
			}
		case isIdentStart(r):
			l.lexIdent()
		default:
			return fmt.Errorf("unexpected character %q at position %d", r, l.pos)
		}
	}
	l.tokens = append(l.tokens, token{kind: tEOF, pos: l.pos})
	return nil
}

func (l *lexer) emit(k tokenKind, text string, n int) {
	l.tokens = append(l.tokens, token{kind: k, text: text, pos: l.pos})
	l.pos += n
}

func (l *lexer) peekAt(i int) byte {
	if i < len(l.src) {
		return l.src[i]
	}
	return 0
}

// lexColumn handles $name, $"quoted", and $`raw`.
func (l *lexer) lexColumn() error {
	start := l.pos
	l.pos++ // consume '$'
	switch l.peekAt(l.pos) {
	case '"':
		return l.lexString(tColumn)
	case '`':
		return l.lexRawString(tColumn)
	}
	nameStart := l.pos
	for l.pos < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		if !isColumnRune(r) {
			break
		}
		l.pos += size
	}
	if l.pos == nameStart {
		return fmt.Errorf("empty column reference at position %d", start)
	}
	l.tokens = append(l.tokens, token{kind: tColumn, text: l.src[nameStart:l.pos], pos: start})
	return nil
}

// lexString handles a double-quoted string with escape sequences, emitting the
// decoded value under the given token kind (tString or tColumn).
func (l *lexer) lexString(kind tokenKind) error {
	start := l.pos
	l.pos++ // consume opening quote
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case '"':
			l.pos++
			l.tokens = append(l.tokens, token{kind: kind, text: sb.String(), pos: start})
			return nil
		case '\\':
			n, err := decodeEscape(l.src[l.pos:], &sb)
			if err != nil {
				return fmt.Errorf("%w at position %d", err, l.pos)
			}
			l.pos += n
		default:
			sb.WriteByte(c)
			l.pos++
		}
	}
	return fmt.Errorf("unterminated string starting at position %d", start)
}

// lexRawString handles a backtick-quoted raw string (no escapes).
func (l *lexer) lexRawString(kind tokenKind) error {
	start := l.pos
	l.pos++ // consume opening backtick
	contentStart := l.pos
	for l.pos < len(l.src) {
		if l.src[l.pos] == '`' {
			text := l.src[contentStart:l.pos]
			l.pos++
			l.tokens = append(l.tokens, token{kind: kind, text: text, pos: start})
			return nil
		}
		l.pos++
	}
	return fmt.Errorf("unterminated raw string starting at position %d", start)
}

func (l *lexer) lexNumber() error {
	start := l.pos
	isFloat := false
	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		isFloat = true
		l.pos++
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
	}
	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		isFloat = true
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
			l.pos++
		}
	}
	text := l.src[start:l.pos]
	if text == "." {
		return fmt.Errorf("invalid number at position %d", start)
	}
	kind := tInt
	if isFloat {
		kind = tFloat
	}
	l.tokens = append(l.tokens, token{kind: kind, text: text, pos: start})
	return nil
}

func (l *lexer) lexIdent() {
	start := l.pos
	for l.pos < len(l.src) {
		r, size := utf8.DecodeRuneInString(l.src[l.pos:])
		if !isIdentPart(r) {
			break
		}
		l.pos += size
	}
	text := l.src[start:l.pos]
	kind := tIdent
	switch text {
	case "true":
		kind = tTrue
	case "false":
		kind = tFalse
	case "null":
		kind = tNull
	case "AND":
		kind = tAnd
	case "OR":
		kind = tOr
	}
	l.tokens = append(l.tokens, token{kind: kind, text: text, pos: start})
}

func isIdentStart(r rune) bool {
	return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || r >= '0' && r <= '9' || r == '_'
}

// isColumnRune matches the COLRUNE fragment from HCDC.g4.
func isColumnRune(r rune) bool {
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return true
	}
	switch r {
	case '_', '.', '/', ':', '=', '+', '?', '-':
		return true
	}
	return false
}
