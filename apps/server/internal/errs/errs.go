// Package errs holds the sentinel errors every domain wraps; httpapi maps them to status codes.
package errs

import (
	"errors"
	"fmt"
)

var (
	ErrNotFound     = errors.New("not found")
	ErrForbidden    = errors.New("forbidden")
	ErrInvalid      = errors.New("invalid")
	ErrUnauthorized = errors.New("unauthorized")
	ErrConflict     = errors.New("conflict")
)

func Invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalid, msg) }
