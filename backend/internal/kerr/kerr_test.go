package kerr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"syscall"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestClassify(t *testing.T) {
	pods := schema.GroupResource{Resource: "pods"}
	refused := &url.Error{Op: "Get", URL: "https://192.0.2.10:6443/api/v1/nodes", Err: &net.OpError{Op: "dial", Err: syscall.ECONNREFUSED}}

	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"forbidden", apierrors.NewForbidden(pods, "", errors.New("rbac")), http.StatusForbidden, "forbidden"},
		{"unauthorized", apierrors.NewUnauthorized("bad token"), http.StatusUnauthorized, "cluster_unauthorized"},
		{"not found", apierrors.NewNotFound(pods, "api"), http.StatusNotFound, "not_found"},
		{"throttled", apierrors.NewTooManyRequests("slow down", 1), http.StatusServiceUnavailable, "cluster_busy"},
		{"deadline", fmt.Errorf("list nodes: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, "cluster_timeout"},
		{"connection refused", fmt.Errorf("list nodes: %w", refused), http.StatusServiceUnavailable, "cluster_unreachable"},
		{"no route", &net.OpError{Op: "dial", Err: syscall.EHOSTUNREACH}, http.StatusServiceUnavailable, "cluster_unreachable"},
		{"other", apierrors.NewInternalError(errors.New("etcd")), http.StatusBadGateway, "cluster_error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Classify(tt.err)
			if got.Status != tt.status || got.Code != tt.code || got.Message == "" {
				t.Fatalf("got %+v, want %d %s", got, tt.status, tt.code)
			}
		})
	}
}
