package plugin

import "errors"

var (
	// ErrValidationFailed indicates one or more entries failed validation.
	ErrValidationFailed = errors.New("validation failed")

	// ErrUnknownTable indicates the entry references an unknown table.
	ErrUnknownTable = errors.New("unknown table")

	// ErrMissingRequiredField indicates a required field is missing.
	ErrMissingRequiredField = errors.New("missing required field")

	// ErrInvalidPayload indicates the payload JSON is malformed.
	ErrInvalidPayload = errors.New("invalid payload")
)

// ValidationError provides details about a validation failure.
type ValidationError struct {
	Sequence  int64  `json:"sequence"`
	TableName string `json:"table_name"`
	EntityID  string `json:"entity_id"`
	Field     string `json:"field,omitempty"`
	Message   string `json:"message"`
}

// Error implements the error interface.
func (e ValidationError) Error() string {
	if e.Field != "" {
		return e.TableName + "." + e.Field + ": " + e.Message
	}
	return e.TableName + ": " + e.Message
}

// ValidationErrors collects multiple validation errors.
type ValidationErrors struct {
	Errors []ValidationError `json:"errors"`
}

// Error implements the error interface.
func (e ValidationErrors) Error() string {
	if len(e.Errors) == 0 {
		return "validation failed"
	}
	return e.Errors[0].Error()
}

// Unwrap returns ErrValidationFailed for errors.Is() compatibility.
func (e ValidationErrors) Unwrap() error {
	return ErrValidationFailed
}
