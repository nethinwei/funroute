package compile

import (
	"errors"
	"fmt"

	"funroute/lang/internal/machine"
)

func compileError(err error) error {
	if err == nil || errors.Is(err, machine.ErrCompile) || errors.Is(err, machine.ErrContract) {
		return err
	}
	return fmt.Errorf("%w: %v", machine.ErrCompile, err)
}

func contractErrorf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", machine.ErrContract, fmt.Sprintf(format, args...))
}
