package hcdc

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// sexpr renders an AST as a compact s-expression for assertions.
func sexpr(n Node) string {
	switch v := n.(type) {
	case *Literal:
		switch v.Kind {
		case KString:
			return strconv.Quote(v.S)
		case KInt:
			return fmt.Sprintf("%d", v.I)
		case KFloat:
			return strconv.FormatFloat(v.F, 'g', -1, 64)
		case KBool:
			return fmt.Sprintf("%t", v.B)
		case KNull:
			return "null"
		}
	case *Column:
		return "$" + v.Name
	case *Unary:
		return fmt.Sprintf("(%s %s)", v.Op, sexpr(v.X))
	case *Binary:
		return fmt.Sprintf("(%s %s %s)", v.Op, sexpr(v.L), sexpr(v.R))
	case *Call:
		parts := make([]string, 0, len(v.Args)+2)
		parts = append(parts, "call", v.Name)
		for _, a := range v.Args {
			parts = append(parts, sexpr(a))
		}
		return "(" + strings.Join(parts, " ") + ")"
	}
	return "?"
}

func TestParseOK(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`1000`, `1000`},
		{`-5`, `-5`},
		{`-1.5e3`, `-1500`},
		{`true`, `true`},
		{`null`, `null`},
		{`$duration_ms`, `$duration_ms`},
		{`$"my col"`, `$my col`},
		{"`raw col`", `"raw col"`}, // backtick at top level is a raw string literal
		{`"a\nb"`, `"a\nb"`},
		{`GT($duration_ms, 1000)`, `(call GT $duration_ms 1000)`},
		{`$a + $b * $c`, `(+ $a (* $b $c))`},
		{`$a * $b + $c`, `(+ (* $a $b) $c)`},
		{`$x > 5 AND $y < 3`, `(AND (> $x 5) (< $y 3))`},
		{`$a OR $b AND $c`, `(OR $a (AND $b $c))`},
		{`!$flag`, `(! $flag)`},
		{`($a + $b) * $c`, `(* (+ $a $b) $c)`},
		{`IF($x > 1, "big", "small")`, `(call IF (> $x 1) "big" "small")`},
		{`CONCAT($a, $b,)`, `(call CONCAT $a $b)`}, // trailing comma
		{`COALESCE($a, IF($b, 1, 2))`, `(call COALESCE $a (call IF $b 1 2))`},
		{`if($x, 1, 2)`, `(call IF $x 1 2)`}, // case-insensitive function name
		{`AND($a, $b)`, `(call AND $a $b)`},  // AND as function name
		{`$a != $b`, `(!= $a $b)`},
		{`$a - $b - $c`, `(- (- $a $b) $c)`}, // left associative
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			n, err := Parse(tc.in)
			if err != nil {
				t.Fatalf("Parse(%q) error: %v", tc.in, err)
			}
			if got := sexpr(n); got != tc.want {
				t.Errorf("Parse(%q) = %s, want %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseStringEscapes(t *testing.T) {
	n, err := Parse(`"tab\tendA"`)
	if err != nil {
		t.Fatal(err)
	}
	lit, ok := n.(*Literal)
	if !ok || lit.Kind != KString {
		t.Fatalf("expected string literal, got %T", n)
	}
	if lit.S != "tab\tendA" {
		t.Errorf("decoded = %q, want %q", lit.S, "tab\tendA")
	}
}

func TestParseRawStringLiteral(t *testing.T) {
	n, err := Parse("`no\\escape`")
	if err != nil {
		t.Fatal(err)
	}
	lit, ok := n.(*Literal)
	if !ok || lit.Kind != KString || lit.S != `no\escape` {
		t.Fatalf("raw string mis-decoded: %+v", n)
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		``,
		`GT($a,`,
		`$a +`,
		`* $a`,
		`foo`, // bare ident, not a call
		`"unterminated`,
		`$a $b`, // two expressions
		`-$a`,   // unary minus only on numbers
	}
	for _, in := range bad {
		t.Run(in, func(t *testing.T) {
			if _, err := Parse(in); err == nil {
				t.Errorf("Parse(%q) expected error, got nil", in)
			}
		})
	}
}
