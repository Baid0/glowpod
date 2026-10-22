// Package topology builds a namespace's resource map: how traffic enters
// (Ingress → Service), what runs (workloads), and what they use (ConfigMaps,
// Secrets, volume claims).
package topology

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/apps"
	"glowpod/internal/paging"
)

// MaxNodes keeps the map readable and the response small.
const MaxNodes = 400

// Column is where a node sits on the map, left to right.
type Column int

// Columns of the map.
const (
	Entry    Column = iota // Ingresses
	Services               // Services
	Runs                   // workloads
	Uses                   // ConfigMaps, Secrets, claims
)

// Node is one object on the map.
type Node struct {
	ID        string `json:"id"`
	Column    Column `json:"column"`
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	// Status is ok, high, critical or neutral, as on status pills.
	Status string `json:"status"`
	// Detail is one short line: "3/3 ready", "LoadBalancer", "Bound".
	Detail string `json:"detail,omitempty"`
	// Missing is true for something a workload refers to that doesn't exist.
	Missing bool `json:"missing,omitempty"`
}

// Edge points from one node to another, left to right.
type Edge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// Map is GET /api/v1/clusters/{cluster}/map?namespace=.
type Map struct {
	Namespace string `json:"namespace"`
	Nodes     []Node `json:"nodes"`
	Edges     []Edge `json:"edges"`
	// Truncated is set when the namespace has more than MaxNodes objects.
	Truncated bool `json:"truncated"`
}

func id(kind, name string) string { return kind + "/" + name }

type workload struct {
	node     Node
	template corev1.PodTemplateSpec
}

// Build reads the namespace and connects what it finds.
func Build(ctx context.Context, kube kubernetes.Interface, namespace string, now time.Time) (*Map, error) {
	var (
		snap     *apps.Snapshot
		services []corev1.Service
		ingress  []networkingv1.Ingress
		claims   []corev1.PersistentVolumeClaim
		maps     []corev1.ConfigMap
		// Whether the optional lists could be read; if not, nothing is marked missing.
		claimsOK, mapsOK bool
	)
	g, gctx := errgroup.WithContext(ctx)
	g.Go(func() (err error) { snap, err = apps.Load(gctx, kube, namespace); return err })
	g.Go(func() (err error) {
		services, err = paging.All(gctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Service, string, error) {
			l, err := kube.CoreV1().Services(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return err
	})
	// Optional reads: a user may be allowed to see workloads but not these.
	g.Go(func() error {
		ingress, _ = paging.All(gctx, func(ctx context.Context, o metav1.ListOptions) ([]networkingv1.Ingress, string, error) {
			l, err := kube.NetworkingV1().Ingresses(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		return nil
	})
	g.Go(func() error {
		var err error
		claims, err = paging.All(gctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.PersistentVolumeClaim, string, error) {
			l, err := kube.CoreV1().PersistentVolumeClaims(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		claimsOK = err == nil
		return nil
	})
	g.Go(func() error {
		// ConfigMap names only, to mark references to ones that don't exist.
		// Secrets aren't listed: that would read their values.
		var err error
		maps, err = paging.All(gctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.ConfigMap, string, error) {
			l, err := kube.CoreV1().ConfigMaps(namespace).List(ctx, o)
			if err != nil {
				return nil, "", err
			}
			return l.Items, l.Continue, nil
		})
		mapsOK = err == nil
		return nil
	})
	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("read namespace %s: %w", namespace, err)
	}

	m := &Map{Namespace: namespace, Nodes: []Node{}, Edges: []Edge{}}
	nodes := map[string]bool{}
	add := func(n Node) {
		if nodes[n.ID] {
			return
		}
		if len(m.Nodes) >= MaxNodes {
			m.Truncated = true
			return
		}
		nodes[n.ID] = true
		m.Nodes = append(m.Nodes, n)
	}
	link := func(from, to string) {
		if nodes[from] && nodes[to] {
			m.Edges = append(m.Edges, Edge{From: from, To: to})
		}
	}

	ix := apps.NewIndex(snap.ReplicaSets)
	failing := apps.Failing(snap.Pods, ix, now)
	workloads := collect(snap, failing)
	for _, w := range workloads {
		add(w.node)
	}

	for i := range services {
		s := &services[i]
		sid := id("Service", s.Name)
		add(Node{ID: sid, Column: Services, Kind: "Service", Namespace: s.Namespace, Name: s.Name, Status: "neutral", Detail: string(s.Spec.Type)})
		if len(s.Spec.Selector) == 0 {
			continue
		}
		sel := labels.SelectorFromSet(s.Spec.Selector)
		matched := 0
		for _, w := range workloads {
			if sel.Matches(labels.Set(w.template.Labels)) {
				link(sid, w.node.ID)
				matched++
			}
		}
		if matched == 0 {
			m.Nodes[slices.IndexFunc(m.Nodes, func(n Node) bool { return n.ID == sid })].Status = "high"
			setDetail(m, sid, string(s.Spec.Type)+" · no pods match")
		}
	}

	for i := range ingress {
		in := &ingress[i]
		iid := id("Ingress", in.Name)
		add(Node{ID: iid, Column: Entry, Kind: "Ingress", Namespace: in.Namespace, Name: in.Name, Status: "neutral", Detail: hosts(in)})
		for _, svc := range backends(in) {
			sid := id("Service", svc)
			if !nodes[sid] {
				add(Node{ID: sid, Column: Services, Kind: "Service", Namespace: in.Namespace, Name: svc, Status: "critical", Detail: "doesn't exist", Missing: true})
			}
			link(iid, sid)
		}
	}

	claimStatus := map[string]corev1.PersistentVolumeClaimPhase{}
	for i := range claims {
		claimStatus[claims[i].Name] = claims[i].Status.Phase
	}
	haveMap := map[string]bool{}
	for i := range maps {
		haveMap[maps[i].Name] = true
	}
	for _, w := range workloads {
		for _, ref := range uses(w.template.Spec) {
			n := Node{ID: id(ref.kind, ref.name), Column: Uses, Kind: ref.kind, Namespace: w.node.Namespace, Name: ref.name, Status: "neutral"}
			switch ref.kind {
			case "PersistentVolumeClaim":
				phase, ok := claimStatus[ref.name]
				n.Detail, n.Status = string(phase), claimTone(phase)
				if !ok && claimsOK {
					n.Detail, n.Status, n.Missing = "doesn't exist", "critical", !ref.optional
				}
			case "ConfigMap":
				if mapsOK && !haveMap[ref.name] && !ref.optional {
					n.Detail, n.Status, n.Missing = "doesn't exist", "critical", true
				}
			}
			add(n)
			link(w.node.ID, n.ID)
		}
	}

	slices.SortStableFunc(m.Nodes, func(a, b Node) int {
		return cmp.Or(cmp.Compare(a.Column, b.Column), cmp.Compare(a.Kind, b.Kind), cmp.Compare(a.Name, b.Name))
	})
	return m, nil
}

func setDetail(m *Map, nodeID, detail string) {
	for i := range m.Nodes {
		if m.Nodes[i].ID == nodeID {
			m.Nodes[i].Detail = detail
		}
	}
}

func collect(snap *apps.Snapshot, failing map[apps.Owner]bool) []workload {
	var out []workload
	for i := range snap.Workloads.Deployments {
		d := &snap.Workloads.Deployments[i]
		out = append(out, workload{node: workloadNode("Deployment", d.ObjectMeta, string(apps.DeploymentStatus(d, failing)),
			ready(d.Status.ReadyReplicas, replicas(d.Spec.Replicas))), template: d.Spec.Template})
	}
	for i := range snap.Workloads.StatefulSets {
		s := &snap.Workloads.StatefulSets[i]
		out = append(out, workload{node: workloadNode("StatefulSet", s.ObjectMeta, string(apps.StatefulSetStatus(s, failing)),
			ready(s.Status.ReadyReplicas, replicas(s.Spec.Replicas))), template: s.Spec.Template})
	}
	for i := range snap.Workloads.DaemonSets {
		d := &snap.Workloads.DaemonSets[i]
		out = append(out, workload{node: workloadNode("DaemonSet", d.ObjectMeta, string(apps.DaemonSetStatus(d, failing)),
			ready(d.Status.NumberReady, d.Status.DesiredNumberScheduled)), template: d.Spec.Template})
	}
	return out
}

func workloadNode(kind string, meta metav1.ObjectMeta, status, detail string) Node {
	return Node{ID: id(kind, meta.Name), Column: Runs, Kind: kind, Namespace: meta.Namespace, Name: meta.Name, Status: status, Detail: detail}
}

func replicas(r *int32) int32 {
	if r == nil {
		return 1
	}
	return *r
}

func ready(have, want int32) string { return fmt.Sprintf("%d/%d ready", have, want) }

func claimTone(p corev1.PersistentVolumeClaimPhase) string {
	switch p {
	case corev1.ClaimBound:
		return "ok"
	case corev1.ClaimPending:
		return "high"
	case corev1.ClaimLost:
		return "critical"
	default:
		return "neutral"
	}
}

func hosts(in *networkingv1.Ingress) string {
	var hs []string
	for _, r := range in.Spec.Rules {
		if r.Host != "" && !slices.Contains(hs, r.Host) {
			hs = append(hs, r.Host)
		}
	}
	switch len(hs) {
	case 0:
		return "any host"
	case 1:
		return hs[0]
	default:
		return fmt.Sprintf("%s +%d", hs[0], len(hs)-1)
	}
}

func backends(in *networkingv1.Ingress) []string {
	var out []string
	addSvc := func(b *networkingv1.IngressBackend) {
		if b != nil && b.Service != nil && !slices.Contains(out, b.Service.Name) {
			out = append(out, b.Service.Name)
		}
	}
	addSvc(in.Spec.DefaultBackend)
	for _, r := range in.Spec.Rules {
		if r.HTTP == nil {
			continue
		}
		for i := range r.HTTP.Paths {
			addSvc(&r.HTTP.Paths[i].Backend)
		}
	}
	return out
}

type ref struct {
	kind, name string
	optional   bool
}

// uses lists the ConfigMaps, Secrets and claims a pod template refers to.
func uses(spec corev1.PodSpec) []ref {
	var out []ref
	seen := map[string]bool{}
	addRef := func(kind, name string, optional *bool) {
		if name == "" || seen[kind+"/"+name] {
			return
		}
		seen[kind+"/"+name] = true
		out = append(out, ref{kind: kind, name: name, optional: optional != nil && *optional})
	}
	for _, v := range spec.Volumes {
		switch {
		case v.ConfigMap != nil:
			addRef("ConfigMap", v.ConfigMap.Name, v.ConfigMap.Optional)
		case v.Secret != nil:
			addRef("Secret", v.Secret.SecretName, v.Secret.Optional)
		case v.PersistentVolumeClaim != nil:
			addRef("PersistentVolumeClaim", v.PersistentVolumeClaim.ClaimName, nil)
		case v.Projected != nil:
			for _, s := range v.Projected.Sources {
				if s.ConfigMap != nil {
					addRef("ConfigMap", s.ConfigMap.Name, s.ConfigMap.Optional)
				}
				if s.Secret != nil {
					addRef("Secret", s.Secret.Name, s.Secret.Optional)
				}
			}
		}
	}
	containers := slices.Concat(spec.InitContainers, spec.Containers)
	for _, c := range containers {
		for _, e := range c.EnvFrom {
			if e.ConfigMapRef != nil {
				addRef("ConfigMap", e.ConfigMapRef.Name, e.ConfigMapRef.Optional)
			}
			if e.SecretRef != nil {
				addRef("Secret", e.SecretRef.Name, e.SecretRef.Optional)
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom == nil {
				continue
			}
			if r := e.ValueFrom.ConfigMapKeyRef; r != nil {
				addRef("ConfigMap", r.Name, r.Optional)
			}
			if r := e.ValueFrom.SecretKeyRef; r != nil {
				addRef("Secret", r.Name, r.Optional)
			}
		}
	}
	return out
}
