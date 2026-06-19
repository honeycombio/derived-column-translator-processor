// Package hcdc parses the Honeycomb Derived Column (HCDC) expression language
// into an AST. The grammar is a faithful reimplementation of
// hound/lib/retriever/derived/parser/HCDC.g4 as a hand-written lexer + Pratt
// parser, avoiding an ANTLR runtime dependency (this parser is embedded in a
// collector binary).
package hcdc

// Node is any AST node.
type Node interface{ node() }

// Kind discriminates Literal values.
type Kind int

const (
	KString Kind = iota
	KInt
	KFloat
	KBool
	KNull
)

// Literal is a constant: string, int, float, bool, or null.
type Literal struct {
	Kind Kind
	S    string  // KString
	I    int64   // KInt
	F    float64 // KFloat
	B    bool    // KBool
}

// Column is a reference to a Honeycomb column, e.g. $duration_ms.
type Column struct {
	Name string
}

// Call is a function invocation. Name is canonicalised to upper case because
// HCDC function names are case-insensitive.
type Call struct {
	Name string
	Args []Node
}

// Unary is a prefix operator: "!" (logical not).
type Unary struct {
	Op string
	X  Node
}

// Binary is an infix operator. Op is one of:
// * / % + - < <= > >= = != AND OR
type Binary struct {
	Op   string
	L, R Node
}

func (*Literal) node() {}
func (*Column) node()  {}
func (*Call) node()    {}
func (*Unary) node()   {}
func (*Binary) node()  {}
