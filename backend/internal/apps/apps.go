// Package apps groups workloads into the "apps" people think in: a Helm
// release, an app.kubernetes.io/part-of group, or a single workload. Glance
// mode's cards, the Apps pages and the Overview all use it.
package apps

import (
	"cmp"
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"glowpod/internal/summary"
)

// Status uses the gauge zone words: ok, high (rolling out, pending), critical.
type Status string

// App statuses.
const (
	OK       Status = "ok"
	High     Status = "high"
	Critical Status = "critical"
)

// Severity orders statuses: critical > high > ok.
func Severity(s Status) int {
	switch s {
	case Critical:
		return 2
	case High:
		return 1
	default:
		return 0
	}
}

// App is a group of workloads that belong together.
type App struct {
	Name       string     `json:"name"`
	Namespace  string     `json:"namespace"`
	Workloads  int        `json:"workloads"`
	Ready      int32      `json:"ready"`
	Total      int32      `json:"total"`
	Status     Status     `json:"status"`
	LastDeploy *time.Time `json:"lastDeploy,omitempty"`
}

// Labels and annotations that name an app, in order of preference.
const (
	helmRelease = "meta.helm.sh/release-name"
	partOf      = "app.kubernetes.io/part-of"
	appName     = "app.kubernetes.io/name"
	instance    = "app.kubernetes.io/instance"
)

// Key names the app a workload belongs to: its Helm release, then
// app.kubernetes.io/part-of, then app.kubernetes.io/name, then older
// labels, then the workload's own name.
func Key(meta *metav1.ObjectMeta) string {
	for _, v := range []string{
		meta.Annotations[helmRelease],
		meta.Labels[partOf],
		meta.Labels[appName],
		meta.Labels[instance],
		meta.Labels["app"],
		meta.Labels["k8s-app"],
	} {
		if v != "" {
			return v
		}
	}
	return meta.Name
}

// Owner names a top-level workload.
type Owner struct{ Kind, Namespace, Name string }

// Index traces pods to their top-level workload, through ReplicaSets.
type Index struct {
	rsToDeployment map[Owner]string
	newestRS       map[Owner]time.Time
}

// NewIndex indexes replicaSets by the Deployment that owns them.
func NewIndex(replicaSets []appsv1.ReplicaSet) *Index {
	ix := &Index{rsToDeployment: map[Owner]string{}, newestRS: map[Owner]time.Time{}}
	for i := range replicaSets {
		rs := &replicaSets[i]
		ref := metav1.GetControllerOf(rs)
		if ref == nil || ref.Kind != "Deployment" {
			continue
		}
		ix.rsToDeployment[Owner{"ReplicaSet", rs.Namespace, rs.Name}] = ref.Name
		dep := Owner{"Deployment", rs.Namespace, ref.Name}
		if t := rs.CreationTimestamp.Time; t.After(ix.newestRS[dep]) {
			ix.newestRS[dep] = t
		}
	}
	return ix
}

// OwnerOf returns the workload running pod, if any.
func (ix *Index) OwnerOf(pod *corev1.Pod) (Owner, bool) {
	ref := metav1.GetControllerOf(pod)
	if ref == nil {
		return Owner{}, false
	}
	o := Owner{ref.Kind, pod.Namespace, ref.Name}
	if d, ok := ix.rsToDeployment[o]; ok {
		o = Owner{"Deployment", pod.Namespace, d}
	}
	return o, true
}

// Failing returns the workloads that own a pod with a critical problem.
func Failing(pods []corev1.Pod, ix *Index, now time.Time) map[Owner]bool {
	out := map[Owner]bool{}
	for i := range pods {
		p, ok := summary.PodProblem(&pods[i], now)
		if !ok || p.Severity != summary.Critical {
			continue
		}
		if o, ok := ix.OwnerOf(&pods[i]); ok {
			out[o] = true
		}
	}
	return out
}

func replicas(p *int32) int32 {
	if p == nil {
		return 1
	}
	return *p
}

// DeploymentStatus is ready, failing (crashing pods or a stuck rollout), or rolling out.
func DeploymentStatus(d *appsv1.Deployment, failing map[Owner]bool) Status {
	want := replicas(d.Spec.Replicas)
	st := d.Status
	if st.ReadyReplicas >= want && st.UpdatedReplicas >= want && st.AvailableReplicas >= want {
		return OK
	}
	if failing[Owner{"Deployment", d.Namespace, d.Name}] {
		return Critical
	}
	for _, c := range st.Conditions {
		if (c.Type == appsv1.DeploymentProgressing && c.Status == corev1.ConditionFalse) ||
			(c.Type == appsv1.DeploymentReplicaFailure && c.Status == corev1.ConditionTrue) {
			return Critical
		}
	}
	return High
}

// StatefulSetStatus is ready, failing or pending.
func StatefulSetStatus(s *appsv1.StatefulSet, failing map[Owner]bool) Status {
	if s.Status.ReadyReplicas >= replicas(s.Spec.Replicas) {
		return OK
	}
	if failing[Owner{"StatefulSet", s.Namespace, s.Name}] {
		return Critical
	}
	return High
}

// DaemonSetStatus is ready, failing or pending.
func DaemonSetStatus(d *appsv1.DaemonSet, failing map[Owner]bool) Status {
	if d.Status.NumberReady >= d.Status.DesiredNumberScheduled {
		return OK
	}
	if failing[Owner{"DaemonSet", d.Namespace, d.Name}] {
		return Critical
	}
	return High
}

// Workload is one Deployment, StatefulSet or DaemonSet in an app.
type Workload struct {
	Kind     string   `json:"kind"`
	Group    string   `json:"group"`
	Version  string   `json:"version"`
	Resource string   `json:"resource"`
	Name     string   `json:"name"`
	Ready    int32    `json:"ready"`
	Total    int32    `json:"total"`
	Status   Status   `json:"status"`
	Images   []string `json:"images"`
	// Not sent: the app it belongs to, its namespace and last rollout.
	app       string
	namespace string
	deployed  time.Time
}

// Workloads are the objects apps are made of, in one or all namespaces.
type Workloads struct {
	Deployments  []appsv1.Deployment
	StatefulSets []appsv1.StatefulSet
	DaemonSets   []appsv1.DaemonSet
}

func images(spec corev1.PodSpec) []string {
	out := make([]string, 0, len(spec.Containers))
	for _, c := range spec.Containers {
		out = append(out, c.Image)
	}
	return out
}

// rows flattens w into workload rows with their status and app key.
func rows(w Workloads, ix *Index, failing map[Owner]bool) []Workload {
	var out []Workload
	for i := range w.Deployments {
		d := &w.Deployments[i]
		deployed := ix.newestRS[Owner{"Deployment", d.Namespace, d.Name}]
		if deployed.IsZero() {
			deployed = d.CreationTimestamp.Time
		}
		out = append(out, Workload{
			Kind: "Deployment", Group: "apps", Version: "v1", Resource: "deployments", Name: d.Name,
			Ready: d.Status.ReadyReplicas, Total: replicas(d.Spec.Replicas), Status: DeploymentStatus(d, failing),
			Images: images(d.Spec.Template.Spec), app: Key(&d.ObjectMeta), namespace: d.Namespace, deployed: deployed,
		})
	}
	for i := range w.StatefulSets {
		s := &w.StatefulSets[i]
		out = append(out, Workload{
			Kind: "StatefulSet", Group: "apps", Version: "v1", Resource: "statefulsets", Name: s.Name,
			Ready: s.Status.ReadyReplicas, Total: replicas(s.Spec.Replicas), Status: StatefulSetStatus(s, failing),
			Images: images(s.Spec.Template.Spec), app: Key(&s.ObjectMeta), namespace: s.Namespace, deployed: s.CreationTimestamp.Time,
		})
	}
	for i := range w.DaemonSets {
		d := &w.DaemonSets[i]
		out = append(out, Workload{
			Kind: "DaemonSet", Group: "apps", Version: "v1", Resource: "daemonsets", Name: d.Name,
			Ready: d.Status.NumberReady, Total: d.Status.DesiredNumberScheduled, Status: DaemonSetStatus(d, failing),
			Images: images(d.Spec.Template.Spec), app: Key(&d.ObjectMeta), namespace: d.Namespace, deployed: d.CreationTimestamp.Time,
		})
	}
	return out
}

// Group turns workloads into apps, most troubled first, then by namespace and name.
func Group(w Workloads, ix *Index, failing map[Owner]bool) []App {
	type key struct{ ns, name string }
	byKey := map[key]*App{}
	for _, wl := range rows(w, ix, failing) {
		k := key{wl.namespace, wl.app}
		a, ok := byKey[k]
		if !ok {
			a = &App{Name: wl.app, Namespace: wl.namespace, Status: OK}
			byKey[k] = a
		}
		a.Workloads++
		a.Ready += wl.Ready
		a.Total += wl.Total
		if Severity(wl.Status) > Severity(a.Status) {
			a.Status = wl.Status
		}
		if d := wl.deployed; !d.IsZero() && (a.LastDeploy == nil || d.After(*a.LastDeploy)) {
			a.LastDeploy = &d
		}
	}

	out := make([]App, 0, len(byKey))
	for _, a := range byKey {
		out = append(out, *a)
	}
	slices.SortFunc(out, func(a, b App) int {
		if c := cmp.Compare(Severity(b.Status), Severity(a.Status)); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	return out
}

// Deploy is a new Deployment revision.
type Deploy struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Revision  string    `json:"revision"`
	Image     string    `json:"image"`
	At        time.Time `json:"at"`
}

// RecentDeploys lists Deployment revisions newest first, at most limit,
// keeping only Deployments that keep(name) accepts (nil keeps all).
func RecentDeploys(replicaSets []appsv1.ReplicaSet, limit int, keep func(deployment string) bool) []Deploy {
	out := []Deploy{}
	for i := range replicaSets {
		rs := &replicaSets[i]
		ref := metav1.GetControllerOf(rs)
		rev := rs.Annotations["deployment.kubernetes.io/revision"]
		if ref == nil || ref.Kind != "Deployment" || rev == "" || (keep != nil && !keep(ref.Name)) {
			continue
		}
		var image string
		if cs := rs.Spec.Template.Spec.Containers; len(cs) > 0 {
			image = cs[0].Image
		}
		out = append(out, Deploy{Namespace: rs.Namespace, Name: ref.Name, Revision: rev, Image: image, At: rs.CreationTimestamp.Time})
	}
	slices.SortFunc(out, func(a, b Deploy) int { return b.At.Compare(a.At) })
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
