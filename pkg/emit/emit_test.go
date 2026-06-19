package emit

import (
	"strings"
	"testing"

	"github.com/honeycombio/derived-column-translator-processor/pkg/translate"
)

func TestGenerateDependencyOrder(t *testing.T) {
	// b depends on a (references $a). a must be emitted first.
	inputs := []Input{
		{Alias: "b", Expression: `TO_LOWER($a)`},
		{Alias: "a", Expression: `CONCAT($first, $last)`},
	}
	out := Generate(inputs, translate.DefaultResolver)

	want := []string{
		`set(attributes["a"], Concat([attributes["first"], attributes["last"]], ""))`,
		`set(attributes["b"], ToLowerCase(attributes["a"]))`,
	}
	if len(out.Statements) != len(want) {
		t.Fatalf("got %d statements, want %d: %v", len(out.Statements), len(want), out.Statements)
	}
	for i := range want {
		if out.Statements[i] != want[i] {
			t.Errorf("statement %d\n got: %s\nwant: %s", i, out.Statements[i], want[i])
		}
	}
}

func TestGenerateBooleanAndSkip(t *testing.T) {
	inputs := []Input{
		{Alias: "is_slow", Expression: `$duration_ms > 1000`},
		{Alias: "bucketed", Expression: `BUCKET($duration_ms, 100)`},
	}
	out := Generate(inputs, translate.DefaultResolver)

	wantStmts := []string{
		`set(attributes["is_slow"], false)`,
		`set(attributes["is_slow"], true) where attributes["duration_ms"] > 1000`,
	}
	if strings.Join(out.Statements, "\n") != strings.Join(wantStmts, "\n") {
		t.Errorf("statements:\n got: %v\nwant: %v", out.Statements, wantStmts)
	}

	var skipped *DCReport
	for i := range out.Reports {
		if out.Reports[i].Alias == "bucketed" {
			skipped = &out.Reports[i]
		}
	}
	if skipped == nil || !skipped.Skipped {
		t.Fatalf("expected 'bucketed' to be skipped, reports: %+v", out.Reports)
	}
}

func TestTransformConfigRender(t *testing.T) {
	out := Generate([]Input{{Alias: "svc", Expression: `$service`}}, translate.DefaultResolver)
	cfg := out.TransformConfig("derived_columns", "ignore")

	for _, want := range []string{
		"transform/derived_columns:",
		"error_mode: ignore",
		"- context: span",
		`- 'set(attributes["svc"], attributes["service"])'`,
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("config missing %q:\n%s", want, cfg)
		}
	}
}

func TestReport(t *testing.T) {
	out := Generate([]Input{
		{Alias: "ok", Expression: `$x`},
		{Alias: "bad", Expression: `RAND()`},
	}, translate.DefaultResolver)
	rep := out.Report()
	if !strings.Contains(rep, "1 translated, 1 skipped") {
		t.Errorf("report summary wrong:\n%s", rep)
	}
	if !strings.Contains(rep, "SKIP `bad`") {
		t.Errorf("report missing skip:\n%s", rep)
	}
}
