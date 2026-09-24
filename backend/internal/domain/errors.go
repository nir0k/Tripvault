// Package domain holds the entities and business rules of the service. It
// depends on no transport or storage package, so the rules can be tested and
// reused independently of how data arrives or is persisted.
package domain

import (
	"errors"
	"fmt"
)

// Sentinel errors the storage and service layers return so that the HTTP layer
// can map a failure to a status code without knowing which package produced it.
var (
	// ErrNotFound reports that the requested entity does not exist.
	ErrNotFound = errors.New("not found")
	// ErrAlreadyExists reports a uniqueness conflict, such as a duplicate email.
	ErrAlreadyExists = errors.New("already exists")
	// ErrInvalidCredentials reports a failed sign-in. It deliberately does not
	// distinguish an unknown account, a deactivated one and a wrong password, so
	// the endpoint cannot be used to learn which addresses are registered.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrTokenInvalid reports a token that is malformed, expired, revoked or
	// signed with the wrong key.
	ErrTokenInvalid = errors.New("token is invalid")
	// ErrLastAdmin reports a change that would leave the service without an
	// active administrator, and therefore with nobody able to manage accounts.
	ErrLastAdmin = errors.New("the service must keep at least one active administrator")
	// ErrForbidden reports an action the reader's role on a trip does not allow.
	ErrForbidden = errors.New("forbidden")
	// ErrAlreadyMember reports adding a person who already has access to a trip.
	ErrAlreadyMember = errors.New("the person already has access to the trip")
)

// ValidationError reports input that violates a business rule. Field names the
// offending input and Code is a stable machine-readable reason the client
// translates; Message is an English explanation for logs and API users.
type ValidationError struct {
	Field   string
	Code    string
	Message string
}

// Error - formats the validation failure.
//
// Returns:
//   - the field and the English explanation.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// NewValidationError - builds a validation failure for one field.
//
// Arguments:
//   - field: the name of the input field as the API spells it.
//   - code: the stable reason code, such as "too_short".
//   - message: an English explanation.
//
// Returns:
//   - the error.
func NewValidationError(field, code, message string) error {
	return &ValidationError{Field: field, Code: code, Message: message}
}
