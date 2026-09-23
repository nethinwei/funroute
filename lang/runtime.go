package lang

import "funroute/lang/internal/machine"

// Running an artifact. Instantiate rejects an artifact whose registry has
// drifted; Run enforces the fuel budget.
type (
	Runtime    = machine.Runtime
	RunOptions = machine.RunOptions
)

var Instantiate = machine.Instantiate

// DecodeArgs reads a program's arguments from a JSON object, keeping every
// digit of an integer; DefaultFuel is the budget to give a run when nobody
// named one. A CLI, a language server and a console decode alike through it.
var DecodeArgs = machine.DecodeArgs

const DefaultFuel = machine.DefaultFuel
