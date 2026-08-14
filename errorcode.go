package main

import "net/http"

// ErrorCode is a status value written to the CSV "Status Code" column. It is
// either a real HTTP status code or one of the internal error codes below.
type ErrorCode int

//go:generate stringer -linecomment -type=ErrorCode
const (
	errCodeBadRequest                    ErrorCode = http.StatusBadRequest                    // Bad Request
	errCodeUnauthorized                  ErrorCode = http.StatusUnauthorized                  // Unauthorized
	errCodeForbidden                     ErrorCode = http.StatusForbidden                     // Forbidden
	errCodeNotFound                      ErrorCode = http.StatusNotFound                      // Not Found
	errCodeMethodNotAllowed              ErrorCode = http.StatusMethodNotAllowed              // Method Not Allowed
	errCodeNotAcceptable                 ErrorCode = http.StatusNotAcceptable                 // Not Acceptable
	errCodeRequestTimeout                ErrorCode = http.StatusRequestTimeout                // Request Timeout
	errCodeConflict                      ErrorCode = http.StatusConflict                      // Conflict
	errCodeGone                          ErrorCode = http.StatusGone                          // Gone
	errCodeLengthRequired                ErrorCode = http.StatusLengthRequired                // Length Required
	errCodePayloadTooLarge               ErrorCode = http.StatusRequestEntityTooLarge         // Payload Too Large
	errCodeURITooLong                    ErrorCode = http.StatusRequestURITooLong             // URI Too Long
	errCodeUnsupportedMediaType          ErrorCode = http.StatusUnsupportedMediaType          // Unsupported Media Type
	errCodeRequestedRangeNotSatisfiable  ErrorCode = http.StatusRequestedRangeNotSatisfiable  // Range Not Satisfiable
	errCodeUnprocessableEntity           ErrorCode = http.StatusUnprocessableEntity           // Unprocessable Entity
	errCodeUpgradeRequired               ErrorCode = http.StatusUpgradeRequired               // Upgrade Required
	errCodePreconditionRequired          ErrorCode = http.StatusPreconditionRequired          // Precondition Required
	errCodeTooManyRequests               ErrorCode = http.StatusTooManyRequests               // Too Many Requests
	errCodeRequestHeaderFieldsTooLarge   ErrorCode = http.StatusRequestHeaderFieldsTooLarge   // Request Header Fields Too Large
	errCodeUnavailableForLegalReasons    ErrorCode = http.StatusUnavailableForLegalReasons    // Unavailable For Legal Reasons
	errCodeInternalServerError           ErrorCode = http.StatusInternalServerError           // Internal Server Error
	errCodeNotImplemented                ErrorCode = http.StatusNotImplemented                // Not Implemented
	errCodeBadGateway                    ErrorCode = http.StatusBadGateway                    // Bad Gateway
	errCodeServiceUnavailable            ErrorCode = http.StatusServiceUnavailable            // Service Unavailable
	errCodeGatewayTimeout                ErrorCode = http.StatusGatewayTimeout                // Gateway Timeout
	errCodeHTTPVersionNotSupported       ErrorCode = http.StatusHTTPVersionNotSupported       // HTTP Version Not Supported
	errCodeNetworkAuthenticationRequired ErrorCode = http.StatusNetworkAuthenticationRequired // Network Authentication Required
	errCodeCloudflare520                 ErrorCode = 520                                      // Unknown Error
	errCodeCloudflare521                 ErrorCode = 521                                      // Web Server Is Down
	errCodeCloudflare522                 ErrorCode = 522                                      // Connection Timed Out
	errCodeCloudflare523                 ErrorCode = 523                                      // Origin Is Unreachable
	errCodeCloudflare524                 ErrorCode = 524                                      // A Timeout Occurred
	errCodeCloudflare525                 ErrorCode = 525                                      // SSL Handshake Failed
	errCodeCloudflare526                 ErrorCode = 526                                      // Invalid SSL Certificate
	errCodeCloudflare527                 ErrorCode = 527                                      // Railgun Error
	errCodeCloudflare530                 ErrorCode = 530                                      // Origin DNS Error
	errCodeLinkedInDenied                ErrorCode = 999                                      // LinkedIn denied the request

	// Internal error codes written to the CSV "Status Code" column when a link
	// fails before an HTTP response is received. They live in the 1001-1099
	// range so they can never collide with real HTTP status codes.
	errCodeRequestConstruction ErrorCode = 1001 // building the HTTP request failed
	errCodeRequestFailed       ErrorCode = 1002 // the HTTP request could not be completed
	errCodeProcessingPanic     ErrorCode = 1003 // processing the link panicked
)
