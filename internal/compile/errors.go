package compile

import (
	"errors"
	"fmt"

	"github.com/nethinwei/funroute/internal/machine"
	"github.com/nethinwei/funroute/internal/money"
)

func compileError(err error) error {
	if err == nil || errors.Is(err, machine.ErrCompile) || errors.Is(err, machine.ErrContract) {
		return err
	}
	// errors.Is finds the class, errors.As the position the lexer or the
	// type checker recorded behind it.
	return money.Classify(machine.ErrCompile, "", err)
}

func contractErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", machine.ErrContract, fmt.Sprintf(format, args...))
}
