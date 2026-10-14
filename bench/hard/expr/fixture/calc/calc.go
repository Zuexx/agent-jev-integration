// Package calc evaluates integer arithmetic expressions.
package calc

// Eval evaluates s and returns its int64 value.
//
// Grammar, from lowest to highest precedence:
//   - binary + and -, left-associative
//   - binary *, / and %, left-associative; / truncates toward zero and % takes
//     the sign of the dividend (Go semantics)
//   - unary + and - (may repeat, e.g. "--3")
//   - binary ^ (power), right-associative, binding tighter than unary minus,
//     so -2^2 == -4 and 2^3^2 == 512
//   - integer literals and parenthesised expressions
//
// Spaces are allowed between tokens. It returns an error for malformed input,
// division or modulo by zero, and negative exponents.
func Eval(s string) (int64, error) {
	panic("not implemented")
}
