// Package money is FunRoute's money, as Go: amounts in a currency's minor
// unit (Money), money between minor units inside a round (ExactMoney), exact
// ratios (Ratio), exchange rates (FxRate), the declared currency table
// (Currencies), the rounding modes, allocation and the aggregates. It knows
// nothing of the language: the machine carries these as values, and its
// kernel's money operations are these methods, so a rule and a host get one
// answer. Everything is two int64 at most and allocates nothing.
package money
