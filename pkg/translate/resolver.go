package translate

import "fmt"

// Resolver maps a Honeycomb column name to an OTTL path expression.
//
// Honeycomb columns live in a flat namespace; OTTL has distinct scopes. The
// default maps every column to a span attribute. Callers can override specific
// columns (e.g. service.name -> resource.attributes["service.name"]).
type Resolver func(column string) string

// DefaultResolver maps $col to attributes["col"] (span attributes).
func DefaultResolver(column string) string {
	return fmt.Sprintf("attributes[%q]", column)
}

// NewResolver returns a Resolver that consults overrides first, then falls back
// to DefaultResolver. An override value is the literal OTTL path to use.
func NewResolver(overrides map[string]string) Resolver {
	return func(column string) string {
		if p, ok := overrides[column]; ok {
			return p
		}
		return DefaultResolver(column)
	}
}
