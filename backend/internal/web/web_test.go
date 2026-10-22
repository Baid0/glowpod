package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	built := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>Glowpod</title>")},
		"assets/index-abc.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc.css": {Data: []byte("body{}")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		".gitkeep":             {Data: nil},
	}
	empty := fstest.MapFS{".gitkeep": {Data: nil}}

	tests := []struct {
		name         string
		fsys         fstest.MapFS
		method       string
		path         string
		wantStatus   int
		wantBody     string
		wantType     string
		wantCacheHdr string
	}{
		{name: "root serves index", fsys: built, path: "/", wantStatus: 200, wantBody: "<title>Glowpod", wantType: "text/html", wantCacheHdr: "no-store"},
		{name: "index.html directly", fsys: built, path: "/index.html", wantStatus: 200, wantBody: "<title>Glowpod"},
		{name: "client route falls back to index", fsys: built, path: "/workloads/deployments", wantStatus: 200, wantBody: "<title>Glowpod"},
		{name: "hashed asset is cached forever", fsys: built, path: "/assets/index-abc.js", wantStatus: 200, wantBody: "console.log", wantCacheHdr: "immutable"},
		{name: "css asset", fsys: built, path: "/assets/index-abc.css", wantStatus: 200, wantType: "text/css"},
		{name: "top-level file", fsys: built, path: "/favicon.svg", wantStatus: 200, wantBody: "<svg/>"},
		{name: "missing asset is 404, not index", fsys: built, path: "/assets/gone.js", wantStatus: 404},
		{name: "route with a dotted name falls back to index", fsys: built, path: "/resources/apiextensions.k8s.io/v1/customresourcedefinitions/_/applications.argoproj.io", wantStatus: 200, wantBody: "<title>Glowpod"},
		{name: "node with a dotted name", fsys: built, path: "/nodes/ip-10-0-1-21.ec2.internal", wantStatus: 200, wantBody: "<title>Glowpod"},
		{name: "dotfiles are hidden", fsys: built, path: "/.gitkeep", wantStatus: 404},
		{name: "path traversal is cleaned", fsys: built, path: "/../index.html", wantStatus: 200, wantBody: "<title>Glowpod"},
		{name: "directory falls back to index", fsys: built, path: "/assets", wantStatus: 200, wantBody: "<title>Glowpod"},
		{name: "HEAD has no body", fsys: built, method: http.MethodHead, path: "/", wantStatus: 200},
		{name: "POST is not allowed", fsys: built, method: http.MethodPost, path: "/", wantStatus: 405},
		{name: "unbuilt frontend explains itself", fsys: empty, path: "/", wantStatus: 503, wantBody: "make build"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			rec := httptest.NewRecorder()
			Handler(tt.fsys).ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), method, tt.path, nil))

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantBody != "" && !strings.Contains(rec.Body.String(), tt.wantBody) {
				t.Fatalf("body = %q, want it to contain %q", rec.Body.String(), tt.wantBody)
			}
			if method == http.MethodHead && rec.Body.Len() != 0 {
				t.Fatalf("HEAD returned a body: %q", rec.Body.String())
			}
			if tt.wantType != "" && !strings.HasPrefix(rec.Header().Get("Content-Type"), tt.wantType) {
				t.Fatalf("Content-Type = %q, want %q", rec.Header().Get("Content-Type"), tt.wantType)
			}
			if tt.wantCacheHdr != "" && !strings.Contains(rec.Header().Get("Cache-Control"), tt.wantCacheHdr) {
				t.Fatalf("Cache-Control = %q, want it to contain %q", rec.Header().Get("Cache-Control"), tt.wantCacheHdr)
			}
		})
	}
}

func TestDistIsEmbedded(t *testing.T) {
	// The placeholder keeps `go build` working before the frontend is built.
	if _, err := Dist().Open(".gitkeep"); err != nil {
		t.Fatalf("embedded dist is missing its placeholder: %v", err)
	}
}

func TestIndexNonce(t *testing.T) {
	fsys := fstest.MapFS{"index.html": {Data: []byte(`<meta name="csp-nonce" content="` + NoncePlaceholder + `">`)}}
	h := Handler(fsys)
	nonceOf := func() (string, string) {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil))
		body := rec.Body.String()
		start := strings.Index(body, `content="`) + len(`content="`)
		return body[start : start+strings.Index(body[start:], `"`)], rec.Header().Get("Content-Security-Policy")
	}
	n1, csp := nonceOf()
	n2, _ := nonceOf()
	if n1 == NoncePlaceholder || len(n1) < 20 || n1 == n2 {
		t.Fatalf("nonces %q, %q: want fresh random values", n1, n2)
	}
	if !strings.Contains(csp, "style-src 'self' 'nonce-"+n1+"'") || !strings.Contains(csp, "script-src 'self';") {
		t.Fatalf("csp = %q", csp)
	}
	if strings.Contains(ContentSecurityPolicy(""), "nonce") {
		t.Fatal("no nonce without one")
	}
}
