package hcdc

import (
	"errors"
	"strconv"
)

// decodeEscape decodes a single escape sequence beginning at s[0] == '\\',
// writes the decoded rune(s) to sb, and returns the number of bytes consumed.
// It mirrors the ESCAPED_VALUE fragment in HCDC.g4 (Go-style escapes).
func decodeEscape(s string, sb interface{ WriteRune(rune) (int, error) }) (int, error) {
	if len(s) < 2 {
		return 0, errors.New("dangling escape")
	}
	switch s[1] {
	case 'a':
		sb.WriteRune('\a')
		return 2, nil
	case 'b':
		sb.WriteRune('\b')
		return 2, nil
	case 'f':
		sb.WriteRune('\f')
		return 2, nil
	case 'n':
		sb.WriteRune('\n')
		return 2, nil
	case 'r':
		sb.WriteRune('\r')
		return 2, nil
	case 't':
		sb.WriteRune('\t')
		return 2, nil
	case 'v':
		sb.WriteRune('\v')
		return 2, nil
	case '\\':
		sb.WriteRune('\\')
		return 2, nil
	case '\'':
		sb.WriteRune('\'')
		return 2, nil
	case '"':
		sb.WriteRune('"')
		return 2, nil
	case 'x':
		return decodeHexRune(s, 2, sb)
	case 'u':
		return decodeHexRune(s, 4, sb)
	case 'U':
		return decodeHexRune(s, 8, sb)
	default:
		// Octal: exactly three octal digits.
		if isOctal(s[1]) {
			if len(s) < 4 || !isOctal(s[2]) || !isOctal(s[3]) {
				return 0, errors.New("invalid octal escape")
			}
			v, err := strconv.ParseUint(s[1:4], 8, 32)
			if err != nil {
				return 0, errors.New("invalid octal escape")
			}
			sb.WriteRune(rune(v))
			return 4, nil
		}
		return 0, errors.New("invalid escape sequence")
	}
}

// decodeHexRune reads `digits` hex digits after the 2-byte escape prefix (\x, \u, \U).
// For \x the value is a byte; for \u and \U it is a Unicode code point.
func decodeHexRune(s string, digits int, sb interface{ WriteRune(rune) (int, error) }) (int, error) {
	end := 2 + digits
	if len(s) < end {
		return 0, errors.New("truncated hex escape")
	}
	v, err := strconv.ParseUint(s[2:end], 16, 32)
	if err != nil {
		return 0, errors.New("invalid hex escape")
	}
	sb.WriteRune(rune(v))
	return end, nil
}

func isOctal(b byte) bool { return b >= '0' && b <= '7' }
