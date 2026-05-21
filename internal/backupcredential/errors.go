package backupcredential

import (
	"errors"
	"fmt"
	"net/http"
)

// APIError wraps a non-2xx response from the backup server. Code/Message come
// from the JSON envelope when present; otherwise Message falls back to the
// HTTP status text.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound
}
