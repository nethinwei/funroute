// Package golden holds what the language answers, so that a change to how it
// runs cannot change what it says. Random programs over every kind of value
// the kernel and std know are run on a few fixed inputs — plain ones, empty
// ones, the extremes, long ones, floats that are not finite — through both
// ways a host runs a program, Program.Run and Runtime.RunValues; each
// answer's JSON, or each failure's class and words, is kept in
// testdata/outcomes.jsonl. go test fails when a run and the record disagree,
// and go test -update writes the record anew, for a change meant to change
// an answer: its diff is the change, read line by line.
package golden
