// Package helm reads Helm 3 releases from the Secrets Helm stores them in.
//
// A release Secret holds the whole release: chart, values and rendered
// manifest, which often include passwords. This package decodes it on the
// server and keeps only what's safe to show: chart name and version,
// status, dates, and the kind and name of each object it installed. Values
// and manifest contents never leave this package.
package helm

import (
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"

	"glowpod/internal/paging"
)

const (
	secretType = "helm.sh/release.v1" //nolint:gosec // a Secret type name, not a credential
	// maxRelease bounds a decompressed release (Helm caps Secrets at 1 MiB compressed).
	maxRelease = 32 << 20
)

// Release is one revision of a Helm release, without its values.
type Release struct {
	Name         string    `json:"name"`
	Namespace    string    `json:"namespace"`
	Revision     int       `json:"revision"`
	Status       string    `json:"status"`
	Chart        string    `json:"chart"`
	ChartVersion string    `json:"chartVersion"`
	AppVersion   string    `json:"appVersion,omitempty"`
	Updated      time.Time `json:"updated"`
	Description  string    `json:"description,omitempty"`
}

// Object is something a release installed.
type Object struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

// Detail is one release: its latest revision, history and objects.
type Detail struct {
	Release
	History []Release `json:"history"`
	Objects []Object  `json:"objects"`
}

// stored mirrors the fields Glowpod reads from Helm's release JSON. Fields
// it doesn't name (config, the chart's values and templates) are dropped
// by the decoder.
type stored struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Version   int    `json:"version"`
	Info      struct {
		Status        string    `json:"status"`
		Description   string    `json:"description"`
		LastDeployed  time.Time `json:"last_deployed"`
		FirstDeployed time.Time `json:"first_deployed"`
	} `json:"info"`
	Chart struct {
		Metadata struct {
			Name       string `json:"name"`
			Version    string `json:"version"`
			AppVersion string `json:"appVersion"`
		} `json:"metadata"`
	} `json:"chart"`
	Manifest string `json:"manifest"`
}

// ErrNotFound means no Secret holds a release by that name.
var ErrNotFound = errors.New("helm release not found")

// List returns the latest revision of every release in namespace ("" for all).
func List(ctx context.Context, kube kubernetes.Interface, namespace string) ([]Release, error) {
	secrets, err := releaseSecrets(ctx, kube, namespace, "")
	if err != nil {
		return nil, err
	}
	latest := map[string]Release{}
	for i := range secrets {
		r, ok := fromLabels(&secrets[i])
		if !ok {
			continue
		}
		key := r.Namespace + "/" + r.Name
		if cur, seen := latest[key]; seen && cur.Revision >= r.Revision {
			continue
		}
		latest[key] = r
	}
	out := make([]Release, 0, len(latest))
	for key, r := range latest {
		// Labels don't carry the chart; decode the latest revision for it.
		for i := range secrets {
			s := &secrets[i]
			if s.Namespace+"/"+s.Labels["name"] == key && s.Labels["version"] == strconv.Itoa(r.Revision) {
				if st, err := decode(s); err == nil {
					r = summary(st, s.Namespace)
				}
				break
			}
		}
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b Release) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})
	return out, nil
}

// Get returns one release with its history and the objects it installed.
func Get(ctx context.Context, kube kubernetes.Interface, namespace, name string) (*Detail, error) {
	secrets, err := releaseSecrets(ctx, kube, namespace, name)
	if err != nil {
		return nil, err
	}
	if len(secrets) == 0 {
		return nil, ErrNotFound
	}
	var latest *stored
	var history []Release
	for i := range secrets {
		st, err := decode(&secrets[i])
		if err != nil {
			continue
		}
		history = append(history, summary(st, namespace))
		if latest == nil || st.Version > latest.Version {
			latest = st
		}
	}
	if latest == nil {
		return nil, fmt.Errorf("decode release %s/%s: no readable revision", namespace, name)
	}
	slices.SortFunc(history, func(a, b Release) int { return cmp.Compare(b.Revision, a.Revision) })
	return &Detail{
		Release: summary(latest, namespace),
		History: history,
		Objects: objects(latest.Manifest, namespace),
	}, nil
}

func releaseSecrets(ctx context.Context, kube kubernetes.Interface, namespace, name string) ([]corev1.Secret, error) {
	selector := "owner=helm"
	if name != "" {
		selector += ",name=" + name
	}
	secrets, err := paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Secret, string, error) {
		o.LabelSelector = selector
		o.FieldSelector = "type=" + secretType
		list, err := kube.CoreV1().Secrets(namespace).List(ctx, o)
		if err != nil {
			return nil, "", fmt.Errorf("list helm releases: %w", err)
		}
		return list.Items, list.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	return secrets, nil
}

func fromLabels(s *corev1.Secret) (Release, bool) {
	rev, err := strconv.Atoi(s.Labels["version"])
	if err != nil || s.Labels["name"] == "" {
		return Release{}, false
	}
	r := Release{Name: s.Labels["name"], Namespace: s.Namespace, Revision: rev, Status: s.Labels["status"]}
	if ts, err := strconv.ParseInt(s.Labels["modifiedAt"], 10, 64); err == nil {
		r.Updated = time.Unix(ts, 0).UTC()
	} else {
		r.Updated = s.CreationTimestamp.UTC()
	}
	return r, true
}

// decode undoes Helm's encoding: base64, then gzip, then JSON.
func decode(s *corev1.Secret) (*stored, error) {
	raw, err := base64.StdEncoding.DecodeString(string(s.Data["release"]))
	if err != nil {
		return nil, fmt.Errorf("release %s: %w", s.Name, err)
	}
	if bytes.HasPrefix(raw, []byte{0x1f, 0x8b}) {
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return nil, fmt.Errorf("release %s: %w", s.Name, err)
		}
		raw, err = io.ReadAll(io.LimitReader(zr, maxRelease))
		if err != nil {
			return nil, fmt.Errorf("release %s: %w", s.Name, err)
		}
	}
	var st stored
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, fmt.Errorf("release %s: %w", s.Name, err)
	}
	return &st, nil
}

func summary(st *stored, namespace string) Release {
	ns := st.Namespace
	if ns == "" {
		ns = namespace
	}
	updated := st.Info.LastDeployed
	if updated.IsZero() {
		updated = st.Info.FirstDeployed
	}
	return Release{
		Name: st.Name, Namespace: ns, Revision: st.Version, Status: st.Info.Status,
		Chart: st.Chart.Metadata.Name, ChartVersion: st.Chart.Metadata.Version, AppVersion: st.Chart.Metadata.AppVersion,
		Updated: updated.UTC(), Description: st.Info.Description,
	}
}

var docSplit = regexp.MustCompile(`(?m)^---.*$`)

// objects lists the kind and name of each manifest document. It reads
// nothing else from them: rendered Secrets carry real values.
func objects(manifest, namespace string) []Object {
	out := []Object{}
	for _, doc := range docSplit.Split(manifest, -1) {
		if strings.TrimSpace(doc) == "" {
			continue
		}
		var head struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
			Metadata   struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
		}
		if err := yaml.Unmarshal([]byte(doc), &head); err != nil || head.Kind == "" || head.Metadata.Name == "" {
			continue
		}
		o := Object{APIVersion: head.APIVersion, Kind: head.Kind, Name: head.Metadata.Name, Namespace: head.Metadata.Namespace}
		if o.Namespace == "" && !clusterScoped[o.Kind] {
			o.Namespace = namespace
		}
		out = append(out, o)
	}
	slices.SortFunc(out, func(a, b Object) int { return cmp.Or(cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name)) })
	return out
}

// clusterScoped kinds a chart commonly installs.
var clusterScoped = map[string]bool{
	"Namespace": true, "ClusterRole": true, "ClusterRoleBinding": true, "CustomResourceDefinition": true,
	"StorageClass": true, "PersistentVolume": true, "PriorityClass": true, "IngressClass": true,
	"ValidatingWebhookConfiguration": true, "MutatingWebhookConfiguration": true, "APIService": true,
}
