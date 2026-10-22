// What the demo cluster runs when it starts: a small shop, payments and search, plus the usual
// add-ons. The story: payments-api 1.8.0 was rolled out 25 minutes ago and crashes on start (its
// Helm upgrade failed, the old pods keep serving), search-indexer-1 doesn't fit on any node, and
// ip-10-0-2-14 is short of memory.

import type { LogKind } from "./logs";
import { GiB, MiB } from "./util";

export const CLUSTER_NAME = "demo-cluster";
export const KUBE_VERSION = "v1.34.2";

export const NAMESPACES = [
  "cert-manager",
  "default",
  "glowpod",
  "ingress-nginx",
  "kube-system",
  "monitoring",
  "payments",
  "search",
  "shop",
];

export interface NodeSpec {
  name: string;
  zone: string;
  ip: string;
  /** Typical load, as a share of allocatable. */
  cpu: number;
  memory: number;
  ageDays: number;
}

export const NODES: NodeSpec[] = [
  {
    name: "ip-10-0-1-21",
    zone: "eu-west-1a",
    ip: "10.0.1.21",
    cpu: 0.58,
    memory: 0.71,
    ageDays: 41,
  },
  {
    name: "ip-10-0-1-34",
    zone: "eu-west-1a",
    ip: "10.0.1.34",
    cpu: 0.66,
    memory: 0.68,
    ageDays: 41,
  },
  {
    name: "ip-10-0-2-14",
    zone: "eu-west-1b",
    ip: "10.0.2.14",
    cpu: 0.79,
    memory: 0.92,
    ageDays: 12,
  },
  {
    name: "ip-10-0-3-31",
    zone: "eu-west-1c",
    ip: "10.0.3.31",
    cpu: 0.47,
    memory: 0.63,
    ageDays: 6,
  },
];

/** m6i.xlarge, as EKS reports it. */
export const NODE_CAPACITY = { cpu: "4", memory: "16073188Ki", pods: "58" };
export const NODE_ALLOCATABLE = { cpu: "3920m", memory: "15155268Ki", pods: "58" };

/** Images that crash on start. Changing the image away from one fixes the pods. */
export const BROKEN_IMAGES = new Set(["ghcr.io/acme/payments-api:1.8.0"]);

export interface WorkloadSpec {
  kind: "Deployment" | "StatefulSet" | "DaemonSet";
  namespace: string;
  name: string;
  replicas?: number;
  container: string;
  image: string;
  /** Deployments: the image of the ReplicaSet before the current one. */
  previousImage?: string;
  cpu: string;
  memory: string;
  memoryLimit?: string;
  /** What one pod typically uses: cores and bytes. */
  usage: [number, number];
  port?: { name: string; port: number };
  labels: Record<string, string>;
  /** The Helm release that installed it. */
  helm?: string;
  env?: { name: string; value?: string; secret?: [string, string] }[];
  configMap?: string;
  claim?: { size: string; storageClass: string };
  log: LogKind;
  ageDays: number;
  /** Deployments: when the current ReplicaSet was rolled out, in minutes ago. */
  deployedMinutesAgo?: number;
  revision?: number;
}

const app = (name: string, partOf?: string, extra: Record<string, string> = {}) => ({
  "app.kubernetes.io/name": name,
  ...(partOf ? { "app.kubernetes.io/part-of": partOf } : {}),
  ...extra,
});

export const WORKLOADS: WorkloadSpec[] = [
  {
    kind: "Deployment",
    namespace: "shop",
    name: "storefront",
    replicas: 3,
    container: "web",
    image: "ghcr.io/acme/storefront:2.14.0",
    previousImage: "ghcr.io/acme/storefront:2.13.2",
    cpu: "500m",
    memory: "1Gi",
    memoryLimit: "1536Mi",
    usage: [0.31, 610 * MiB],
    port: { name: "http", port: 3000 },
    labels: app("storefront", "shop", { "app.kubernetes.io/version": "2.14.0" }),
    env: [
      { name: "CART_URL", value: "http://cart.shop.svc.cluster.local" },
      { name: "CHECKOUT_URL", value: "http://checkout.shop.svc.cluster.local" },
    ],
    configMap: "storefront-config",
    log: "web",
    ageDays: 94,
    deployedMinutesAgo: 190,
    revision: 31,
  },
  {
    kind: "Deployment",
    namespace: "shop",
    name: "cart",
    replicas: 2,
    container: "cart",
    image: "ghcr.io/acme/cart:1.9.2",
    cpu: "200m",
    memory: "512Mi",
    usage: [0.07, 180 * MiB],
    port: { name: "http", port: 8080 },
    labels: app("cart", "shop"),
    env: [{ name: "REDIS_ADDR", value: "shop-redis-master.shop.svc.cluster.local:6379" }],
    log: "cart",
    ageDays: 94,
    deployedMinutesAgo: 2 * 24 * 60,
    revision: 12,
  },
  {
    kind: "Deployment",
    namespace: "shop",
    name: "checkout",
    replicas: 2,
    container: "checkout",
    image: "ghcr.io/acme/checkout:3.2.1",
    cpu: "500m",
    memory: "1Gi",
    usage: [0.14, 420 * MiB],
    port: { name: "http", port: 8080 },
    labels: app("checkout", "shop"),
    env: [{ name: "PAYMENTS_URL", value: "http://payments-api.payments.svc.cluster.local" }],
    log: "checkout",
    ageDays: 94,
    deployedMinutesAgo: 9 * 60,
    revision: 18,
  },
  {
    kind: "StatefulSet",
    namespace: "shop",
    name: "shop-redis-master",
    replicas: 1,
    container: "redis",
    image: "docker.io/bitnami/redis:7.4.1",
    cpu: "250m",
    memory: "2Gi",
    usage: [0.05, 740 * MiB],
    port: { name: "redis", port: 6379 },
    labels: app("redis", undefined, { "app.kubernetes.io/instance": "shop-redis" }),
    helm: "shop-redis",
    claim: { size: "8Gi", storageClass: "gp3" },
    log: "redis",
    ageDays: 94,
  },
  {
    kind: "Deployment",
    namespace: "payments",
    name: "payments-api",
    replicas: 2,
    container: "api",
    image: "ghcr.io/acme/payments-api:1.8.0",
    previousImage: "ghcr.io/acme/payments-api:1.7.3",
    cpu: "500m",
    memory: "1536Mi",
    memoryLimit: "2Gi",
    usage: [0.38, 820 * MiB],
    port: { name: "http", port: 8080 },
    labels: app("payments-api", "payments", { "app.kubernetes.io/version": "1.8.0" }),
    helm: "payments",
    env: [
      { name: "DB_HOST", value: "payments-db.payments.svc.cluster.local" },
      { name: "DB_PASSWORD", secret: ["payments-db", "password"] },
    ],
    log: "payments",
    ageDays: 61,
    deployedMinutesAgo: 25,
    revision: 7,
  },
  {
    kind: "StatefulSet",
    namespace: "payments",
    name: "payments-db",
    replicas: 1,
    container: "postgres",
    image: "docker.io/library/postgres:17.0",
    cpu: "1",
    memory: "4Gi",
    usage: [0.42, 2.9 * GiB],
    port: { name: "postgres", port: 5432 },
    labels: app("payments-db", "payments"),
    helm: "payments",
    env: [{ name: "POSTGRES_PASSWORD", secret: ["payments-db", "password"] }],
    claim: { size: "50Gi", storageClass: "gp3" },
    log: "postgres",
    ageDays: 61,
  },
  {
    kind: "Deployment",
    namespace: "search",
    name: "search-api",
    replicas: 2,
    container: "api",
    image: "ghcr.io/acme/search-api:0.21.4",
    cpu: "500m",
    memory: "1Gi",
    usage: [0.22, 560 * MiB],
    port: { name: "http", port: 8080 },
    labels: app("search-api", "search"),
    log: "search-api",
    ageDays: 33,
    deployedMinutesAgo: 6 * 24 * 60,
    revision: 9,
  },
  {
    kind: "StatefulSet",
    namespace: "search",
    name: "search-indexer",
    replicas: 2,
    container: "indexer",
    image: "ghcr.io/acme/search-indexer:0.21.4",
    cpu: "500m",
    memory: "6Gi",
    usage: [0.46, 4.7 * GiB],
    labels: app("search-indexer", "search"),
    claim: { size: "30Gi", storageClass: "gp3" },
    log: "indexer",
    ageDays: 33,
  },
  {
    kind: "StatefulSet",
    namespace: "monitoring",
    name: "prometheus-kube-prometheus-stack-prometheus",
    replicas: 1,
    container: "prometheus",
    image: "quay.io/prometheus/prometheus:v3.6.0",
    cpu: "1",
    memory: "8Gi",
    usage: [0.71, 6.1 * GiB],
    port: { name: "http-web", port: 9090 },
    labels: app("prometheus", undefined, { "app.kubernetes.io/instance": "kube-prometheus-stack" }),
    helm: "kube-prometheus-stack",
    claim: { size: "100Gi", storageClass: "gp3" },
    log: "prometheus",
    ageDays: 41,
  },
  {
    kind: "Deployment",
    namespace: "monitoring",
    name: "kube-prometheus-stack-grafana",
    replicas: 1,
    container: "grafana",
    image: "docker.io/grafana/grafana:12.2.0",
    cpu: "100m",
    memory: "512Mi",
    usage: [0.03, 240 * MiB],
    port: { name: "http", port: 3000 },
    labels: app("grafana", undefined, { "app.kubernetes.io/instance": "kube-prometheus-stack" }),
    helm: "kube-prometheus-stack",
    log: "grafana",
    ageDays: 41,
    deployedMinutesAgo: 41 * 24 * 60,
    revision: 4,
  },
  {
    kind: "Deployment",
    namespace: "monitoring",
    name: "kube-prometheus-stack-operator",
    replicas: 1,
    container: "kube-prometheus-stack",
    image: "quay.io/prometheus-operator/prometheus-operator:v0.86.0",
    cpu: "100m",
    memory: "256Mi",
    usage: [0.01, 52 * MiB],
    labels: app("kube-prometheus-stack-prometheus-operator", undefined, {
      "app.kubernetes.io/instance": "kube-prometheus-stack",
    }),
    helm: "kube-prometheus-stack",
    log: "operator",
    ageDays: 41,
    deployedMinutesAgo: 41 * 24 * 60,
    revision: 4,
  },
  {
    kind: "Deployment",
    namespace: "monitoring",
    name: "kube-prometheus-stack-kube-state-metrics",
    replicas: 1,
    container: "kube-state-metrics",
    image: "registry.k8s.io/kube-state-metrics/kube-state-metrics:v2.17.0",
    cpu: "100m",
    memory: "256Mi",
    usage: [0.02, 71 * MiB],
    port: { name: "http", port: 8080 },
    labels: app("kube-state-metrics", undefined, {
      "app.kubernetes.io/instance": "kube-prometheus-stack",
    }),
    helm: "kube-prometheus-stack",
    log: "klog",
    ageDays: 41,
    deployedMinutesAgo: 41 * 24 * 60,
    revision: 4,
  },
  {
    kind: "DaemonSet",
    namespace: "monitoring",
    name: "kube-prometheus-stack-prometheus-node-exporter",
    container: "node-exporter",
    image: "quay.io/prometheus/node-exporter:v1.9.1",
    cpu: "100m",
    memory: "128Mi",
    usage: [0.01, 24 * MiB],
    port: { name: "http-metrics", port: 9100 },
    labels: app("prometheus-node-exporter", undefined, {
      "app.kubernetes.io/instance": "kube-prometheus-stack",
    }),
    helm: "kube-prometheus-stack",
    log: "node-exporter",
    ageDays: 41,
  },
  {
    kind: "Deployment",
    namespace: "kube-system",
    name: "coredns",
    replicas: 2,
    container: "coredns",
    image: "registry.k8s.io/coredns/coredns:v1.12.3",
    cpu: "100m",
    memory: "70Mi",
    memoryLimit: "170Mi",
    usage: [0.006, 26 * MiB],
    port: { name: "dns", port: 53 },
    labels: { "k8s-app": "kube-dns" },
    log: "coredns",
    ageDays: 120,
    deployedMinutesAgo: 41 * 24 * 60,
    revision: 3,
  },
  {
    kind: "DaemonSet",
    namespace: "kube-system",
    name: "kube-proxy",
    container: "kube-proxy",
    image: "registry.k8s.io/kube-proxy:v1.34.2",
    cpu: "100m",
    memory: "64Mi",
    usage: [0.004, 31 * MiB],
    labels: { "k8s-app": "kube-proxy" },
    log: "kube-proxy",
    ageDays: 120,
  },
  {
    kind: "Deployment",
    namespace: "kube-system",
    name: "metrics-server",
    replicas: 1,
    container: "metrics-server",
    image: "registry.k8s.io/metrics-server/metrics-server:v0.8.0",
    cpu: "100m",
    memory: "200Mi",
    usage: [0.007, 41 * MiB],
    port: { name: "https", port: 10250 },
    labels: app("metrics-server", undefined, { "app.kubernetes.io/instance": "metrics-server" }),
    helm: "metrics-server",
    log: "klog",
    ageDays: 120,
    deployedMinutesAgo: 88 * 24 * 60,
    revision: 2,
  },
  {
    kind: "Deployment",
    namespace: "ingress-nginx",
    name: "ingress-nginx-controller",
    replicas: 2,
    container: "controller",
    image: "registry.k8s.io/ingress-nginx/controller:v1.13.3",
    cpu: "100m",
    memory: "512Mi",
    usage: [0.06, 150 * MiB],
    port: { name: "https", port: 443 },
    labels: app("ingress-nginx", undefined, {
      "app.kubernetes.io/instance": "ingress-nginx",
      "app.kubernetes.io/component": "controller",
    }),
    helm: "ingress-nginx",
    log: "nginx",
    ageDays: 120,
    deployedMinutesAgo: 20 * 24 * 60,
    revision: 5,
  },
  {
    kind: "Deployment",
    namespace: "cert-manager",
    name: "cert-manager",
    replicas: 1,
    container: "cert-manager-controller",
    image: "quay.io/jetstack/cert-manager-controller:v1.19.1",
    cpu: "50m",
    memory: "256Mi",
    usage: [0.003, 44 * MiB],
    labels: app("cert-manager", undefined, { "app.kubernetes.io/instance": "cert-manager" }),
    helm: "cert-manager",
    log: "klog",
    ageDays: 120,
    deployedMinutesAgo: 30 * 24 * 60,
    revision: 3,
  },
  {
    kind: "Deployment",
    namespace: "glowpod",
    name: "glowpod",
    replicas: 1,
    container: "glowpod",
    image: "ghcr.io/glowpod-dev/glowpod:0.1.4",
    cpu: "50m",
    memory: "64Mi",
    usage: [0.004, 36 * MiB],
    port: { name: "http", port: 7171 },
    labels: app("glowpod", undefined, { "app.kubernetes.io/instance": "glowpod" }),
    helm: "glowpod",
    log: "glowpod",
    ageDays: 3,
    deployedMinutesAgo: 3 * 24 * 60,
    revision: 1,
  },
];

export interface HelmSpec {
  name: string;
  namespace: string;
  chart: string;
  chartVersion: string;
  appVersion: string;
  /** Oldest first; the last is the current revision. */
  history: { status: string; daysAgo: number; description: string }[];
}

const upgrades = (n: number, firstDaysAgo: number, lastDaysAgo: number) =>
  Array.from({ length: n }, (_, i) => ({
    status: i === n - 1 ? "deployed" : "superseded",
    daysAgo: firstDaysAgo - ((firstDaysAgo - lastDaysAgo) * i) / Math.max(1, n - 1),
    description: i === 0 ? "Install complete" : "Upgrade complete",
  }));

export const HELM_RELEASES: HelmSpec[] = [
  {
    name: "payments",
    namespace: "payments",
    chart: "payments",
    chartVersion: "1.8.0",
    appVersion: "1.8.0",
    history: [
      ...upgrades(6, 61, 4),
      {
        status: "failed",
        daysAgo: 25 / (24 * 60),
        description:
          'Upgrade "payments" failed: context deadline exceeded: resource Deployment/payments/payments-api not ready',
      },
    ],
  },
  {
    name: "kube-prometheus-stack",
    namespace: "monitoring",
    chart: "kube-prometheus-stack",
    chartVersion: "78.3.0",
    appVersion: "v0.86.0",
    history: upgrades(4, 41, 9),
  },
  {
    name: "shop-redis",
    namespace: "shop",
    chart: "redis",
    chartVersion: "20.1.7",
    appVersion: "7.4.1",
    history: upgrades(2, 94, 30),
  },
  {
    name: "ingress-nginx",
    namespace: "ingress-nginx",
    chart: "ingress-nginx",
    chartVersion: "4.13.3",
    appVersion: "1.13.3",
    history: upgrades(5, 120, 20),
  },
  {
    name: "cert-manager",
    namespace: "cert-manager",
    chart: "cert-manager",
    chartVersion: "v1.19.1",
    appVersion: "v1.19.1",
    history: upgrades(3, 120, 30),
  },
  {
    name: "metrics-server",
    namespace: "kube-system",
    chart: "metrics-server",
    chartVersion: "3.13.0",
    appVersion: "0.8.0",
    history: upgrades(2, 120, 88),
  },
  {
    name: "glowpod",
    namespace: "glowpod",
    chart: "glowpod",
    chartVersion: "0.1.4",
    appVersion: "0.1.4",
    history: upgrades(1, 3, 3),
  },
];
