package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"text/template"

	"glowpod/internal/agent"
)

// AgentNamespace is where the agent runs in an imported cluster.
const AgentNamespace = "glowpod-system"

// manifestData fills the import manifest. Every string goes through json
// (valid YAML), so no value can break out of its field.
type manifestData struct {
	ClusterID string
	HubURL    string
	HubCA     string
	Token     string
	Image     string
	// Owner, when set, becomes cluster-admin: the person who imported it.
	Owner string
}

// manifestTemplate is what kubectl apply installs in an imported cluster.
// The agent's ClusterRole may only impersonate and read role bindings; it
// can read and update its own Secret, nothing else.
var manifestTemplate = template.Must(template.New("import").Funcs(template.FuncMap{
	"q": func(s string) (string, error) {
		b, err := json.Marshal(s)
		return string(b), err
	},
}).Parse(`# Glowpod agent for cluster {{q .ClusterID}}. It connects out to the Glowpod Hub
# and acts there only as the people who sign in to the Hub.
apiVersion: v1
kind: Namespace
metadata:
  name: {{q .Namespace}}
  labels: {app.kubernetes.io/name: glowpod-agent, app.kubernetes.io/managed-by: glowpod-hub}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: glowpod-agent
  namespace: {{q .Namespace}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: glowpod-agent
rules:
  # Act as the person signed in to the Hub. The agent refuses system: names.
  - apiGroups: [""]
    resources: [users, groups]
    verbs: [impersonate]
  # Who has access here, so the Hub lists this cluster only for them.
  - apiGroups: [rbac.authorization.k8s.io]
    resources: [rolebindings, clusterrolebindings]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: glowpod-agent
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: glowpod-agent}
subjects:
  - {kind: ServiceAccount, name: glowpod-agent, namespace: {{q .Namespace}}}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: glowpod-agent
  namespace: {{q .Namespace}}
rules:
  # Its own Secret: the import token becomes a credential on first contact.
  - apiGroups: [""]
    resources: [secrets]
    resourceNames: [glowpod-agent]
    verbs: [get, update]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: glowpod-agent
  namespace: {{q .Namespace}}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: Role, name: glowpod-agent}
subjects:
  - {kind: ServiceAccount, name: glowpod-agent, namespace: {{q .Namespace}}}
---
apiVersion: v1
kind: Secret
metadata:
  name: glowpod-agent
  namespace: {{q .Namespace}}
type: Opaque
stringData:
  {{.KeyHubURL}}: {{q .HubURL}}
  {{.KeyToken}}: {{q .Token}}
{{- if .HubCA}}
  {{.KeyCA}}: {{q .HubCA}}
{{- end}}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: glowpod-agent
  namespace: {{q .Namespace}}
  labels: {app.kubernetes.io/name: glowpod-agent}
spec:
  replicas: 1
  strategy: {type: Recreate}
  selector:
    matchLabels: {app.kubernetes.io/name: glowpod-agent}
  template:
    metadata:
      labels: {app.kubernetes.io/name: glowpod-agent}
    spec:
      serviceAccountName: glowpod-agent
      securityContext:
        runAsNonRoot: true
        seccompProfile: {type: RuntimeDefault}
      containers:
        - name: agent
          image: {{q .Image}}
          command: [/glowpod, agent]
          env:
            - name: POD_NAMESPACE
              valueFrom: {fieldRef: {fieldPath: metadata.namespace}}
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities: {drop: [ALL]}
          resources:
            requests: {cpu: 10m, memory: 32Mi}
            limits: {memory: 128Mi}
{{- if .Owner}}
---
# The person who imported this cluster owns it, as in Rancher. Remove this
# binding to take that away; the agent never had these rights itself.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: glowpod-owner
  labels: {app.kubernetes.io/managed-by: glowpod-hub}
roleRef: {apiGroup: rbac.authorization.k8s.io, kind: ClusterRole, name: cluster-admin}
subjects:
  - {apiGroup: rbac.authorization.k8s.io, kind: User, name: {{q .Owner}}}
{{- end}}
`))

func renderManifest(d manifestData) ([]byte, error) {
	var buf bytes.Buffer
	err := manifestTemplate.Execute(&buf, struct {
		manifestData
		Namespace, KeyHubURL, KeyToken, KeyCA string
	}{d, AgentNamespace, agent.KeyHubURL, agent.KeyToken, agent.KeyCA})
	if err != nil {
		return nil, fmt.Errorf("render the import manifest: %w", err)
	}
	return buf.Bytes(), nil
}

// cleanupCommand removes everything the import manifest created.
func cleanupCommand() string {
	return "kubectl delete namespace " + AgentNamespace +
		" && kubectl delete clusterrole glowpod-agent && kubectl delete clusterrolebinding glowpod-agent glowpod-owner --ignore-not-found"
}
