// Package translate converts a parsed Honeycomb derived-column AST into OTTL.
//
// The central problem: Honeycomb derived columns are value-producing
// expressions (a column can evaluate to a conditionally-selected value, or to a
// boolean), but OTTL's set() takes a plain value and expresses conditionals only
// as `where` clauses on statements. We bridge that by normalising every derived
// column into an ordered list of (guard, value) Branches. The emitter renders
// them as set() statements, default-first and highest-priority-last, so OTTL's
// sequential "last write wins" reproduces the derived column's first-true-wins
// precedence.
package translate

import (
	"fmt"
	"strconv"

	"github.com/honeycombio/derived-column-translator-processor/pkg/hcdc"
)

// Branch is one (guard, value) pair. Guard is an OTTL boolean expression for a
// `where` clause; an empty Guard means the branch is unconditional.
type Branch struct {
	Guard string
	Value string
}

// Result is the outcome of translating one derived column.
type Result struct {
	Alias    string
	Branches []Branch
	Skipped  bool
	Reason   string // populated when Skipped is true
}

// unsupportedError carries a human-readable reason a node cannot be translated.
type unsupportedError struct{ reason string }

func (e *unsupportedError) Error() string { return e.reason }

func unsupported(format string, args ...any) error {
	return &unsupportedError{reason: fmt.Sprintf(format, args...)}
}

// Translate converts a derived column into a Result. It never returns an error;
// anything untranslatable yields Skipped=true with a Reason.
func Translate(alias string, node hcdc.Node, resolve Resolver) Result {
	t := &translator{resolve: resolve}
	branches, err := t.branches(node)
	if err != nil {
		return Result{Alias: alias, Skipped: true, Reason: err.Error()}
	}
	return Result{Alias: alias, Branches: branches}
}

type translator struct {
	resolve Resolver
}

// branches normalises a node into an ordered branch list (low -> high priority).
func (t *translator) branches(node hcdc.Node) ([]Branch, error) {
	if call, ok := node.(*hcdc.Call); ok {
		switch call.Name {
		case "IF":
			return t.ifBranches(call.Args)
		case "SWITCH":
			return t.switchBranches(call.Args)
		}
	}

	// Not a conditional. If the node is boolean-valued, materialise it as a
	// stored boolean: default false, then true where the condition holds.
	if b, err := t.boolExpr(node); err == nil {
		return []Branch{
			{Value: "false"},
			{Guard: b, Value: "true"},
		}, nil
	}

	// Otherwise it must be a plain value.
	v, err := t.valueExpr(node)
	if err != nil {
		return nil, err
	}
	return []Branch{{Value: v}}, nil
}

// ifBranches expands IF(c1, v1, c2, v2, ..., [default]).
func (t *translator) ifBranches(args []hcdc.Node) ([]Branch, error) {
	if len(args) < 2 {
		return nil, unsupported("IF needs at least 2 arguments, got %d", len(args))
	}
	var def hcdc.Node
	n := len(args)
	if n%2 == 1 {
		def = args[n-1]
		n--
	}
	pairs := make([]condVal, 0, n/2)
	for i := 0; i < n; i += 2 {
		pairs = append(pairs, condVal{args[i], args[i+1]})
	}
	return t.guardedBranches(pairs, def)
}

// switchBranches expands SWITCH(test, case1, result1, ..., [default]) by turning
// each case into the condition (test == case).
func (t *translator) switchBranches(args []hcdc.Node) ([]Branch, error) {
	if len(args) < 3 {
		return nil, unsupported("SWITCH needs at least 3 arguments, got %d", len(args))
	}
	test := args[0]
	rest := args[1:]
	var def hcdc.Node
	if len(rest)%2 == 1 {
		def = rest[len(rest)-1]
		rest = rest[:len(rest)-1]
	}
	pairs := make([]condVal, 0, len(rest)/2)
	for i := 0; i < len(rest); i += 2 {
		cond := &hcdc.Binary{Op: "=", L: test, R: rest[i]}
		pairs = append(pairs, condVal{cond, rest[i+1]})
	}
	return t.guardedBranches(pairs, def)
}

type condVal struct{ cond, val hcdc.Node }

// guardedBranches builds the branch list for a set of (cond, value) pairs and an
// optional default. Output order is low -> high priority: default first, then
// pairs from last to first, so the first pair ends up highest priority.
func (t *translator) guardedBranches(pairs []condVal, def hcdc.Node) ([]Branch, error) {
	var out []Branch

	if def != nil {
		inner, err := t.branches(def)
		if err != nil {
			return nil, err
		}
		out = append(out, inner...)
	}

	for i := len(pairs) - 1; i >= 0; i-- {
		guard, err := t.boolExpr(pairs[i].cond)
		if err != nil {
			return nil, err
		}
		inner, err := t.branches(pairs[i].val)
		if err != nil {
			return nil, err
		}
		for _, b := range inner {
			out = append(out, Branch{Guard: andGuards(guard, b.Guard), Value: b.Value})
		}
	}
	return out, nil
}

// andGuards combines an outer condition with an inner branch guard.
func andGuards(outer, inner string) string {
	if inner == "" {
		return outer
	}
	return "(" + outer + ") and (" + inner + ")"
}

// valueExpr renders a node as an OTTL value expression, or returns an
// unsupportedError if the node is boolean/conditional or uses an unsupported
// function.
func (t *translator) valueExpr(node hcdc.Node) (string, error) {
	switch v := node.(type) {
	case *hcdc.Literal:
		return literalValue(v), nil
	case *hcdc.Column:
		return t.resolve(v.Name), nil
	case *hcdc.Binary:
		switch v.Op {
		case "+", "-", "*", "/":
			l, err := t.valueExpr(v.L)
			if err != nil {
				return "", err
			}
			r, err := t.valueExpr(v.R)
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("(%s %s %s)", l, v.Op, r), nil
		case "%":
			return "", unsupported("modulo (%%) has no OTTL equivalent")
		default:
			return "", unsupported("boolean operator %q cannot be used as a value", v.Op)
		}
	case *hcdc.Unary:
		return "", unsupported("boolean operator %q cannot be used as a value", v.Op)
	case *hcdc.Call:
		return t.callValue(v)
	}
	return "", unsupported("unrecognised node %T", node)
}

// callValue handles value-producing function calls.
func (t *translator) callValue(c *hcdc.Call) (string, error) {
	switch c.Name {
	case "CONCAT":
		args, err := t.valueList(c.Args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Concat([%s], \"\")", args), nil
	case "COALESCE":
		args, err := t.valueList(c.Args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("Coalesce([%s])", args), nil
	case "INT":
		return t.unaryCall("Int", c)
	case "FLOAT":
		return t.unaryCall("Double", c)
	case "BOOL":
		return t.unaryCall("Bool", c)
	case "STRING":
		return t.unaryCall("String", c)
	case "TO_LOWER":
		return t.unaryCall("ToLowerCase", c)
	case "LENGTH":
		// HCDC LENGTH(str, "bytes"|"chars"); OTTL Len gives byte length.
		if len(c.Args) == 0 {
			return "", unsupported("LENGTH needs an argument")
		}
		return t.unaryCallNode("Len", c.Args[0])
	case "IF", "SWITCH":
		return "", unsupported("conditional %s cannot be used as an inline value", c.Name)
	default:
		// Boolean-returning or unsupported functions are not values.
		if _, err := t.callBool(c); err == nil {
			return "", unsupported("boolean function %s cannot be used as a value", c.Name)
		}
		return "", unsupported("function %s has no OTTL value mapping", c.Name)
	}
}

// boolExpr renders a node as an OTTL boolean expression usable in a where clause.
func (t *translator) boolExpr(node hcdc.Node) (string, error) {
	switch v := node.(type) {
	case *hcdc.Literal:
		if v.Kind == hcdc.KBool {
			return strconv.FormatBool(v.B), nil
		}
		return "", unsupported("non-boolean literal in boolean position")
	case *hcdc.Unary:
		if v.Op == "!" {
			x, err := t.boolExpr(v.X)
			if err != nil {
				return "", err
			}
			return "not (" + x + ")", nil
		}
		return "", unsupported("unary operator %q", v.Op)
	case *hcdc.Binary:
		return t.binaryBool(v)
	case *hcdc.Call:
		return t.callBool(v)
	default:
		return "", unsupported("%T is not a boolean", node)
	}
}

func (t *translator) binaryBool(b *hcdc.Binary) (string, error) {
	switch b.Op {
	case "AND", "OR":
		l, err := t.boolExpr(b.L)
		if err != nil {
			return "", err
		}
		r, err := t.boolExpr(b.R)
		if err != nil {
			return "", err
		}
		joiner := "and"
		if b.Op == "OR" {
			joiner = "or"
		}
		return fmt.Sprintf("(%s %s %s)", l, joiner, r), nil
	case "<", "<=", ">", ">=":
		return t.compare(b, b.Op)
	case "=":
		return t.compare(b, "==")
	case "!=":
		return t.compare(b, "!=")
	default:
		return "", unsupported("operator %q is not boolean", b.Op)
	}
}

func (t *translator) compare(b *hcdc.Binary, ottlOp string) (string, error) {
	l, err := t.valueExpr(b.L)
	if err != nil {
		return "", err
	}
	r, err := t.valueExpr(b.R)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s %s", l, ottlOp, r), nil
}

// callBool handles boolean-returning function calls.
func (t *translator) callBool(c *hcdc.Call) (string, error) {
	switch c.Name {
	case "STARTS_WITH", "PREFIX":
		return t.boolFunc("HasPrefix", c, 2)
	case "ENDS_WITH", "SUFFIX":
		return t.boolFunc("HasSuffix", c, 2)
	case "REG_MATCH":
		return t.boolFunc("IsMatch", c, 2)
	case "EXISTS", "HAS_VALUE":
		if len(c.Args) != 1 {
			return "", unsupported("%s needs exactly 1 argument", c.Name)
		}
		x, err := t.valueExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		return x + " != nil", nil
	case "IN", "NOT_IN":
		if len(c.Args) < 2 {
			return "", unsupported("%s needs at least 2 arguments", c.Name)
		}
		lhs, err := t.valueExpr(c.Args[0])
		if err != nil {
			return "", err
		}
		var clauses string
		for i, a := range c.Args[1:] {
			rv, err := t.valueExpr(a)
			if err != nil {
				return "", err
			}
			if i > 0 {
				clauses += " or "
			}
			clauses += fmt.Sprintf("%s == %s", lhs, rv)
		}
		expr := "(" + clauses + ")"
		if c.Name == "NOT_IN" {
			return "not " + expr, nil
		}
		return expr, nil
	default:
		return "", unsupported("function %s is not a supported boolean", c.Name)
	}
}

// boolFunc renders a fixed-arity OTTL boolean converter from a HCDC call.
func (t *translator) boolFunc(ottlName string, c *hcdc.Call, arity int) (string, error) {
	if len(c.Args) != arity {
		return "", unsupported("%s needs %d arguments, got %d", c.Name, arity, len(c.Args))
	}
	args, err := t.valueList(c.Args)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s(%s)", ottlName, args), nil
}

func (t *translator) unaryCall(ottlName string, c *hcdc.Call) (string, error) {
	if len(c.Args) != 1 {
		return "", unsupported("%s needs exactly 1 argument, got %d", c.Name, len(c.Args))
	}
	return t.unaryCallNode(ottlName, c.Args[0])
}

func (t *translator) unaryCallNode(ottlName string, arg hcdc.Node) (string, error) {
	v, err := t.valueExpr(arg)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s(%s)", ottlName, v), nil
}

// valueList renders a comma-separated list of value expressions.
func (t *translator) valueList(nodes []hcdc.Node) (string, error) {
	out := ""
	for i, n := range nodes {
		v, err := t.valueExpr(n)
		if err != nil {
			return "", err
		}
		if i > 0 {
			out += ", "
		}
		out += v
	}
	return out, nil
}

func literalValue(l *hcdc.Literal) string {
	switch l.Kind {
	case hcdc.KString:
		return strconv.Quote(l.S)
	case hcdc.KInt:
		return strconv.FormatInt(l.I, 10)
	case hcdc.KFloat:
		return strconv.FormatFloat(l.F, 'g', -1, 64)
	case hcdc.KBool:
		return strconv.FormatBool(l.B)
	case hcdc.KNull:
		return "nil"
	}
	return "nil"
}
