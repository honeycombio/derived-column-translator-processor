// Package emit turns a set of Honeycomb derived columns into a transform
// processor configuration plus a human-readable translation report.
package emit

import (
	"fmt"
	"sort"
	"strings"

	"github.com/honeycombio/derived-column-translator-processor/pkg/hcdc"
	"github.com/honeycombio/derived-column-translator-processor/pkg/translate"
)

// Input is one derived column to translate.
type Input struct {
	Alias      string
	Expression string
}

// DCReport records the outcome of translating one derived column.
type DCReport struct {
	Alias      string
	Expression string
	Statements []string // OTTL statements, empty when skipped
	Skipped    bool
	Reason     string // populated when Skipped
}

// Output is the result of generating config for a batch of derived columns.
type Output struct {
	// Statements is the ordered, flat list of OTTL statements (dependencies first).
	Statements []string
	// Reports is one entry per input, in dependency order.
	Reports []DCReport
}

// Generate parses, translates, dependency-orders, and emits OTTL for the given
// derived columns. It never fails on an individual untranslatable column; that
// column is reported as skipped instead.
func Generate(inputs []Input, resolve translate.Resolver) Output {
	// Parse everything first; collect parse failures as skips.
	type parsed struct {
		in   Input
		node hcdc.Node
		err  error
	}
	items := make([]parsed, len(inputs))
	aliases := make(map[string]bool, len(inputs))
	for _, in := range inputs {
		aliases[in.Alias] = true
	}
	for i, in := range inputs {
		node, err := hcdc.Parse(in.Expression)
		items[i] = parsed{in: in, node: node, err: err}
	}

	// Dependency edges: alias -> referenced aliases that are also derived columns.
	deps := make(map[string][]string, len(inputs))
	for _, it := range items {
		if it.err != nil {
			continue
		}
		for _, col := range hcdc.Columns(it.node) {
			if col != it.in.Alias && aliases[col] {
				deps[it.in.Alias] = append(deps[it.in.Alias], col)
			}
		}
	}

	order := topoSort(inputs, deps)
	byAlias := make(map[string]parsed, len(items))
	for _, it := range items {
		byAlias[it.in.Alias] = it
	}

	var out Output
	for _, alias := range order {
		it := byAlias[alias]
		report := DCReport{Alias: it.in.Alias, Expression: it.in.Expression}

		if it.err != nil {
			report.Skipped = true
			report.Reason = "parse error: " + it.err.Error()
			out.Reports = append(out.Reports, report)
			continue
		}

		res := translate.Translate(it.in.Alias, it.node, resolve)
		if res.Skipped {
			report.Skipped = true
			report.Reason = res.Reason
			out.Reports = append(out.Reports, report)
			continue
		}

		stmts := statements(it.in.Alias, res.Branches)
		report.Statements = stmts
		out.Statements = append(out.Statements, stmts...)
		out.Reports = append(out.Reports, report)
	}
	return out
}

// statements renders one derived column's branches as OTTL set() statements,
// targeting a span attribute named after the alias.
func statements(alias string, branches []translate.Branch) []string {
	target := fmt.Sprintf("attributes[%q]", alias)
	stmts := make([]string, 0, len(branches))
	for _, b := range branches {
		stmt := fmt.Sprintf("set(%s, %s)", target, b.Value)
		if b.Guard != "" {
			stmt += " where " + b.Guard
		}
		stmts = append(stmts, stmt)
	}
	return stmts
}

// topoSort orders aliases so that dependencies come before dependents. Inputs
// retain their original order where dependencies don't force otherwise. Aliases
// involved in a cycle are appended in input order (a cycle should not occur:
// Honeycomb rejects circular derived columns).
func topoSort(inputs []Input, deps map[string][]string) []string {
	const (
		unvisited = 0
		active    = 1
		done      = 2
	)
	state := make(map[string]int, len(inputs))
	var order []string

	var visit func(alias string)
	visit = func(alias string) {
		switch state[alias] {
		case done, active:
			return // already placed, or cycle: break it
		}
		state[alias] = active
		dl := append([]string(nil), deps[alias]...)
		sort.Strings(dl) // deterministic
		for _, d := range dl {
			visit(d)
		}
		state[alias] = done
		order = append(order, alias)
	}

	for _, in := range inputs {
		visit(in.Alias)
	}
	return order
}

// TransformConfig renders the OTTL statements as a transformprocessor config
// block. name is the component name suffix (e.g. "derived_columns").
func (o Output) TransformConfig(name, errorMode string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "processors:\n")
	fmt.Fprintf(&sb, "  transform/%s:\n", name)
	fmt.Fprintf(&sb, "    error_mode: %s\n", errorMode)
	fmt.Fprintf(&sb, "    trace_statements:\n")
	fmt.Fprintf(&sb, "      - context: span\n")
	fmt.Fprintf(&sb, "        statements:\n")
	if len(o.Statements) == 0 {
		fmt.Fprintf(&sb, "          [] # no derived columns could be translated\n")
		return sb.String()
	}
	for _, s := range o.Statements {
		fmt.Fprintf(&sb, "          - %s\n", yamlQuote(s))
	}
	return sb.String()
}

// Report renders a markdown summary of what was and was not translated.
func (o Output) Report() string {
	var sb strings.Builder
	var ok, skipped int
	for _, r := range o.Reports {
		if r.Skipped {
			skipped++
		} else {
			ok++
		}
	}
	fmt.Fprintf(&sb, "# Derived column translation report\n\n")
	fmt.Fprintf(&sb, "%d translated, %d skipped.\n\n", ok, skipped)
	for _, r := range o.Reports {
		if r.Skipped {
			fmt.Fprintf(&sb, "- SKIP `%s`: %s\n", r.Alias, r.Reason)
		} else {
			fmt.Fprintf(&sb, "- OK   `%s` (%d statement(s))\n", r.Alias, len(r.Statements))
		}
	}
	return sb.String()
}

// yamlQuote wraps an OTTL statement in single quotes for YAML, escaping any
// embedded single quotes by doubling them (YAML single-quote escaping).
func yamlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
