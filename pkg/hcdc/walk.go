package hcdc

// Columns returns the distinct column names referenced in an expression, in
// first-seen order. Used to compute dependencies between derived columns.
func Columns(n Node) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(Node)
	walk = func(n Node) {
		switch v := n.(type) {
		case *Column:
			if !seen[v.Name] {
				seen[v.Name] = true
				out = append(out, v.Name)
			}
		case *Unary:
			walk(v.X)
		case *Binary:
			walk(v.L)
			walk(v.R)
		case *Call:
			for _, a := range v.Args {
				walk(a)
			}
		}
	}
	walk(n)
	return out
}
