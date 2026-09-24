package kit

import "fmt"

// Classify is one error with a class and a cause: errors.Is answers the class
// from this error itself, and Unwrap leads to the cause, so a host still finds
// its own error behind the class it was given. detail, when there is one,
// sits between the two in the text and ends in its own separator.
func Classify(class error, detail string, cause error) error {
	return &classified{class: class, detail: detail, cause: cause}
}

type classified struct {
	class  error
	detail string
	cause  error
}

func (c *classified) Error() string        { return c.class.Error() + ": " + c.detail + c.cause.Error() }
func (c *classified) Is(target error) bool { return target == c.class }
func (c *classified) Unwrap() error        { return c.cause }

// Errorf is a message of the class class: "class: message".
func Errorf(class error, format string, args ...any) error {
	return fmt.Errorf("%w: %s", class, fmt.Sprintf(format, args...))
}
