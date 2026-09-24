package lang

import "funroute/lang/internal/machine"

// A Manifest is a registry without its implementations, for a tool that has
// to type-check, explain and complete a program where the host's functions
// cannot run — a language server, an editor in the browser. Registry.Manifest
// writes one; Apply adds what it describes to a registry that holds the
// kernel and the standard library, each missing function as its signature
// alone, and TrackUnavailable says which of those a run called.
type Manifest = machine.Manifest

const ManifestVersion = machine.ManifestVersion

var TrackUnavailable = machine.TrackUnavailable
