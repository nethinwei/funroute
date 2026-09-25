// Package api holds the tests of package funroute, written the way a host
// writes them: they import github.com/nethinwei/funroute and nothing else of
// this module's, so they use only the public surface and guard it. Their files
// are named by topic, not by a source file — the whole surface is one file,
// funroute.go. example_test.go holds the runnable examples, their output
// checked by go test.
package api
