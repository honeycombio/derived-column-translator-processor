package translate

import (
	"strings"
	"testing"

	"github.com/honeycombio/derived-column-translator-processor/pkg/hcdc"
)

// render formats a translation result for compact assertions: each branch as
// "value" or "value WHERE guard", joined by " | ".
func render(r Result) string {
	if r.Skipped {
		return "SKIP: " + r.Reason
	}
	parts := make([]string, 0, len(r.Branches))
	for _, b := range r.Branches {
		if b.Guard == "" {
			parts = append(parts, b.Value)
		} else {
			parts = append(parts, b.Value+" WHERE "+b.Guard)
		}
	}
	return strings.Join(parts, " | ")
}

func translate(t *testing.T, expr string) Result {
	t.Helper()
	node, err := hcdc.Parse(expr)
	if err != nil {
		t.Fatalf("parse %q: %v", expr, err)
	}
	return Translate("col", node, DefaultResolver)
}

func TestTranslateSupported(t *testing.T) {
	cases := []struct{ in, want string }{
		{`$x`, `attributes["x"]`},
		{`"hi"`, `"hi"`},
		{`42`, `42`},
		{`$x > 5`, `false | true WHERE attributes["x"] > 5`},
		{`$x = 5`, `false | true WHERE attributes["x"] == 5`},
		{`$a AND $b > 1`, `SKIP: ` /* $a not boolean */},
		{`CONCAT($a, "-", $b)`, `Concat([attributes["a"], "-", attributes["b"]], "")`},
		{`COALESCE($a, $b)`, `Coalesce([attributes["a"], attributes["b"]])`},
		{`INT($x)`, `Int(attributes["x"])`},
		{`FLOAT($x)`, `Double(attributes["x"])`},
		{`TO_LOWER($s)`, `ToLowerCase(attributes["s"])`},
		{`$a + $b * $c`, `(attributes["a"] + (attributes["b"] * attributes["c"]))`},
		{`STARTS_WITH($path, "/api")`, `false | true WHERE HasPrefix(attributes["path"], "/api")`},
		{`REG_MATCH($s, "^foo")`, `false | true WHERE IsMatch(attributes["s"], "^foo")`},
		{`EXISTS($x)`, `false | true WHERE attributes["x"] != nil`},
		{`IF($x > 5, "big", "small")`, `"small" | "big" WHERE attributes["x"] > 5`},
		{
			`IF($x > 5, "a", IF($y > 3, "b", "c"))`,
			`"c" | "b" WHERE attributes["y"] > 3 | "a" WHERE attributes["x"] > 5`,
		},
		{
			`SWITCH($env, "prod", 1, "dev", 2, 0)`,
			`0 | 2 WHERE attributes["env"] == "dev" | 1 WHERE attributes["env"] == "prod"`,
		},
		{
			`IN($code, 200, 201, 204)`,
			`false | true WHERE (attributes["code"] == 200 or attributes["code"] == 201 or attributes["code"] == 204)`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := render(translate(t, tc.in))
			// For the SKIP-prefixed expectation we only check the prefix.
			if strings.HasPrefix(tc.want, "SKIP:") {
				if !strings.HasPrefix(got, "SKIP:") {
					t.Errorf("translate(%q) = %q, want a SKIP", tc.in, got)
				}
				return
			}
			if got != tc.want {
				t.Errorf("translate(%q)\n got: %s\nwant: %s", tc.in, got, tc.want)
			}
		})
	}
}

func TestTranslateSkips(t *testing.T) {
	skip := []string{
		`BUCKET($x, 10)`,
		`$a % $b`,
		`RAND()`,
		`LOG10($x)`,
		`METRO_HASH($x)`,
		`IF($flag, 1, 2)`, // bare column condition is not a boolean
	}
	for _, in := range skip {
		t.Run(in, func(t *testing.T) {
			r := translate(t, in)
			if !r.Skipped {
				t.Errorf("translate(%q) should be skipped, got %s", in, render(r))
			}
		})
	}
}
