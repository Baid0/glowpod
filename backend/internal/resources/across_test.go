package resources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"testing"
)

// pagedTables serves two pages for "a", one for "b", and refuses "secret".
// calls is read only after the requests that write it have returned.
func pagedTables(t *testing.T) (*[]string, func(http.ResponseWriter, *http.Request)) {
	t.Helper()
	var mu sync.Mutex
	var calls []string
	return &calls, func(w http.ResponseWriter, r *http.Request) {
		ns := strings.Split(r.URL.Path, "/")[4] // /api/v1/namespaces/{ns}/pods
		mu.Lock()
		calls = append(calls, ns+"?"+r.URL.Query().Get("continue"))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if ns == "secret" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(`{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"Forbidden","code":403}`))
			return
		}
		page, cont := "1", ""
		if ns == "a" && r.URL.Query().Get("continue") == "" {
			cont = "a-2"
		}
		if r.URL.Query().Get("continue") == "a-2" {
			page = "2"
		}
		_, _ = fmt.Fprintf(w, `{"kind":"Table","apiVersion":"meta.k8s.io/v1","metadata":{"continue":%q},
 "columnDefinitions":[{"name":"Name","type":"string","priority":0}],
 "rows":[{"cells":["%s-%s"],"object":{"kind":"PartialObjectMetadata","apiVersion":"meta.k8s.io/v1","metadata":{"name":"%s-%s","namespace":"%s","uid":"%s%s"}}}]}`,
			cont, ns, page, ns, page, ns, ns, page)
	}
}

func names(tbl *Table) []string {
	var out []string
	for _, r := range tbl.Rows {
		out = append(out, r.Object.Name)
	}
	return out
}

func TestListTableIn(t *testing.T) {
	calls, handler := pagedTables(t)
	client := tableServer(t, handler)
	ctx := context.Background()

	first, err := ListTableIn(ctx, client, podsType, ListOptions{Limit: 10}, []string{"a", "b", "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(first); !slices.Equal(got, []string{"a-1", "b-1"}) {
		t.Fatalf("first page = %v", got)
	}
	if !slices.Equal(first.Skipped, []string{"secret"}) || first.Continue == "" || len(first.Columns) != 1 {
		t.Fatalf("skipped %v continue %q columns %v", first.Skipped, first.Continue, first.Columns)
	}

	*calls = nil
	second, err := ListTableIn(ctx, client, podsType, ListOptions{Limit: 10, Continue: first.Continue}, []string{"a", "b", "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if got := names(second); !slices.Equal(got, []string{"a-2"}) || second.Continue != "" {
		t.Fatalf("second page = %v, continue %q", got, second.Continue)
	}
	if !slices.Equal(*calls, []string{"a?a-2"}) {
		t.Fatalf("only namespaces with more rows are asked again: %v", *calls)
	}
}

func TestListTableInEdges(t *testing.T) {
	calls, handler := pagedTables(t)
	client := tableServer(t, handler)
	ctx := context.Background()

	if _, err := ListTableIn(ctx, client, podsType, ListOptions{}, []string{"secret", "secret"}); err == nil {
		t.Fatal("all namespaces forbidden is an error")
	}
	var ie *InputError
	if _, err := ListTableIn(ctx, client, podsType, ListOptions{Continue: "garbage"}, []string{"a", "b"}); !errors.As(err, &ie) {
		t.Fatalf("bad token = %v", err)
	}

	*calls = nil
	one, err := ListTableIn(ctx, client, podsType, ListOptions{}, []string{"b"})
	if err != nil || !slices.Equal(names(one), []string{"b-1"}) || !slices.Equal(*calls, []string{"b?"}) {
		t.Fatalf("one namespace is a plain list: %v %v %v", names(one), *calls, err)
	}
}
