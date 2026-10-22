package resources

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

// tableAccept asks the API server for kubectl-style columns, so any resource
// (CRDs included) gets the same columns `kubectl get` shows.
const tableAccept = "application/json;as=Table;g=meta.k8s.io;v=v1, application/json"

// Paging limits for lists.
const (
	DefaultLimit = 100
	MaxLimit     = 500
)

// ListOptions narrows a list. Namespace "" means all namespaces.
type ListOptions struct {
	Namespace     string
	LabelSelector string
	FieldSelector string
	Continue      string
	Limit         int64
}

// Column is a Table column. Priority 0 columns are what `kubectl get` shows;
// higher priorities are the `-o wide` extras.
type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Format      string `json:"format,omitempty"`
	Description string `json:"description,omitempty"`
	Priority    int32  `json:"priority"`
}

// RowObject identifies the object behind a row.
type RowObject struct {
	Name              string            `json:"name"`
	Namespace         string            `json:"namespace,omitempty"`
	UID               string            `json:"uid"`
	CreationTimestamp *time.Time        `json:"creationTimestamp,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
}

// Row is one object's cells, in column order.
type Row struct {
	Cells  []any     `json:"cells"`
	Object RowObject `json:"object"`
}

// Table is one page of a list.
type Table struct {
	Type               Type     `json:"type"`
	Columns            []Column `json:"columns"`
	Rows               []Row    `json:"rows"`
	Continue           string   `json:"continue,omitempty"`
	ResourceVersion    string   `json:"resourceVersion,omitempty"`
	RemainingItemCount *int64   `json:"remainingItemCount,omitempty"`
	// Skipped lists picked namespaces the user may not list, when others could be.
	Skipped []string `json:"skipped,omitempty"`
}

// Path is the REST path for t, in a namespace when t is namespaced and ns is set.
func Path(t Type, namespace string) string {
	base := "/apis/" + t.Group + "/" + t.Version
	if t.Group == "" {
		base = "/api/" + t.Version
	}
	if t.Namespaced && namespace != "" {
		return base + "/namespaces/" + namespace + "/" + t.Resource
	}
	return base + "/" + t.Resource
}

// ListTable fetches one page of t as a Table, with each row's metadata.
func ListTable(ctx context.Context, client rest.Interface, t Type, opts ListOptions) (*Table, error) {
	limit := opts.Limit
	if limit <= 0 || limit > MaxLimit {
		limit = DefaultLimit
	}
	req := client.Get().
		AbsPath(Path(t, opts.Namespace)).
		SetHeader("Accept", tableAccept).
		Param("includeObject", "Metadata").
		Param("limit", fmt.Sprint(limit))
	for k, v := range map[string]string{
		"labelSelector": opts.LabelSelector,
		"fieldSelector": opts.FieldSelector,
		"continue":      opts.Continue,
	} {
		if v != "" {
			req = req.Param(k, v)
		}
	}
	raw, err := req.DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", t.Resource, err)
	}

	var table metav1.Table
	if err := json.Unmarshal(raw, &table); err != nil {
		return nil, fmt.Errorf("decode %s table: %w", t.Resource, err)
	}
	if table.Kind != "Table" {
		return nil, fmt.Errorf("list %s: the API server didn't return a table (kind %q)", t.Resource, table.Kind)
	}

	out := &Table{
		Type:               t,
		Columns:            make([]Column, 0, len(table.ColumnDefinitions)),
		Rows:               make([]Row, 0, len(table.Rows)),
		Continue:           table.Continue,
		ResourceVersion:    table.ResourceVersion,
		RemainingItemCount: table.RemainingItemCount,
	}
	for _, c := range table.ColumnDefinitions {
		out.Columns = append(out.Columns, Column{
			Name: c.Name, Type: c.Type, Format: c.Format, Description: c.Description, Priority: c.Priority,
		})
	}
	for _, r := range table.Rows {
		row := Row{Cells: r.Cells}
		var meta metav1.PartialObjectMetadata
		if len(r.Object.Raw) > 0 && json.Unmarshal(r.Object.Raw, &meta) == nil {
			row.Object = RowObject{
				Name:      meta.Name,
				Namespace: meta.Namespace,
				UID:       string(meta.UID),
				Labels:    meta.Labels,
			}
			if !meta.CreationTimestamp.IsZero() {
				ts := meta.CreationTimestamp.UTC()
				row.Object.CreationTimestamp = &ts
			}
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}
