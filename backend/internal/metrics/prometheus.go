package metrics

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// Prometheus runs instant queries and returns the summed result.
type Prometheus interface {
	Query(ctx context.Context, query string) (float64, error)
}

// Target is a Prometheus service found in the cluster.
type Target struct {
	Namespace string
	Service   string
	Port      int32
	Scheme    string
}

// discoverySelectors are tried in order: the Prometheus Operator's governing
// service, then the community chart's labels.
var discoverySelectors = []string{
	"operated-prometheus=true",
	"app.kubernetes.io/name=prometheus",
	"app=prometheus",
}

// Discover finds a Prometheus service, or returns nil when there is none.
func Discover(ctx context.Context, kube kubernetes.Interface) (*Target, error) {
	for _, sel := range discoverySelectors {
		list, err := kube.CoreV1().Services("").List(ctx, metav1.ListOptions{LabelSelector: sel, Limit: 50})
		if err != nil {
			return nil, fmt.Errorf("look for prometheus services (%s): %w", sel, err)
		}
		for i := range list.Items {
			if t, ok := targetFor(&list.Items[i]); ok {
				return t, nil
			}
		}
	}
	return nil, nil
}

func targetFor(svc *corev1.Service) (*Target, bool) {
	var chosen *corev1.ServicePort
	for i := range svc.Spec.Ports {
		p := &svc.Spec.Ports[i]
		switch {
		case p.Port == 9090:
			chosen = p
		case chosen == nil && (p.Name == "web" || p.Name == "http-web" || p.Name == "http"):
			chosen = p
		}
		if chosen != nil && chosen.Port == 9090 {
			break
		}
	}
	if chosen == nil {
		return nil, false
	}
	scheme := "http"
	if strings.Contains(chosen.Name, "https") {
		scheme = "https"
	}
	return &Target{Namespace: svc.Namespace, Service: svc.Name, Port: chosen.Port, Scheme: scheme}, true
}

// ServiceProxy queries Prometheus through the API server's service proxy,
// so it runs with the caller's identity and needs no network route.
type ServiceProxy struct {
	rest   rest.Interface
	target Target
}

// NewServiceProxy returns a client for target using kube's credentials.
func NewServiceProxy(kube kubernetes.Interface, target Target) *ServiceProxy {
	return &ServiceProxy{rest: kube.CoreV1().RESTClient(), target: target}
}

// Query implements Prometheus.
func (p *ServiceProxy) Query(ctx context.Context, query string) (float64, error) {
	t := p.target
	name := fmt.Sprintf("%s:%s:%d", t.Scheme, t.Service, t.Port)
	raw, err := p.rest.Get().
		AbsPath("/api/v1/namespaces", t.Namespace, "services", name, "proxy", "api", "v1", "query").
		Param("query", query).
		DoRaw(ctx)
	if err != nil {
		return 0, fmt.Errorf("query prometheus %s/%s: %w", t.Namespace, t.Service, err)
	}
	return parseResult(raw)
}

// URL queries a Prometheus reachable at a fixed address (-prometheus-url).
type URL struct {
	Client *http.Client
	Base   string
}

// Query implements Prometheus.
func (u *URL) Query(ctx context.Context, query string) (float64, error) {
	endpoint := strings.TrimSuffix(u.Base, "/") + "/api/v1/query?query=" + url.QueryEscape(query)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("build prometheus request: %w", err)
	}
	client := u.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("query prometheus: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, fmt.Errorf("read prometheus response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("prometheus returned %s", resp.Status)
	}
	return parseResult(raw)
}

type promResponse struct {
	Status string `json:"status"`
	Error  string `json:"error"`
	Data   struct {
		ResultType string          `json:"resultType"`
		Result     json.RawMessage `json:"result"`
	} `json:"data"`
}

// parseResult sums a vector result, or reads a scalar. Empty is 0.
func parseResult(raw []byte) (float64, error) {
	var resp promResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, fmt.Errorf("decode prometheus response: %w", err)
	}
	if resp.Status != "success" {
		return 0, fmt.Errorf("prometheus query failed: %s", resp.Error)
	}
	switch resp.Data.ResultType {
	case "vector":
		var vec []struct {
			Value [2]json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(resp.Data.Result, &vec); err != nil {
			return 0, fmt.Errorf("decode vector: %w", err)
		}
		var sum float64
		for _, s := range vec {
			v, err := sampleValue(s.Value[1])
			if err != nil {
				return 0, err
			}
			sum += v
		}
		return sum, nil
	case "scalar":
		var s [2]json.RawMessage
		if err := json.Unmarshal(resp.Data.Result, &s); err != nil {
			return 0, fmt.Errorf("decode scalar: %w", err)
		}
		return sampleValue(s[1])
	default:
		return 0, fmt.Errorf("unexpected prometheus result type %q", resp.Data.ResultType)
	}
}

func sampleValue(raw json.RawMessage) (float64, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return 0, fmt.Errorf("decode sample: %w", err)
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse sample %q: %w", s, err)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, nil
	}
	return v, nil
}

// networkSelector keeps pod-network interfaces. Host-network pods report the
// node's own interfaces, so they're removed below via kube-state-metrics.
const networkSelector = `pod!="",interface!~"lo|flannel.*|cni.*|cali.*|vxlan.*|tunl.*|docker.*|veth.*|br-.*|kube-ipvs.*"`

// NetworkRateQuery is cluster pod traffic, bytes/s in + out.
var NetworkRateQuery = fmt.Sprintf(
	`sum((rate(container_network_receive_bytes_total{%[1]s}[5m]) + rate(container_network_transmit_bytes_total{%[1]s}[5m])) unless on (namespace, pod) kube_pod_info{host_network="true"})`,
	networkSelector,
)

// NetworkPeakQuery is the 7-day peak of NetworkRateQuery.
var NetworkPeakQuery = fmt.Sprintf("max_over_time((%s)[7d:5m])", NetworkRateQuery)

// Network builds the network gauge: traffic now against its 7-day peak.
func Network(ctx context.Context, prom Prometheus) Gauge {
	g := Gauge{State: Live, Unit: "bytes/s", Source: "prometheus"}
	now, err := prom.Query(ctx, NetworkRateQuery)
	if err == nil {
		g.Value = now
		g.Max, err = prom.Query(ctx, NetworkPeakQuery)
	}
	if err != nil {
		return Gauge{State: Failed, Unit: "bytes/s", Source: "prometheus", Message: "Couldn't query Prometheus."}
	}
	g.Max = math.Max(g.Max, g.Value)
	return g
}
