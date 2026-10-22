package kerr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
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
		{"conflict", apierrors.NewConflict(pods, "api", errors.New("changed")), http.StatusConflict, "conflict"},
		{"exists", apierrors.NewAlreadyExists(pods, "api"), http.StatusConflict, "already_exists"},
		{"invalid", apierrors.NewInvalid(schema.GroupKind{Kind: "Pod"}, "api", nil), http.StatusUnprocessableEntity, "invalid"},
		{"bad request", apierrors.NewBadRequest("no"), http.StatusBadRequest, "bad_request"},
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

func TestClassifyDetail(t *testing.T) {
	deny := apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "api",
		errors.New(`admission webhook "policy.example.com" denied the request: images must be signed`))
	if got := Classify(deny).Detail; got == "" || !strings.Contains(got, "images must be signed") {
		t.Fatalf("detail = %q", got)
	}
	if got := Classify(fmt.Errorf("x: %w", context.DeadlineExceeded)).Detail; got != "" {
		t.Fatalf("non-API errors have no detail, got %q", got)
	}
	long := apierrors.NewBadRequest(string(make([]byte, 5000)))
	if got := Classify(long).Detail; len(got) > maxDetail+len("…") {
		t.Fatalf("detail not capped: %d bytes", len(got))
	}
}
