// Package compile turns a program into an artifact: type inference against
// the host's contract and the registry, constant folding with the real VM,
// and bytecode with a digest. It also answers what a program means without
// running it — Analyze for a language server, the text contract, the
// rendering of a contract as comments — and hosts Bind, the compiler for a
// contract read off Go types. It depends on syntax and machine.
package compile
