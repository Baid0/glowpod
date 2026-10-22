package resources

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"sync"

	"golang.org/x/sync/errgroup"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
)

// ListTableIn lists one page of t in several namespaces at once. With one
// namespace (or "" for all) it is ListTable. With more, it reads up to
// opts.Limit rows from each namespace that still has some and joins them;
// the continue token remembers where each namespace stopped. Namespaces the
// user may not list are skipped (and named) as long as another one works.
func ListTableIn(ctx context.Context, client rest.Interface, t Type, opts ListOptions, namespaces []string) (*Table, error) {
	if len(namespaces) <= 1 || !t.Namespaced {
		if len(namespaces) == 1 && t.Namespaced {
			opts.Namespace = namespaces[0]
		}
		return ListTable(ctx, client, t, opts)
	}

	pending := map[string]string{}
	if opts.Continue == "" {
		for _, ns := range namespaces {
			pending[ns] = ""
		}
	} else {
		saved, err := decodeContinue(opts.Continue)
		if err != nil {
			return nil, err
		}
		for ns, c := range saved {
			if slices.Contains(namespaces, ns) {
				pending[ns] = c
			}
		}
	}

	var (
		mu      sync.Mutex
		tables  = map[string]*Table{}
		skipped []string
		denied  error
		g, gctx = errgroup.WithContext(ctx)
		ordered = make([]string, 0, len(pending))
	)
	for ns := range pending {
		ordered = append(ordered, ns)
	}
	slices.Sort(ordered)
	for _, ns := range ordered {
		o := opts
		o.Namespace, o.Continue = ns, pending[ns]
		g.Go(func() error {
			tbl, err := ListTable(gctx, client, t, o)
			mu.Lock()
			defer mu.Unlock()
			switch {
			case apierrors.IsForbidden(err):
				skipped, denied = append(skipped, ns), err
				return nil
			case err != nil:
				return err
			}
			tables[ns] = tbl
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err //nolint:wrapcheck // ListTable already says what it listed
	}
	if len(tables) == 0 && denied != nil {
		return nil, denied
	}

	out := &Table{Type: t, Rows: []Row{}}
	next := map[string]string{}
	var remaining int64
	counted := true
	for _, ns := range ordered {
		tbl, ok := tables[ns]
		if !ok {
			continue
		}
		if out.Columns == nil {
			out.Columns = tbl.Columns
		}
		out.Rows = append(out.Rows, tbl.Rows...)
		if tbl.Continue != "" {
			next[ns] = tbl.Continue
		}
		if tbl.RemainingItemCount != nil {
			remaining += *tbl.RemainingItemCount
		} else if tbl.Continue != "" {
			counted = false
		}
	}
	if out.Columns == nil {
		out.Columns = []Column{}
	}
	if len(next) > 0 {
		out.Continue = encodeContinue(next)
		if counted {
			out.RemainingItemCount = &remaining
		}
	}
	slices.Sort(skipped)
	out.Skipped = skipped
	return out, nil
}

// The continue token for several namespaces: each one's own token, so the
// next page carries on where each stopped. Opaque to the browser.
func encodeContinue(next map[string]string) string {
	raw, _ := json.Marshal(next) //nolint:errchkjson // a map of strings always encodes
	return "ns:" + base64.RawURLEncoding.EncodeToString(raw)
}

func decodeContinue(token string) (map[string]string, error) {
	bad := inputErr("bad_continue", "That page link has expired. Reload the list.")
	if len(token) < 3 || token[:3] != "ns:" {
		return nil, bad
	}
	raw, err := base64.RawURLEncoding.DecodeString(token[3:])
	if err != nil {
		return nil, bad
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, bad
	}
	return m, nil
}
