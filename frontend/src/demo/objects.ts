// The demo cluster's other objects: Services, Ingresses, policies, config, storage classes and a
// couple of custom resources. Values in Secrets are made up for the demo.

import { NAMESPACES, WORKLOADS } from "./catalog";
import type { DemoCluster } from "./cluster";
import { iso, type Obj } from "./util";

const DAY = 86_400_000;

const b64 = (s: string) => btoa(s);

const FAKE_CERT = `-----BEGIN CERTIFICATE-----
MIIBszCCAVmgAwIBAgIUDemoOnlyNotARealCertificate0wCgYIKoZIzj0EAwIw
GDEWMBQGA1UEAwwNZ2xvd3BvZC1kZW1vMB4XDTI2MTAwMTAwMDAwMFoXDTI3MDEw
MTAwMDAwMFowGDEWMBQGA1UEAwwNZ2xvd3BvZC1kZW1vMFkwEwYHKoZIzj0CAQYI
-----END CERTIFICATE-----
`;

export function extraObjects(c: DemoCluster, t0: number): [string, Obj[]][] {
  const at = (days: number) => iso(t0 - days * DAY);
  const metaFor = (name: string, namespace: string | undefined, days: number, extra: Obj = {}) => ({
    name,
    ...(namespace ? { namespace } : {}),
    uid: c.uid(),
    creationTimestamp: at(days),
    resourceVersion: "",
    ...extra,
  });
  const lb = "k8s-ingressn-ingressn-4f2a9c1d7e-8d3b2a1f0c6e9b47.elb.eu-west-1.amazonaws.com";

  const services: Obj[] = [
    {
      apiVersion: "v1",
      kind: "Service",
      metadata: metaFor("kubernetes", "default", 120, {
        labels: { component: "apiserver", provider: "kubernetes" },
      }),
      spec: {
        type: "ClusterIP",
        clusterIP: "172.20.0.1",
        clusterIPs: ["172.20.0.1"],
        ports: [{ name: "https", port: 443, protocol: "TCP", targetPort: 443 }],
        sessionAffinity: "None",
      },
      status: { loadBalancer: {} },
    },
  ];
  let ip = 10;
  for (const w of WORKLOADS) {
    if (!w.port || w.kind === "DaemonSet") continue;
    ip += 17;
    const name =
      w.name === "coredns"
        ? "kube-dns"
        : w.name.startsWith("prometheus-")
          ? "kube-prometheus-stack-prometheus"
          : w.name;
    const selector = Object.fromEntries(
      Object.entries(w.labels).filter(([k]) =>
        ["app.kubernetes.io/name", "app.kubernetes.io/instance", "k8s-app"].includes(k),
      ),
    );
    const loadBalancer = w.name === "ingress-nginx-controller";
    const headless = w.kind === "StatefulSet" && w.name === "payments-db";
    const clusterIP =
      name === "kube-dns"
        ? "172.20.0.10"
        : headless
          ? "None"
          : `172.20.${String(ip % 250)}.${String((ip * 7) % 250)}`;
    services.push({
      apiVersion: "v1",
      kind: "Service",
      metadata: metaFor(name, w.namespace, w.ageDays, {
        labels: { ...w.labels, ...(w.helm ? { "app.kubernetes.io/managed-by": "Helm" } : {}) },
        ...(w.helm
          ? {
              annotations: {
                "meta.helm.sh/release-name": w.helm,
                "meta.helm.sh/release-namespace": w.namespace,
              },
            }
          : {}),
      }),
      spec: {
        type: loadBalancer ? "LoadBalancer" : "ClusterIP",
        clusterIP,
        clusterIPs: [clusterIP],
        selector,
        ports: loadBalancer
          ? [
              { name: "http", port: 80, protocol: "TCP", targetPort: "http", nodePort: 31480 },
              { name: "https", port: 443, protocol: "TCP", targetPort: "https", nodePort: 30443 },
            ]
          : [
              {
                name: w.port.name,
                port: w.port.name === "http" ? 80 : w.port.port,
                protocol: w.port.name === "dns" ? "UDP" : "TCP",
                targetPort: w.port.name,
              },
            ],
        sessionAffinity: "None",
        ...(loadBalancer ? { externalTrafficPolicy: "Local" } : {}),
      },
      status: { loadBalancer: loadBalancer ? { ingress: [{ hostname: lb }] } : {} },
    });
  }

  const ingress = (
    name: string,
    namespace: string,
    host: string,
    service: string,
    days: number,
  ) => ({
    apiVersion: "networking.k8s.io/v1",
    kind: "Ingress",
    metadata: metaFor(name, namespace, days, {
      annotations: { "cert-manager.io/cluster-issuer": "letsencrypt" },
    }),
    spec: {
      ingressClassName: "nginx",
      tls: [{ hosts: [host], secretName: `${name}-tls` }],
      rules: [
        {
          host,
          http: {
            paths: [
              {
                path: "/",
                pathType: "Prefix",
                backend: { service: { name: service, port: { name: "http" } } },
              },
            ],
          },
        },
      ],
    },
    status: { loadBalancer: { ingress: [{ hostname: lb }] } },
  });

  const configMaps: Obj[] = NAMESPACES.map((ns) => ({
    apiVersion: "v1",
    kind: "ConfigMap",
    metadata: metaFor("kube-root-ca.crt", ns, ns === "glowpod" ? 3 : 60, {
      annotations: {
        "kubernetes.io/description":
          "Contains a CA bundle that can be used to verify the kube-apiserver when using internal endpoints such as the internal service IP or kubernetes.default.svc. No other usage is guaranteed across distributions of Kubernetes clusters.",
      },
    }),
    data: { "ca.crt": FAKE_CERT },
  }));
  configMaps.push(
    {
      apiVersion: "v1",
      kind: "ConfigMap",
      metadata: metaFor("storefront-config", "shop", 94, {
        labels: { "app.kubernetes.io/name": "storefront", "app.kubernetes.io/part-of": "shop" },
      }),
      data: {
        LOG_LEVEL: "info",
        FEATURE_NEW_CHECKOUT: "true",
        FEATURE_RECOMMENDATIONS: "false",
        CDN_URL: "https://cdn.shop.demo.glowpod.dev",
        "banner.json": '{\n  "text": "Free shipping this week",\n  "until": "2026-10-31"\n}\n',
      },
    },
    {
      apiVersion: "v1",
      kind: "ConfigMap",
      metadata: metaFor("coredns", "kube-system", 120, {
        labels: { "k8s-app": "kube-dns", "eks.amazonaws.com/component": "coredns" },
      }),
      data: {
        Corefile:
          ".:53 {\n    errors\n    health {\n        lameduck 5s\n    }\n    ready\n    kubernetes cluster.local in-addr.arpa ip6.arpa {\n        pods insecure\n        fallthrough in-addr.arpa ip6.arpa\n    }\n    prometheus :9153\n    forward . /etc/resolv.conf\n    cache 30\n    loop\n    reload\n    loadbalance\n}\n",
      },
    },
    {
      apiVersion: "v1",
      kind: "ConfigMap",
      metadata: metaFor("kube-prometheus-stack-grafana", "monitoring", 41, {
        labels: { "app.kubernetes.io/name": "grafana", "app.kubernetes.io/managed-by": "Helm" },
        annotations: {
          "meta.helm.sh/release-name": "kube-prometheus-stack",
          "meta.helm.sh/release-namespace": "monitoring",
        },
      }),
      data: {
        "grafana.ini":
          "[analytics]\ncheck_for_updates = true\n[log]\nmode = console\n[paths]\ndata = /var/lib/grafana/\nlogs = /var/log/grafana\nplugins = /var/lib/grafana/plugins\nprovisioning = /etc/grafana/provisioning\n[server]\ndomain = grafana.demo.glowpod.dev\n",
      },
    },
    {
      apiVersion: "v1",
      kind: "ConfigMap",
      metadata: metaFor("payments-api", "payments", 61, {
        labels: {
          "app.kubernetes.io/name": "payments-api",
          "app.kubernetes.io/managed-by": "Helm",
        },
        annotations: {
          "meta.helm.sh/release-name": "payments",
          "meta.helm.sh/release-namespace": "payments",
        },
      }),
      data: { CURRENCY: "EUR", LEDGER_BATCH_SIZE: "500", HTTP_TIMEOUT: "5s" },
    },
  );

  const secret = (
    name: string,
    namespace: string,
    days: number,
    type: string,
    data: Record<string, string>,
    extra: Obj = {},
  ) => ({
    apiVersion: "v1",
    kind: "Secret",
    metadata: metaFor(name, namespace, days, extra),
    type,
    data: Object.fromEntries(Object.entries(data).map(([k, v]) => [k, b64(v)])),
  });
  const secrets: Obj[] = [
    secret("payments-db", "payments", 61, "Opaque", {
      username: "payments",
      password: "demo-only-6vK2qLm9",
      database: "payments",
    }),
    secret(
      "storefront-tls",
      "shop",
      52 / (24 * 60),
      "kubernetes.io/tls",
      {
        "tls.crt": FAKE_CERT,
        "tls.key":
          "-----BEGIN EC PRIVATE KEY-----\nDemo key: not a real key.\n-----END EC PRIVATE KEY-----\n",
      },
      {
        annotations: {
          "cert-manager.io/certificate-name": "storefront-tls",
          "cert-manager.io/issuer-name": "letsencrypt",
        },
      },
    ),
    secret(
      "shop-redis",
      "shop",
      94,
      "Opaque",
      { "redis-password": "demo-only-r3d1s" },
      {
        labels: { "app.kubernetes.io/name": "redis", "app.kubernetes.io/managed-by": "Helm" },
      },
    ),
    secret(
      "kube-prometheus-stack-grafana",
      "monitoring",
      41,
      "Opaque",
      {
        "admin-user": "admin",
        "admin-password": "demo-only-gr4fana",
      },
      { labels: { "app.kubernetes.io/name": "grafana", "app.kubernetes.io/managed-by": "Helm" } },
    ),
    secret("glowpod-session", "glowpod", 3, "Opaque", { "session-key": "demo-only-0f3a9c2b7d" }),
    ...c.helmReleases().map((r) =>
      secret(
        `sh.helm.release.v1.${r.name}.v${String(r.history.length)}`,
        r.namespace,
        r.history.at(-1)?.daysAgo ?? 1,
        "helm.sh/release.v1",
        {
          release: `H4sIAAAAAAAC/demo-release-${r.name}`,
        },
        {
          labels: {
            name: r.name,
            owner: "helm",
            status: r.history.at(-1)?.status ?? "deployed",
            version: String(r.history.length),
          },
        },
      ),
    ),
  ];

  const policy = (name: string, namespace: string, spec: Obj) => ({
    apiVersion: "networking.k8s.io/v1",
    kind: "NetworkPolicy",
    metadata: metaFor(name, namespace, 60),
    spec,
  });

  const crd = (
    plural: string,
    group: string,
    kind: string,
    scope: string,
    short: string[],
    days: number,
  ) => ({
    apiVersion: "apiextensions.k8s.io/v1",
    kind: "CustomResourceDefinition",
    metadata: metaFor(`${plural}.${group}`, undefined, days, {
      labels: { "app.kubernetes.io/managed-by": "Helm" },
    }),
    spec: {
      group,
      names: {
        plural,
        singular: kind.toLowerCase(),
        kind,
        listKind: `${kind}List`,
        shortNames: short,
      },
      scope,
      versions: [{ name: "v1", served: true, storage: true }],
    },
    status: {
      acceptedNames: { plural, kind },
      conditions: [
        {
          type: "Established",
          status: "True",
          reason: "InitialNamesAccepted",
          lastTransitionTime: at(days),
        },
      ],
      storedVersions: ["v1"],
    },
  });

  return [
    ["/services", services],
    [
      "networking.k8s.io/ingresses",
      [
        ingress("storefront", "shop", "shop.demo.glowpod.dev", "storefront", 94),
        ingress(
          "grafana",
          "monitoring",
          "grafana.demo.glowpod.dev",
          "kube-prometheus-stack-grafana",
          41,
        ),
        ingress("glowpod", "glowpod", "glowpod.demo.glowpod.dev", "glowpod", 3),
      ],
    ],
    [
      "networking.k8s.io/ingressclasses",
      [
        {
          apiVersion: "networking.k8s.io/v1",
          kind: "IngressClass",
          metadata: metaFor("nginx", undefined, 120, {
            annotations: { "ingressclass.kubernetes.io/is-default-class": "true" },
          }),
          spec: { controller: "k8s.io/ingress-nginx" },
        },
      ],
    ],
    [
      "networking.k8s.io/networkpolicies",
      [
        policy("default-deny-ingress", "payments", { podSelector: {}, policyTypes: ["Ingress"] }),
        policy("payments-db-from-api", "payments", {
          podSelector: { matchLabels: { "app.kubernetes.io/name": "payments-db" } },
          policyTypes: ["Ingress"],
          ingress: [
            {
              from: [
                { podSelector: { matchLabels: { "app.kubernetes.io/name": "payments-api" } } },
              ],
              ports: [{ port: 5432, protocol: "TCP" }],
            },
          ],
        }),
        policy("payments-api-from-shop", "payments", {
          podSelector: { matchLabels: { "app.kubernetes.io/name": "payments-api" } },
          policyTypes: ["Ingress"],
          ingress: [
            {
              from: [
                { namespaceSelector: { matchLabels: { "kubernetes.io/metadata.name": "shop" } } },
              ],
              ports: [{ port: 8080, protocol: "TCP" }],
            },
          ],
        }),
      ],
    ],
    ["/configmaps", configMaps],
    ["/secrets", secrets],
    [
      "storage.k8s.io/storageclasses",
      [
        {
          apiVersion: "storage.k8s.io/v1",
          kind: "StorageClass",
          metadata: metaFor("gp3", undefined, 120, {
            annotations: { "storageclass.kubernetes.io/is-default-class": "true" },
          }),
          provisioner: "ebs.csi.aws.com",
          parameters: { type: "gp3", encrypted: "true" },
          reclaimPolicy: "Delete",
          volumeBindingMode: "WaitForFirstConsumer",
          allowVolumeExpansion: true,
        },
        {
          apiVersion: "storage.k8s.io/v1",
          kind: "StorageClass",
          metadata: metaFor("gp2", undefined, 120),
          provisioner: "kubernetes.io/aws-ebs",
          parameters: { fsType: "ext4", type: "gp2" },
          reclaimPolicy: "Delete",
          volumeBindingMode: "WaitForFirstConsumer",
          allowVolumeExpansion: false,
        },
      ],
    ],
    [
      "/serviceaccounts",
      [
        ...new Set(WORKLOADS.map((w) => `${w.namespace}/${w.helm ?? w.name}`)),
        ...NAMESPACES.map((n) => `${n}/default`),
      ].map((id) => {
        const [namespace = "", name = ""] = id.split("/");
        return { apiVersion: "v1", kind: "ServiceAccount", metadata: metaFor(name, namespace, 60) };
      }),
    ],
    [
      "policy/poddisruptionbudgets",
      [
        {
          apiVersion: "policy/v1",
          kind: "PodDisruptionBudget",
          metadata: metaFor("storefront", "shop", 94),
          spec: {
            minAvailable: 2,
            selector: { matchLabels: { "app.kubernetes.io/name": "storefront" } },
          },
          status: { currentHealthy: 3, desiredHealthy: 2, disruptionsAllowed: 1, expectedPods: 3 },
        },
      ],
    ],
    [
      "apiextensions.k8s.io/customresourcedefinitions",
      [
        crd("certificates", "cert-manager.io", "Certificate", "Namespaced", ["cert", "certs"], 120),
        crd("clusterissuers", "cert-manager.io", "ClusterIssuer", "Cluster", [], 120),
        crd("issuers", "cert-manager.io", "Issuer", "Namespaced", [], 120),
        crd(
          "servicemonitors",
          "monitoring.coreos.com",
          "ServiceMonitor",
          "Namespaced",
          ["smon"],
          41,
        ),
        crd(
          "prometheusrules",
          "monitoring.coreos.com",
          "PrometheusRule",
          "Namespaced",
          ["promrule"],
          41,
        ),
      ],
    ],
    [
      "cert-manager.io/certificates",
      [
        {
          apiVersion: "cert-manager.io/v1",
          kind: "Certificate",
          metadata: metaFor("storefront-tls", "shop", 94),
          spec: {
            secretName: "storefront-tls",
            dnsNames: ["shop.demo.glowpod.dev"],
            issuerRef: { name: "letsencrypt", kind: "ClusterIssuer", group: "cert-manager.io" },
          },
          status: {
            conditions: [
              {
                type: "Ready",
                status: "True",
                reason: "Ready",
                message: "Certificate is up to date and has not expired",
                lastTransitionTime: iso(t0 - 52 * 60_000),
              },
            ],
            notAfter: iso(t0 + 89 * DAY),
            renewalTime: iso(t0 + 59 * DAY),
            revision: 4,
          },
        },
      ],
    ],
    [
      "cert-manager.io/clusterissuers",
      [
        {
          apiVersion: "cert-manager.io/v1",
          kind: "ClusterIssuer",
          metadata: metaFor("letsencrypt", undefined, 120),
          spec: {
            acme: {
              server: "https://acme-v02.api.letsencrypt.org/directory",
              privateKeySecretRef: { name: "letsencrypt-account-key" },
              solvers: [{ http01: { ingress: { ingressClassName: "nginx" } } }],
            },
          },
          status: {
            conditions: [
              {
                type: "Ready",
                status: "True",
                reason: "ACMEAccountRegistered",
                message: "The ACME account was registered with the ACME server",
                lastTransitionTime: at(120),
              },
            ],
          },
        },
      ],
    ],
    [
      "monitoring.coreos.com/servicemonitors",
      ["storefront", "payments-api", "search-api"].map((name) => ({
        apiVersion: "monitoring.coreos.com/v1",
        kind: "ServiceMonitor",
        metadata: metaFor(name, "monitoring", 30, { labels: { release: "kube-prometheus-stack" } }),
        spec: {
          selector: { matchLabels: { "app.kubernetes.io/name": name } },
          namespaceSelector: { any: true },
          endpoints: [{ port: "http", path: "/metrics", interval: "30s" }],
        },
      })),
    ],
    [
      "monitoring.coreos.com/prometheusrules",
      [
        {
          apiVersion: "monitoring.coreos.com/v1",
          kind: "PrometheusRule",
          metadata: metaFor("shop-alerts", "monitoring", 30, {
            labels: { release: "kube-prometheus-stack" },
          }),
          spec: {
            groups: [
              {
                name: "shop",
                rules: [
                  {
                    alert: "CheckoutErrorRateHigh",
                    expr: 'sum(rate(http_requests_total{app="checkout",code=~"5.."}[5m])) / sum(rate(http_requests_total{app="checkout"}[5m])) > 0.05',
                    for: "10m",
                    labels: { severity: "critical" },
                  },
                ],
              },
            ],
          },
        },
      ],
    ],
  ];
}
