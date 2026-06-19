package hcdc

import (
	"fmt"
	"strconv"
	"strings"
)

// Parse parses an HCDC derived-column expression into an AST.
func Parse(expr string) (Node, error) {
	toks, err := lex(expr)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	node, err := p.parseExpr(0)
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		return nil, fmt.Errorf("unexpected token %q at position %d", p.peek().text, p.peek().pos)
	}
	return node, nil
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() token { return p.toks[p.i] }

func (p *parser) next() token {
	t := p.toks[p.i]
	if p.i < len(p.toks)-1 {
		p.i++
	}
	return t
}

// leftBindingPower returns the infix binding power for a token, mirroring the
// precedence order in HCDC.g4 (tighter binds higher). 0 means "not an infix op".
func leftBindingPower(k tokenKind) int {
	switch k {
	case tOr:
		return 10
	case tAnd:
		return 20
	case tEq, tNeq:
		return 30
	case tLt, tLte, tGt, tGte:
		return 40
	case tPlus:
		return 50
	case tMinus:
		return 60
	case tStar, tSlash, tPercent:
		return 70
	default:
		return 0
	}
}

const bpNot = 80 // unary "!" binds tighter than any binary operator

func (p *parser) parseExpr(rbp int) (Node, error) {
	left, err := p.nud()
	if err != nil {
		return nil, err
	}
	for leftBindingPower(p.peek().kind) > rbp {
		op := p.next()
		right, err := p.parseExpr(leftBindingPower(op.kind))
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: opText(op.kind), L: left, R: right}
	}
	return left, nil
}

// nud handles tokens in prefix position.
func (p *parser) nud() (Node, error) {
	t := p.next()
	switch t.kind {
	case tNot:
		x, err := p.parseExpr(bpNot)
		if err != nil {
			return nil, err
		}
		return &Unary{Op: "!", X: x}, nil
	case tMinus:
		// HCDC only allows a leading minus on a numeric literal.
		num := p.next()
		switch num.kind {
		case tInt:
			v, err := strconv.ParseInt(num.text, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid integer %q: %w", num.text, err)
			}
			return &Literal{Kind: KInt, I: -v}, nil
		case tFloat:
			v, err := strconv.ParseFloat(num.text, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid float %q: %w", num.text, err)
			}
			return &Literal{Kind: KFloat, F: -v}, nil
		default:
			return nil, fmt.Errorf("unary '-' must be followed by a number at position %d", num.pos)
		}
	case tLParen:
		inner, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tRParen {
			return nil, fmt.Errorf("expected ')' at position %d", p.peek().pos)
		}
		p.next()
		return inner, nil
	case tIdent, tAnd, tOr:
		// Function call: AND and OR may also be used as function names.
		if p.peek().kind != tLParen {
			return nil, fmt.Errorf("expected '(' after %q at position %d", t.text, p.peek().pos)
		}
		return p.parseCall(t.text)
	case tColumn:
		return &Column{Name: t.text}, nil
	case tString:
		return &Literal{Kind: KString, S: t.text}, nil
	case tInt:
		v, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", t.text, err)
		}
		return &Literal{Kind: KInt, I: v}, nil
	case tFloat:
		v, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q: %w", t.text, err)
		}
		return &Literal{Kind: KFloat, F: v}, nil
	case tTrue:
		return &Literal{Kind: KBool, B: true}, nil
	case tFalse:
		return &Literal{Kind: KBool, B: false}, nil
	case tNull:
		return &Literal{Kind: KNull}, nil
	default:
		return nil, fmt.Errorf("unexpected token %q at position %d", t.text, t.pos)
	}
}

// parseCall parses the argument list of a function call; the name and '(' are
// already known. It supports an optional trailing comma.
func (p *parser) parseCall(name string) (Node, error) {
	p.next() // consume '('
	call := &Call{Name: strings.ToUpper(name)}
	if p.peek().kind == tRParen {
		p.next()
		return call, nil
	}
	for {
		arg, err := p.parseExpr(0)
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		switch p.peek().kind {
		case tComma:
			p.next()
			if p.peek().kind == tRParen { // trailing comma
				p.next()
				return call, nil
			}
		case tRParen:
			p.next()
			return call, nil
		default:
			return nil, fmt.Errorf("expected ',' or ')' at position %d", p.peek().pos)
		}
	}
}

func opText(k tokenKind) string {
	switch k {
	case tStar:
		return "*"
	case tSlash:
		return "/"
	case tPercent:
		return "%"
	case tPlus:
		return "+"
	case tMinus:
		return "-"
	case tLt:
		return "<"
	case tLte:
		return "<="
	case tGt:
		return ">"
	case tGte:
		return ">="
	case tEq:
		return "="
	case tNeq:
		return "!="
	case tAnd:
		return "AND"
	case tOr:
		return "OR"
	default:
		return "?"
	}
}
