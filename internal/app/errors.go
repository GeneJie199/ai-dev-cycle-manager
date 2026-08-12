package app

import (
	"errors"
	"fmt"
)

type validationError struct {
	message string
}

func (err *validationError) Error() string { return err.message }

func validationf(format string, args ...any) error {
	return &validationError{message: fmt.Sprintf(format, args...)}
}

func IsValidationError(err error) bool {
	var target *validationError
	return errors.As(err, &target)
}
