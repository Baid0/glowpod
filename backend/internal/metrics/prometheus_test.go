package metrics

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseResult(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		want    float64
		wantErr bool
	}{
		{name: "vector sums", body: `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1,"1.5"]},{"metric":{},"value":[1,"2"]}]}}`, want: 3.5},
		{name: "empty vector is zero", body: `{"status":"success","data":{"resultType":"vector","result":[]}}`, want: 0},
		{name: "scalar", body: `{"status":"success","data":{"resultType":"scalar","result":[1,"42"]}}`, want: 42},
		{name: "NaN is zero", body: `{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"NaN"]}]}}`, want: 0},
		{name: "query error", body: `{"status":"error","error":"parse error"}`, wantErr: true},
		{name: "matrix is unexpected", body: `{"status":"success","data":{"resultType":"matrix","result":[]}}`, wantErr: true},
		{name: "not json", body: `<html>`, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseResult([]byte(tt.body))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && got != tt.want {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestServiceProxyQuery(t *testing.T) {
	var gotQuery string
	kube := apiServer(t, map[string]func(http.ResponseWriter, *http.Request){
		"/api/v1/namespaces/monitoring/services/http:prometheus-operated:9090/proxy/api/v1/query": func(w http.ResponseWriter, r *http.Request) {
			gotQuery = r.URL.Query().Get("query")
			body(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"98777.2"]}]}}`)(w, r)
		},
	})
	p := NewServiceProxy(kube, Target{Namespace: "monitoring", Service: "prometheus-operated", Port: 9090, Scheme: "http"})
	got, err := p.Query(context.Background(), "up == 1")
	if err != nil {
		t.Fatal(err)
	}
	if got != 98777.2 || gotQuery != "up == 1" {
		t.Fatalf("got %v for query %q", got, gotQuery)
	}
}

func TestURLQuery(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prom/api/v1/query" || r.URL.Query().Get("query") != "vector(7)" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[{"value":[1,"7"]}]}}`))
	}))
	defer srv.Close()

	got, err := (&URL{Base: srv.URL + "/prom/"}).Query(context.Background(), "vector(7)")
	if err != nil || got != 7 {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := (&URL{Base: srv.URL}).Query(context.Background(), "vector(7)"); err == nil {
		t.Fatal("want an error for a non-200 response")
	}
}

type fakeProm map[string]any // query → float64 or error

func (f fakeProm) Query(_ context.Context, q string) (float64, error) {
	switch v := f[q].(type) {
	case float64:
		return v, nil
	case error:
		return 0, v
	default:
		return 0, errors.New("unexpected query")
	}
}

func TestNetwork(t *testing.T) {
	tests := []struct {
		name      string
		prom      fakeProm
		wantState State
		wantValue float64
		wantMax   float64
	}{
		{name: "rate and peak", prom: fakeProm{NetworkRateQuery: 120e6, NetworkPeakQuery: 200e6}, wantState: Live, wantValue: 120e6, wantMax: 200e6},
		{name: "peak never below now", prom: fakeProm{NetworkRateQuery: 5e6, NetworkPeakQuery: 1e6}, wantState: Live, wantValue: 5e6, wantMax: 5e6},
		{name: "rate fails", prom: fakeProm{NetworkRateQuery: errors.New("down")}, wantState: Failed},
		{name: "peak fails", prom: fakeProm{NetworkRateQuery: 1.0, NetworkPeakQuery: errors.New("timeout")}, wantState: Failed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := Network(context.Background(), tt.prom)
			if g.State != tt.wantState || g.Value != tt.wantValue || g.Max != tt.wantMax || g.Source != "prometheus" {
				t.Fatalf("got %+v", g)
			}
		})
	}
	if !strings.Contains(NetworkRateQuery, `host_network="true"`) {
		t.Fatal("host-network pods must be excluded: they report the node's interfaces")
	}
}
