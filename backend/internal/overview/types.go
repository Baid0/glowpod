// Package overview builds the Overview page's data in one response:
// gauges, workload counts, problems, node load, top consumers, and the
// Glance-mode app cards and recent deploys.
package overview

import (
	"time"

	"glowpod/internal/cluster"
	"glowpod/internal/metrics"
	"glowpod/internal/summary"
)

// Response is GET /api/v1/clusters/{cluster}/overview.
type Response struct {
	Cluster     ClusterInfo `json:"cluster"`
	Namespace   string      `json:"namespace"`
	GeneratedAt time.Time   `json:"generatedAt"`
	// Thresholds are the High and Critical gauge zones, as fractions.
	Thresholds [2]float64 `json:"thresholds"`

	Gauges        Gauges            `json:"gauges"`
	Workloads     Workloads         `json:"workloads"`
	Health        Health            `json:"health"`
	Problems      []summary.Problem `json:"problems"`
	ProblemCount  int               `json:"problemCount"`
	Nodes         []NodeLoad        `json:"nodes"`
	TopConsumers  []Consumer        `json:"topConsumers"`
	Apps          []App             `json:"apps"`
	RecentDeploys []Deploy          `json:"recentDeploys"`

	// Unavailable lists sections that couldn't load; the rest still render.
	Unavailable []Unavailable `json:"unavailable,omitempty"`
}

// ClusterInfo is the line under the page title.
type ClusterInfo struct {
	cluster.Info
	Version    string `json:"version"`
	Nodes      int    `json:"nodes"`
	Namespaces int    `json:"namespaces"`
}

// Gauges are cluster-wide; the namespace picker doesn't change them.
type Gauges struct {
	CPU     metrics.Gauge `json:"cpu"`
	Memory  metrics.Gauge `json:"memory"`
	Pods    metrics.Gauge `json:"pods"`
	Storage metrics.Gauge `json:"storage"`
	// Network is absent when no Prometheus was found; the UI hides it.
	Network *metrics.Gauge `json:"network,omitempty"`
}

// WorkloadCount is one workload tile. Ready + Pending + Failing = Total.
type WorkloadCount struct {
	Total   int `json:"total"`
	Ready   int `json:"ready"`
	Pending int `json:"pending"`
	Failing int `json:"failing"`
}

// JobCount covers jobs started in the last 24 hours.
type JobCount struct {
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Running   int `json:"running"`
}

// Workloads are the Deep-mode tiles.
type Workloads struct {
	Deployments  WorkloadCount `json:"deployments"`
	StatefulSets WorkloadCount `json:"statefulSets"`
	DaemonSets   WorkloadCount `json:"daemonSets"`
	Jobs         JobCount      `json:"jobs"`
}

// Health is the "running · pending · failing" line.
type Health struct {
	Running int `json:"running"`
	Pending int `json:"pending"`
	Failing int `json:"failing"`
}

// NodeLoad is one tile in "Node load". CPU and Memory are fractions of
// allocatable, absent without metrics.
type NodeLoad struct {
	Name   string   `json:"name"`
	Ready  bool     `json:"ready"`
	CPU    *float64 `json:"cpu,omitempty"`
	Memory *float64 `json:"memory,omitempty"`
	Hot    bool     `json:"hot"`
}

// Consumer is a row in "Top consumers": CPU in cores, memory in bytes.
type Consumer struct {
	Namespace string  `json:"namespace"`
	Name      string  `json:"name"`
	CPU       float64 `json:"cpu"`
	Memory    float64 `json:"memory"`
}

// AppStatus uses the gauge zone words: ok, high (rolling out, pending), critical.
type AppStatus string

// App statuses.
const (
	AppOK       AppStatus = "ok"
	AppHigh     AppStatus = "high"
	AppCritical AppStatus = "critical"
)

// App is a Glance-mode card: workloads grouped by app.kubernetes.io/name or
// Helm release.
type App struct {
	Name       string     `json:"name"`
	Namespace  string     `json:"namespace"`
	Workloads  int        `json:"workloads"`
	Ready      int32      `json:"ready"`
	Total      int32      `json:"total"`
	Status     AppStatus  `json:"status"`
	LastDeploy *time.Time `json:"lastDeploy,omitempty"`
}

// Deploy is a row in Glance's "Recent deploys": a new Deployment revision.
type Deploy struct {
	Namespace string    `json:"namespace"`
	Name      string    `json:"name"`
	Revision  string    `json:"revision"`
	Image     string    `json:"image"`
	At        time.Time `json:"at"`
}

// Unavailable names a section that failed to load, and why.
type Unavailable struct {
	Section string `json:"section"`
	Message string `json:"message"`
}
