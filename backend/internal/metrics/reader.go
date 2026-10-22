// Package metrics reads usage from metrics-server, the kubelet and
// Prometheus, and turns it into the Overview gauges.
package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// ErrNoMetrics means metrics-server (metrics.k8s.io) isn't installed or isn't serving.
var ErrNoMetrics = errors.New("metrics.k8s.io is not available")

// Usage is CPU in cores and memory in bytes.
type Usage struct {
	CPU    float64
	Memory float64
}

// PodUsage is one pod's summed container usage.
type PodUsage struct {
	Namespace string
	Name      string
	Usage
}

// PVCKey identifies a PersistentVolumeClaim.
type PVCKey struct {
	Namespace string
	Name      string
}

// Reader reads usage through the Kubernetes API server, as the client's identity.
type Reader struct {
	rest rest.Interface
}

// NewReader returns a Reader using kube's credentials.
func NewReader(kube kubernetes.Interface) *Reader {
	return &Reader{rest: kube.CoreV1().RESTClient()}
}

type metricsList struct {
	Items []struct {
		Metadata struct {
			Name      string `json:"name"`
			Namespace string `json:"namespace"`
		} `json:"metadata"`
		Usage      map[string]string `json:"usage"`
		Containers []struct {
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	} `json:"items"`
}

// NodeUsage returns usage per node name.
func (r *Reader) NodeUsage(ctx context.Context) (map[string]Usage, error) {
	var list metricsList
	if err := r.getMetrics(ctx, "/apis/metrics.k8s.io/v1beta1/nodes", &list); err != nil {
		return nil, err
	}
	out := make(map[string]Usage, len(list.Items))
	for _, item := range list.Items {
		out[item.Metadata.Name] = parseUsage(item.Usage)
	}
	return out, nil
}

// PodUsage returns usage per pod in namespace ("" = all namespaces).
func (r *Reader) PodUsage(ctx context.Context, namespace string) ([]PodUsage, error) {
	path := "/apis/metrics.k8s.io/v1beta1/pods"
	if namespace != "" {
		path = "/apis/metrics.k8s.io/v1beta1/namespaces/" + namespace + "/pods"
	}
	var list metricsList
	if err := r.getMetrics(ctx, path, &list); err != nil {
		return nil, err
	}
	out := make([]PodUsage, 0, len(list.Items))
	for _, item := range list.Items {
		var total Usage
		for _, c := range item.Containers {
			u := parseUsage(c.Usage)
			total.CPU += u.CPU
			total.Memory += u.Memory
		}
		out = append(out, PodUsage{Namespace: item.Metadata.Namespace, Name: item.Metadata.Name, Usage: total})
	}
	return out, nil
}

// ContainerUsage returns one pod's usage per container name.
func (r *Reader) ContainerUsage(ctx context.Context, namespace, name string) (map[string]Usage, error) {
	var item struct {
		Containers []struct {
			Name  string            `json:"name"`
			Usage map[string]string `json:"usage"`
		} `json:"containers"`
	}
	path := "/apis/metrics.k8s.io/v1beta1/namespaces/" + namespace + "/pods/" + name
	if err := r.getMetrics(ctx, path, &item); err != nil {
		return nil, err
	}
	out := make(map[string]Usage, len(item.Containers))
	for _, c := range item.Containers {
		out[c.Name] = parseUsage(c.Usage)
	}
	return out, nil
}

func (r *Reader) getMetrics(ctx context.Context, path string, into any) error {
	raw, err := r.rest.Get().AbsPath(path).DoRaw(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) || apierrors.IsServiceUnavailable(err) {
			return ErrNoMetrics
		}
		return fmt.Errorf("get %s: %w", path, err)
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

func parseUsage(m map[string]string) Usage {
	return Usage{CPU: quantity(m["cpu"]), Memory: quantity(m["memory"])}
}

func quantity(s string) float64 {
	if s == "" {
		return 0
	}
	q, err := resource.ParseQuantity(s)
	if err != nil {
		return 0
	}
	return q.AsApproximateFloat64()
}

type statsSummary struct {
	Pods []struct {
		Volume []struct {
			UsedBytes *float64 `json:"usedBytes"`
			PVCRef    *struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"pvcRef"`
		} `json:"volume"`
	} `json:"pods"`
}

// statsConcurrency bounds parallel kubelet calls on big clusters.
const statsConcurrency = 8

// VolumeUsage returns used bytes per PVC from each node's kubelet stats
// summary. Volume types that don't report usage (e.g. hostPath) are absent.
// It fails only when no node answered.
func (r *Reader) VolumeUsage(ctx context.Context, nodes []string) (map[PVCKey]float64, error) {
	var (
		mu       sync.Mutex
		out      = map[PVCKey]float64{}
		answered int
		lastErr  error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, statsConcurrency)
	)
	for _, node := range nodes {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			var summary statsSummary
			raw, err := r.rest.Get().AbsPath("/api/v1/nodes", node, "proxy", "stats", "summary").DoRaw(ctx)
			if err == nil {
				err = json.Unmarshal(raw, &summary)
			}
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				lastErr = fmt.Errorf("kubelet stats for node %s: %w", node, err)
				return
			}
			answered++
			for _, p := range summary.Pods {
				for _, v := range p.Volume {
					if v.PVCRef != nil && v.UsedBytes != nil {
						out[PVCKey{Namespace: v.PVCRef.Namespace, Name: v.PVCRef.Name}] = *v.UsedBytes
					}
				}
			}
		})
	}
	wg.Wait()
	if answered == 0 && lastErr != nil {
		return nil, lastErr
	}
	return out, nil
}
