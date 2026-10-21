package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRouter(t *testing.T) {
	frontend := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("spa"))
	})
	router := NewRouter(Options{Frontend: frontend})

	tests := []struct {
		name        string
		method      string
		path        string
		wantStatus  int
		wantBody    string
		wantJSONErr string
	}{
		{name: "health check", method: http.MethodGet, path: "/healthz", wantStatus: http.StatusOK, wantBody: `"status":"ok"`},
		{name: "unknown api route is JSON 404", method: http.MethodGet, path: "/api/v1/nope", wantStatus: http.StatusNotFound, wantJSONErr: "not_found"},
		{name: "api root is JSON 404", method: http.MethodGet, path: "/api/", wantStatus: http.StatusNotFound, wantJSONErr: "not_found"},
		{name: "root goes to frontend", method: http.MethodGet, path: "/", wantStatus: http.StatusOK, wantBody: "spa"},
		{name: "client route goes to frontend", method: http.MethodGet, path: "/workloads/pods", wantStatus: http.StatusOK, wantBody: "spa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			if tt.wantJSONErr != "" {
				var body struct {
					Error errorBody `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode error body: %v", err)
				}
				if body.Error.Code != tt.wantJSONErr {
					t.Fatalf("error code = %q, want %q", body.Error.Code, tt.wantJSONErr)
				}
			}
			if csp := rec.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
				t.Fatalf("missing Content-Security-Policy, got %q", csp)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Fatalf("X-Content-Type-Options = %q", got)
			}
		})
	}
}
