package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog"
)

// ErrResponse is used as the Response Body
type ErrResponse struct {
	Error ServiceError `json:"error"`
}

// ServiceError has fields for Service errors. All fields with no data will
// be omitted
type ServiceError struct {
	Kind    string `json:"kind,omitempty"`
	Code    string `json:"code,omitempty"`
	Param   string `json:"param,omitempty"`
	Message string `json:"message,omitempty"`
}

// HTTPErrorResponse takes a writer, error and a logger, performs a
// type switch to determine if the type is an HTTPError (which meets
// the Error interface as defined in this package), then sends the
// Error as a response to the client. If the type does not meet the
// Error interface as defined in this package, then a proper error
// is still formed and sent to the client, however, the Kind and
// Code will be Unanticipated. Logging of error is also done using
// https://github.com/rs/zerolog
func HTTPErrorResponse(w http.ResponseWriter, logger zerolog.Logger, err error) {

	// statusCode maps an error Kind to an HTTP Status Code
	// the zero value of Kind is Other, so if no Kind is present
	// in the error, Other is the default
	var statusCode = map[Kind]int{
		Unauthenticated: http.StatusUnauthorized,
		Unauthorized:    http.StatusForbidden,
		Permission:      http.StatusForbidden,
		Other:           http.StatusBadRequest,
		Invalid:         http.StatusBadRequest,
		Exist:           http.StatusBadRequest,
		NotExist:        http.StatusBadRequest,
		Private:         http.StatusBadRequest,
		BrokenLink:      http.StatusBadRequest,
		Validation:      http.StatusBadRequest,
		InvalidRequest:  http.StatusBadRequest,
		IO:              http.StatusInternalServerError,
		Internal:        http.StatusInternalServerError,
		Database:        http.StatusInternalServerError,
		Unanticipated:   http.StatusInternalServerError,
	}

	var httpStatusCode int

	if err != nil {
		// We perform a "type switch" https://tour.golang.org/methods/16
		// to determine the interface value type
		switch e := err.(type) {
		// If the interface value is of type Error (not a typical error, but
		// the Error interface defined above), then
		case *Error:
			httpStatusCode = statusCode[e.Kind]
			// We can retrieve the status here and write out a specific
			// HTTP status code. If there is error is empty, just
			// send the HTTP Status Code as response
			if e.isZero() {
				logger.Error().Int("HTTP Error StatusCode", httpStatusCode).Msg("")
				sendError(w, "", httpStatusCode)
			} else if e.Kind == Unauthenticated {
				// For Unauthenticated and Unauthorized errors,
				// the response body should be empty. Use logger
				// to log the error and then just send
				// http.StatusUnauthorized (401) or http.StatusForbidden (403)
				// depending on the circumstances. "In summary, a
				// 401 Unauthorized response should be used for missing or bad authentication,
				// and a 403 Forbidden response should be used afterwards, when the user is
				// authenticated but isn’t authorized to perform the requested operation on
				// the given resource."
				logger.Error().Int("HTTP Error StatusCode", http.StatusUnauthorized).Msg(e.Error())
				sendError(w, "", httpStatusCode)
			} else if e.Kind == Unauthorized {
				logger.Error().Int("HTTP Error StatusCode", http.StatusForbidden).Msg(e.Error())
				sendError(w, "", httpStatusCode)
			} else {
				// Make a copy
				eCopy := *e

				// fullErr is the full error that is to be logged
				// before removing the error stack details through the
				// StripStack function
				fullErr := &eCopy
				// log the full embedded error before removing the
				// error stack
				logger.Error().Err(fullErr).
					Int("HTTPStatusCode", httpStatusCode).
					Str("Kind", fullErr.Kind.String()).
					Str("Parameter", string(fullErr.Param)).
					Str("Code", string(fullErr.Code)).
					Msg("Response Error Sent")
				// For API response errors, don't show full recursion details,
				// just the error message
				fullErr.Err = StripStack(fullErr)
				fullErr.StripError = true
				e.Err = fullErr

				er := ErrResponse{
					Error: ServiceError{
						Kind:    e.Kind.String(),
						Code:    string(e.Code),
						Param:   string(e.Param),
						Message: e.Error(),
					},
				}

				// Marshal errResponse struct to JSON for the response body
				errJSON, _ := json.Marshal(er)

				sendError(w, string(errJSON), httpStatusCode)
			}

		default:
			// Any error types we don't specifically look out for default
			// to serving a HTTP 500
			cd := http.StatusInternalServerError
			er := ErrResponse{
				Error: ServiceError{
					Kind:    Unanticipated.String(),
					Code:    "Unanticipated",
					Message: "Unexpected error - contact support",
				},
			}

			logger.Error().Msgf("Unknown Error - HTTP %d - %s", cd, err.Error())

			// Marshal errResponse struct to JSON for the response body
			errJSON, _ := json.Marshal(er)

			sendError(w, string(errJSON), cd)
		}
	} else {
		httpStatusCode = statusCode[0]
		// if a nil error is passed, do not write a response body,
		// just send the HTTP Status Code
		logger.Error().Int("HTTP Error StatusCode", httpStatusCode).Msg("nil error - no response body sent")
		sendError(w, "", httpStatusCode)
	}
}

// Taken from standard library, but changed to send application/json as header
// Error replies to the request with the specified error message and HTTP code.
// It does not otherwise end the request; the caller should ensure no further
// writes are done to w.
// The error message should be json.
func sendError(w http.ResponseWriter, errStr string, httpStatusCode int) {
	if errStr != "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(httpStatusCode)
	// Only write response body if there is an error string populated
	if errStr != "" {
		_, _ = fmt.Fprintln(w, errStr)
	}
}

// StripStack takes an Error type (Error defined in this module) and
// removes the leading stack information
func StripStack(e error) error {
	err, ok := e.(*Error)
	if ok {
		// get error string
		errStr := err.Error()
		// get position where |: character lands in string
		idx := strings.Index(errStr, "|:")
		// substring from after the |: character
		substring := errStr[idx+3:]
		// put substring back into error
		return errors.New(substring)
	}

	// If it's not an *Error type, don't strip anything
	return e
}
