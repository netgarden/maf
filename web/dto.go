package web

import "fmt"

type GateError struct {
	Message string
	Err     error
}

func (e *GateError) Error() string {
	if e.Message != "" && e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	} else if e.Message != "" {
		return e.Message
	} else {
		return e.Err.Error()
	}
}

type InputError struct {
	GateError
}

func NewInputError(message string, err error) *InputError {
	return &InputError{
		GateError{
			Message: message,
			Err:     err,
		},
	}
}

type InternalError struct {
	GateError
}

func NewInternalError(message string, err error) *InternalError {
	return &InternalError{
		GateError{
			Message: message,
			Err:     err,
		},
	}
}

type NotFoundError struct {
	GateError
}

func NewNotFoundError(message string, err error) *NotFoundError {
	return &NotFoundError{
		GateError{
			Message: message,
			Err:     err,
		},
	}
}

type InvalidRequestError struct {
	GateError
}

func NewInvalidRequestError(message string, err error) *InvalidRequestError {
	return &InvalidRequestError{
		GateError{
			Message: message,
			Err:     err,
		},
	}
}
