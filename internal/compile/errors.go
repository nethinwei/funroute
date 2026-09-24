package compile

import (
	"errors"

	"github.com/nethinwei/funroute/internal/kit"
	"github.com/nethinwei/funroute/internal/machine"
)

func compileError(err error) error {
	if err == nil || errors.Is(err, machine.ErrCompile) || errors.Is(err, machine.ErrContract) {
		return err
	}
	// errors.Is finds the class, errors.As the position the lexer or the
	// type checker recorded behind it.
	return kit.Classify(machine.ErrCompile, "", err)
}
