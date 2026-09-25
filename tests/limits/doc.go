// Package limits measures the limits docs/limits.md states and keeps them
// true. Each table between <!-- limits:name --> and <!-- /limits:name --> in
// that file is generated here, by running what it says through the public
// package; go test fails when the file and a run disagree, and go test
// -update (make limits) writes the tables anew. The prose around them is
// written by hand. Only what a run decides is generated — an edge, a class, a
// cost — never a timing, which is tests/perf's.
package limits
