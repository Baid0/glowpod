import type { ResourceType } from "../../lib/types";

/** What a template needs to know about the kind it's for. */
type Kind = Pick<ResourceType, "group" | "version" | "kind" | "namespaced">;

const APP = "my-app";

/** Bodies for common kinds: everything below metadata. */
const BODIES: Record<string, (name: string) => string> = {
  "apps/Deployment": (name) => `spec:
  replicas: 2
  selector:
    matchLabels:
      app: ${name}
  template:
    metadata:
      labels:
        app: ${name}
    spec:
      containers:
        - name: ${name}
          image: nginx:stable
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
`,
  "apps/StatefulSet": (name) => `spec:
  serviceName: ${name}
  replicas: 1
  selector:
    matchLabels:
      app: ${name}
  template:
    metadata:
      labels:
        app: ${name}
    spec:
      containers:
        - name: ${name}
          image: nginx:stable
          volumeMounts:
            - name: data
              mountPath: /data
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: [ReadWriteOnce]
        resources:
          requests:
            storage: 1Gi
`,
  "apps/DaemonSet": (name) => `spec:
  selector:
    matchLabels:
      app: ${name}
  template:
    metadata:
      labels:
        app: ${name}
    spec:
      containers:
        - name: ${name}
          image: busybox:stable
          command: [sh, -c, "while true; do sleep 3600; done"]
`,
  "/Pod": (name) => `spec:
  containers:
    - name: ${name}
      image: busybox:stable
      command: [sh, -c, "echo hello; sleep 3600"]
`,
  "/Service": (name) => `spec:
  selector:
    app: ${name}
  ports:
    - name: http
      port: 80
      targetPort: 80
`,
  "/ConfigMap": () => `data:
  LOG_LEVEL: info
`,
  "/Secret": () => `type: Opaque
# stringData takes plain text; the cluster stores it encoded in data.
stringData:
  password: change-me
`,
  "/PersistentVolumeClaim": () => `spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
`,
  "/ServiceAccount": () => "",
  "/Namespace": () => "",
  "batch/Job": (name) => `spec:
  backoffLimit: 2
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: ${name}
          image: busybox:stable
          command: [sh, -c, "echo done"]
`,
  "batch/CronJob": (name) => `spec:
  schedule: "0 * * * *"
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: ${name}
              image: busybox:stable
              command: [sh, -c, "date"]
`,
  "networking.k8s.io/Ingress": (name) => `spec:
  rules:
    - host: ${name}.example.com
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: ${name}
                port:
                  number: 80
`,
  "networking.k8s.io/NetworkPolicy": (name) => `spec:
  podSelector:
    matchLabels:
      app: ${name}
  policyTypes: [Ingress]
  ingress:
    - from:
        - podSelector: {}
`,
  "autoscaling/HorizontalPodAutoscaler": (name) => `spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: ${name}
  minReplicas: 2
  maxReplicas: 5
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 70
`,
  "policy/PodDisruptionBudget": (name) => `spec:
  minAvailable: 1
  selector:
    matchLabels:
      app: ${name}
`,
  "rbac.authorization.k8s.io/Role": () => `rules:
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, watch]
`,
  "rbac.authorization.k8s.io/ClusterRole": () => `rules:
  - apiGroups: [""]
    resources: [pods]
    verbs: [get, list, watch]
`,
  "rbac.authorization.k8s.io/RoleBinding": (name) => `roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: ${name}
`,
  "rbac.authorization.k8s.io/ClusterRoleBinding": (name) => `roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: view
subjects:
  - kind: Group
    apiGroup: rbac.authorization.k8s.io
    name: ${name}
`,
};

/** A sensible name to start from: my-app for workloads, my-<kind> otherwise. */
function nameFor(kind: Kind): string {
  if (/^(Deployment|StatefulSet|DaemonSet|Pod|Service|Job|CronJob|Ingress)$/.test(kind.kind)) {
    return APP;
  }
  return `my-${kind.kind.replace(/[A-Z]/g, (c, i: number) => (i ? "-" : "") + c.toLowerCase())}`;
}

/**
 * A starting manifest for a new object of kind in namespace: a useful
 * template for common kinds, otherwise apiVersion, kind and metadata with an
 * empty spec, so any CRD can be created too.
 */
export function templateFor(kind: Kind, namespace: string): string {
  const apiVersion = kind.group ? `${kind.group}/${kind.version}` : kind.version;
  const name = nameFor(kind);
  const body = BODIES[`${kind.group}/${kind.kind}`];
  const metadata =
    `metadata:\n  name: ${name}\n` +
    (kind.namespaced ? `  namespace: ${namespace || "default"}\n` : "") +
    (body?.(name).includes("app: ") && kind.kind !== "NetworkPolicy"
      ? `  labels:\n    app: ${name}\n`
      : "");
  return `apiVersion: ${apiVersion}\nkind: ${kind.kind}\n${metadata}${body ? body(name) : "spec: {}\n"}`;
}
