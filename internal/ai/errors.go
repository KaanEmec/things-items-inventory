package ai

import (
	"fmt"
	"strings"
)

// Kind classifies a failure so the UI can explain it.
type Kind string

const (
	KindAuth                Kind = "auth"
	KindModelNotFound       Kind = "model_not_found"
	KindNoImageInput        Kind = "no_image_input"
	KindStructuredUnsupport Kind = "structured_unsupported"
	KindRateLimited         Kind = "rate_limited"
	KindNoCredit            Kind = "no_credit"
	KindTimeout             Kind = "timeout"
	KindInvalidOutput       Kind = "invalid_output"
	KindUnreachable         Kind = "unreachable"
	KindOther               Kind = "other"
)

// Error is the only error type this package returns (besides context errors).
// Message is written for a person and never contains the token.
type Error struct {
	Kind    Kind
	Status  int // HTTP status, 0 when there was none
	Message string
}

func (e *Error) Error() string {
	if e.Status != 0 {
		return fmt.Sprintf("ai: %s (HTTP %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("ai: %s: %s", e.Kind, e.Message)
}

// classify maps an HTTP error response to an Error. body is the raw response.
func classify(status int, body, token string) *Error {
	detail := scrub(snippet(body), token)
	low := strings.ToLower(body)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(low, s) {
				return true
			}
		}
		return false
	}
	e := &Error{Status: status}
	switch {
	case status == 401 || status == 403:
		e.Kind, e.Message = KindAuth, "The endpoint rejected the API token. Check that it is correct and has access to this model."
	case status == 402 || has("insufficient_quota", "insufficient credit", "insufficient_credit", "out of credit", "exceeded your current quota"):
		e.Kind, e.Message = KindNoCredit, "The account is out of credit or over its quota. Add credit at the provider and try again."
	case has("image input", "image_url", "vision", "multimodal", "does not support image", "images are not supported"):
		e.Kind, e.Message = KindNoImageInput, "This model cannot read images. Pick a model with image input."
	case has("response_format", "json_schema", "json_object", "structured output", "structured_output", "requested parameters"):
		e.Kind, e.Message = KindStructuredUnsupport, "This model or provider does not support the requested structured-output format."
	case status == 404 || (status == 400 && has("model") && has("not found", "does not exist", "invalid model", "unknown model", "not a valid model")):
		e.Kind, e.Message = KindModelNotFound, "The model was not found at this endpoint. Check the model name."
	case status == 429:
		e.Kind, e.Message = KindRateLimited, "The endpoint is rate limiting requests. Try again in a moment."
	default:
		e.Kind, e.Message = KindOther, fmt.Sprintf("The endpoint returned an error (HTTP %d).", status)
	}
	if detail != "" {
		e.Message += " Endpoint said: " + detail
	}
	return e
}

func snippet(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 300 {
		s = s[:300] + "..."
	}
	return s
}

func scrub(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "[token]")
	}
	return s
}
