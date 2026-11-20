package googleapi

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
)

// Diagnostics use fixed categories only. URL paths, query strings, error text,
// response bodies and credentials must never enter the profile log.
func requestOperation(path string) string {
	switch path {
	case "/profile":
		return "profile"
	case "/labels":
		return "labels"
	case "/messages":
		return "messages_list"
	case "/history":
		return "history"
	}
	if strings.HasPrefix(path, "/messages/") {
		return "message"
	}
	return "other"
}

func requestErrorKind(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return "timeout"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "unexpected_eof"
	}
	if errors.Is(err, io.EOF) {
		return "eof"
	}
	return "transport"
}

func requestReason(err error) string {
	var remote *HTTPError
	if errors.As(err, &remote) {
		for _, reason := range remote.Reasons {
			switch reason {
			case "rateLimitExceeded", "userRateLimitExceeded", "backendError", "insufficientPermissions", "ACCESS_TOKEN_SCOPE_INSUFFICIENT", "badRequest", "invalidArgument", "failedPrecondition", "notFound", "domainPolicy":
				return reason
			}
		}
	}
	return "other"
}

func requestGoogleStatus(err *HTTPError) string {
	switch err.googleStatus {
	case "INVALID_ARGUMENT", "FAILED_PRECONDITION", "OUT_OF_RANGE", "NOT_FOUND", "PERMISSION_DENIED", "UNAUTHENTICATED", "RESOURCE_EXHAUSTED", "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED":
		return err.googleStatus
	}
	return "other"
}
