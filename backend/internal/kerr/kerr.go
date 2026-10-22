// Package kerr turns Kubernetes client errors into an HTTP status, a stable
// code and a short, plain message for the UI.
package kerr

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"syscall"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Class is how the UI should treat an error.
type Class struct {
	Status  int
	Code    string
	Message string
	// Detail is the API server's own explanation, for errors where it helps
	// (validation, conflicts, policy denials). It names fields and rules,
	// never credentials.
	Detail string
}

// maxDetail keeps a long webhook message from flooding the UI.
const maxDetail = 2000

// Classify maps err. The message never includes server details or credentials.
func Classify(err error) Class {
	switch {
	case apierrors.IsConflict(err):
		return Class{http.StatusConflict, "conflict", "Someone changed this since you opened it. Reload to see their changes, then try again.", detail(err)}
	case apierrors.IsAlreadyExists(err):
		return Class{http.StatusConflict, "already_exists", "Something with that name already exists.", detail(err)}
	case apierrors.IsInvalid(err):
		return Class{http.StatusUnprocessableEntity, "invalid", "The cluster didn't accept this. Fix the fields below and try again.", detail(err)}
	case apierrors.IsBadRequest(err):
		return Class{http.StatusBadRequest, "bad_request", "The cluster didn't accept this request.", detail(err)}
	case apierrors.IsForbidden(err):
		return Class{http.StatusForbidden, "forbidden", "You don't have access to this. Ask an admin for access.", detail(err)}
	case apierrors.IsUnauthorized(err):
		return Class{Status: http.StatusUnauthorized, Code: "cluster_unauthorized", Message: "The cluster didn't accept your credentials. Sign in again."}
	case apierrors.IsNotFound(err):
		return Class{Status: http.StatusNotFound, Code: "not_found", Message: "That doesn't exist anymore."}
	case apierrors.IsTooManyRequests(err):
		return Class{Status: http.StatusServiceUnavailable, Code: "cluster_busy", Message: "The cluster is busy. Glowpod will try again."}
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return Class{Status: http.StatusGatewayTimeout, Code: "cluster_timeout", Message: "The cluster took too long to answer."}
	case isUnreachable(err):
		return Class{Status: http.StatusServiceUnavailable, Code: "cluster_unreachable", Message: "Can't reach the cluster. Check that it's running and your network."}
	default:
		return Class{Status: http.StatusBadGateway, Code: "cluster_error", Message: "The cluster returned an error."}
	}
}

// detail is the API status message, when err came from the API server.
func detail(err error) string {
	var status apierrors.APIStatus
	if !errors.As(err, &status) {
		return ""
	}
	msg := status.Status().Message
	if msg == "unknown" { // what some API paths say instead of nothing
		return ""
	}
	if len(msg) > maxDetail {
		msg = msg[:maxDetail] + "…"
	}
	return msg
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func isUnreachable(err error) bool {
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var urlErr *url.Error
	return errors.As(err, &urlErr)
}
