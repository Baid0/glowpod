// Package nodes builds the node page and changes a node's scheduling:
// cordon, uncordon and drain.
package nodes

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/metrics"
	"glowpod/internal/paging"
	"glowpod/internal/summary"
)

// UsageSource reads node usage. *metrics.Reader implements it.
type UsageSource interface {
	NodeUsage(ctx context.Context) (map[string]metrics.Usage, error)
}

// Detail is GET /api/v1/clusters/{cluster}/nodes/{name}.
type Detail struct {
	Name          string            `json:"name"`
	Ready         bool              `json:"ready"`
	Unschedulable bool              `json:"unschedulable"`
	Roles         []string          `json:"roles"`
	CreatedAt     time.Time         `json:"createdAt"`
	Conditions    []Condition       `json:"conditions"`
	Taints        []Taint           `json:"taints"`
	Addresses     []Address         `json:"addresses"`
	Info          Info              `json:"info"`
	Gauges        Gauges            `json:"gauges"`
	Pods          []summary.Pod     `json:"pods"`
	Events        []summary.Event   `json:"events"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// Condition is one node condition. Problem is true when it's bad news:
// not Ready, or under memory, disk or PID pressure.
type Condition struct {
	Type    string    `json:"type"`
	Status  string    `json:"status"`
	Reason  string    `json:"reason,omitempty"`
	Message string    `json:"message,omitempty"`
	Since   time.Time `json:"since"`
	Problem bool      `json:"problem"`
}

// Taint keeps pods without a matching toleration off the node.
type Taint struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Effect string `json:"effect"`
}

// Address is one of the node's addresses.
type Address struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

// Info is what the kubelet reports about the machine.
type Info struct {
	KubeletVersion   string `json:"kubeletVersion"`
	OSImage          string `json:"osImage"`
	KernelVersion    string `json:"kernelVersion"`
	ContainerRuntime string `json:"containerRuntime"`
	Architecture     string `json:"architecture"`
}

// Gauges are this node's CPU, memory and pod capacity, like the Overview's.
type Gauges struct {
	CPU    metrics.Gauge `json:"cpu"`
	Memory metrics.Gauge `json:"memory"`
	Pods   metrics.Gauge `json:"pods"`
}

const rolePrefix = "node-role.kubernetes.io/"

// Build reads the node, its pods, usage and events. Only the node read is
// required; the rest degrade quietly.
func Build(ctx context.Context, kube kubernetes.Interface, usage UsageSource, name string, now time.Time) (*Detail, error) {
	node, err := kube.CoreV1().Nodes().Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get node %s: %w", name, err)
	}
	pods, _ := PodsOn(ctx, kube, name)

	d := &Detail{
		Name:          node.Name,
		Ready:         isReady(node),
		Unschedulable: node.Spec.Unschedulable,
		Roles:         roles(node),
		CreatedAt:     node.CreationTimestamp.UTC(),
		Conditions:    conditions(node),
		Taints:        []Taint{},
		Addresses:     []Address{},
		Info: Info{
			KubeletVersion:   node.Status.NodeInfo.KubeletVersion,
			OSImage:          node.Status.NodeInfo.OSImage,
			KernelVersion:    node.Status.NodeInfo.KernelVersion,
			ContainerRuntime: node.Status.NodeInfo.ContainerRuntimeVersion,
			Architecture:     node.Status.NodeInfo.Architecture,
		},
		Pods:   make([]summary.Pod, 0, len(pods)),
		Events: []summary.Event{},
		Labels: node.Labels,
	}
	for _, t := range node.Spec.Taints {
		d.Taints = append(d.Taints, Taint{Key: t.Key, Value: t.Value, Effect: string(t.Effect)})
	}
	for _, a := range node.Status.Addresses {
		d.Addresses = append(d.Addresses, Address{Type: string(a.Type), Address: a.Address})
	}

	var usageMap map[string]metrics.Usage
	usageErr := metrics.ErrNoMetrics
	if usage != nil {
		usageMap, usageErr = usage.NodeUsage(ctx)
	}
	one := []corev1.Node{*node}
	d.Gauges.CPU, d.Gauges.Memory = metrics.CPUAndMemory(one, pods, usageMap, usageErr)
	d.Gauges.Pods = metrics.Pods(one, pods)

	for i := range pods {
		d.Pods = append(d.Pods, summary.PodOf(&pods[i], now))
	}
	slices.SortFunc(d.Pods, func(a, b summary.Pod) int {
		return cmp.Or(cmp.Compare(a.Namespace, b.Namespace), cmp.Compare(a.Name, b.Name))
	})

	if list, err := kube.CoreV1().Events("").List(ctx, metav1.ListOptions{
		FieldSelector: fields.Set{"involvedObject.kind": "Node", "involvedObject.name": name}.AsSelector().String(),
		Limit:         200,
	}); err == nil {
		for i := range list.Items {
			d.Events = append(d.Events, summary.EventOf(&list.Items[i]))
		}
		slices.SortFunc(d.Events, func(a, b summary.Event) int { return b.Time.Compare(a.Time) })
	}
	return d, nil
}

// PodsOn lists every pod scheduled to node, in all namespaces.
func PodsOn(ctx context.Context, kube kubernetes.Interface, node string) ([]corev1.Pod, error) {
	selector := fields.OneTermEqualSelector("spec.nodeName", node).String()
	pods, err := paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]corev1.Pod, string, error) {
		o.FieldSelector = selector
		list, err := kube.CoreV1().Pods("").List(ctx, o)
		if err != nil {
			return nil, "", fmt.Errorf("list pods on %s: %w", node, err)
		}
		return list.Items, list.Continue, nil
	})
	if err != nil {
		return nil, err
	}
	return pods, nil
}

func isReady(n *corev1.Node) bool {
	for _, c := range n.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

func roles(n *corev1.Node) []string {
	out := []string{}
	for k := range n.Labels {
		if r, ok := strings.CutPrefix(k, rolePrefix); ok && r != "" {
			out = append(out, r)
		}
	}
	slices.Sort(out)
	return out
}

func conditions(n *corev1.Node) []Condition {
	out := make([]Condition, 0, len(n.Status.Conditions))
	for _, c := range n.Status.Conditions {
		problem := c.Status == corev1.ConditionTrue
		if c.Type == corev1.NodeReady {
			problem = c.Status != corev1.ConditionTrue
		}
		out = append(out, Condition{
			Type: string(c.Type), Status: string(c.Status), Reason: c.Reason, Message: c.Message,
			Since: c.LastTransitionTime.UTC(), Problem: problem,
		})
	}
	// Ready first, then problems, then the rest by name.
	rank := func(c Condition) int {
		switch {
		case c.Type == string(corev1.NodeReady):
			return 0
		case c.Problem:
			return 1
		default:
			return 2
		}
	}
	slices.SortFunc(out, func(a, b Condition) int {
		return cmp.Or(cmp.Compare(rank(a), rank(b)), cmp.Compare(a.Type, b.Type))
	})
	return out
}
