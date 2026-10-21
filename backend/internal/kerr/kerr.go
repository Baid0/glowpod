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
}

// Classify maps err. The message never includes server details or credentials.
func Classify(err error) Class {
	switch {
	case apierrors.IsForbidden(err):
		return Class{http.StatusForbidden, "forbidden", "You don't have access to this. Ask an admin for access."}
	case apierrors.IsUnauthorized(err):
		return Class{http.StatusUnauthorized, "cluster_unauthorized", "The cluster didn't accept your credentials. Check your kubeconfig."}
	case apierrors.IsNotFound(err):
		return Class{http.StatusNotFound, "not_found", "That doesn't exist anymore."}
	case apierrors.IsTooManyRequests(err):
		return Class{http.StatusServiceUnavailable, "cluster_busy", "The cluster is busy. Glowpod will try again."}
	case errors.Is(err, context.DeadlineExceeded), isTimeout(err):
		return Class{http.StatusGatewayTimeout, "cluster_timeout", "The cluster took too long to answer."}
	case isUnreachable(err):
		return Class{http.StatusServiceUnavailable, "cluster_unreachable", "Can't reach the cluster. Check that it's running and your network."}
	default:
		return Class{http.StatusBadGateway, "cluster_error", "The cluster returned an error."}
	}
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
