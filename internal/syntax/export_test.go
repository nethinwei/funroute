package syntax

// This file exposes to the package's own tests what production code has no
// use for: nothing outside the tests needs a one-line rendering.

// Inline writes expr on one line.
func Inline(expr Expr) string { return inline(expr, 0) }
