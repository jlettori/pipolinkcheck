package main

import "net/http"

// ErrorCode is a status value written to the CSV "Status Code" column. It is
// either a real HTTP status code or one of the internal error codes below.
type ErrorCode int

//go:generate stringer -linecomment -type=ErrorCode
const (
	errCodeUnauthorized        ErrorCode = http.StatusUnauthorized        // Unauthorized
	errCodeForbidden           ErrorCode = http.StatusForbidden           // Forbidden
	errCodeNotFound            ErrorCode = http.StatusNotFound            // Not Found
	errCodeTooManyRequests     ErrorCode = http.StatusTooManyRequests     // Too Many Requests
	errCodeInternalServerError ErrorCode = http.StatusInternalServerError // Internal Server Error
	errCodeBadGateway          ErrorCode = http.StatusBadGateway          // Bad Gateway
	errCodeServiceUnavailable  ErrorCode = http.StatusServiceUnavailable  // Service Unavailable
	errCodeGatewayTimeout      ErrorCode = http.StatusGatewayTimeout      // Gateway Timeout
	errCodeLinkedInDenied      ErrorCode = 999                            // LinkedIn denied the request

	// Internal error codes written to the CSV "Status Code" column when a link
	// fails before an HTTP response is received. They live in the 1001-1099
	// range so they can never collide with real HTTP status codes.
	errCodeRequestConstruction ErrorCode = 1001 // building the HTTP request failed
	errCodeRequestFailed       ErrorCode = 1002 // the HTTP request could not be completed
	errCodeProcessingPanic     ErrorCode = 1003 // processing the link panicked
)
