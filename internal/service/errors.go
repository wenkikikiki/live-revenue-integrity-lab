// Package service contains business logic for money movement and projections.
package service

import "fmt"

// AppError is a user-visible API error.
type AppError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *AppError) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

var errValidation = &AppError{HTTPStatus: 400, Code: "VALIDATION_ERROR", Message: "invalid request payload"}
