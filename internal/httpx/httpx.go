// Package httpx provides HTTP response helpers.
package httpx

import (
	"encoding/json"
	"net/http"
)

// WriteJSON writes an application/json response body.
func WriteJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// ErrorBody is a stable error response format.
type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteError writes a JSON error payload.
func WriteError(w http.ResponseWriter, status int, code, msg string) {
	WriteJSON(w, status, ErrorBody{Code: code, Message: msg})
}
