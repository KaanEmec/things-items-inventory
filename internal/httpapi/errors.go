package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

// Error codes and the HTTP status each maps to.
const (
	CodeBadRequest   = "bad_request"
	CodeUnauthorized = "unauthorized"
	CodeForbidden    = "forbidden"
	CodeNotFound     = "not_found"
	CodeConflict     = "conflict"
	CodeNotEmpty     = "not_empty"
	CodeInvalid      = "invalid"
	CodeTooLarge     = "too_large"
	CodeRateLimited  = "rate_limited"
	CodeInternal     = "internal"
)

var statusByCode = map[string]int{
	CodeBadRequest:   http.StatusBadRequest,
	CodeUnauthorized: http.StatusUnauthorized,
	CodeForbidden:    http.StatusForbidden,
	CodeNotFound:     http.StatusNotFound,
	CodeConflict:     http.StatusConflict,
	CodeNotEmpty:     http.StatusConflict,
	CodeInvalid:      http.StatusUnprocessableEntity,
	CodeTooLarge:     http.StatusRequestEntityTooLarge,
	CodeRateLimited:  http.StatusTooManyRequests,
	CodeInternal:     http.StatusInternalServerError,
}

// Error is the one error shape of the API:
// {"error":{"code":"…","message":"…","fields":{"name":"…"}}}.
// Message is written for a person. Fields maps input names to what is wrong.
type Error struct {
	Code    string            `json:"code"`
	Message string            `json:"message"`
	Fields  map[string]string `json:"fields,omitempty"`
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

// Status returns the HTTP status for the error's code (500 when unknown).
func (e *Error) Status() int {
	if s, ok := statusByCode[e.Code]; ok {
		return s
	}
	return http.StatusInternalServerError
}

// NewError builds an Error with the given code and message.
func NewError(code, message string) *Error { return &Error{Code: code, Message: message} }

func BadRequest(message string) *Error { return NewError(CodeBadRequest, message) }
func NotFound(message string) *Error   { return NewError(CodeNotFound, message) }
func Conflict(message string) *Error   { return NewError(CodeConflict, message) }

// Unprocessable is an invalid (422) error: the request is well formed but
// asks for something the data does not allow (a cycle, a missing target).
func Unprocessable(message string, fields map[string]string) *Error {
	e := NewError(CodeInvalid, message)
	e.Fields = fields
	return e
}

// NotEmpty is a not_empty (409) error; fields carry what is still inside.
func NotEmpty(message string, fields map[string]string) *Error {
	e := NewError(CodeNotEmpty, message)
	e.Fields = fields
	return e
}

// Invalid is a bad_request carrying per-field messages.
func Invalid(fields map[string]string) *Error {
	e := BadRequest("Some fields need attention.")
	e.Fields = fields
	return e
}

// WriteJSON writes v as a JSON response with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "err", err)
	}
}

// WriteError writes err in the error shape. An *Error is sent as is; anything
// else is logged and sent as a generic internal error, never leaked.
func WriteError(w http.ResponseWriter, err error) {
	var e *Error
	if !errors.As(err, &e) {
		slog.Error("internal error", "err", err)
		e = NewError(CodeInternal, "Something went wrong. Please try again.")
	}
	WriteJSON(w, e.Status(), struct {
		Error *Error `json:"error"`
	}{e})
}

// DecodeJSON reads the request body into dst: at most 1 MiB, unknown fields
// rejected. The returned error is an *Error ready for WriteError.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		var syntax *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.As(err, &tooBig):
			return NewError(CodeTooLarge, "That request is too large.")
		case errors.As(err, &syntax), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			return BadRequest("The request body is not valid JSON.")
		case errors.As(err, &typ):
			e := BadRequest("Some fields have the wrong type.")
			e.Fields = map[string]string{typ.Field: "Expected " + typ.Type.String() + "."}
			return e
		default:
			return BadRequest("The request body could not be read: " + err.Error())
		}
	}
	if dec.More() {
		return BadRequest("The request body must hold a single JSON value.")
	}
	return nil
}
