{{- define "glowpod.name" -}}
{{- .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "glowpod.fullname" -}}
{{- if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "glowpod.selectorLabels" -}}
app.kubernetes.io/name: {{ include "glowpod.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "glowpod.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{ include "glowpod.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "glowpod.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "glowpod.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/* Impersonation is needed for SSO without token pass-through, and for LDAP. */}}
{{- define "glowpod.impersonates" -}}
{{- if or (and .Values.auth.oidc.enabled (not .Values.auth.oidc.passToken)) .Values.auth.ldap.enabled -}}true{{- end -}}
{{- end -}}

{{- define "glowpod.secretName" -}}
{{- printf "%s-auth" (include "glowpod.fullname" .) -}}
{{- end -}}

{{- define "glowpod.oidcSecret" -}}
{{- default (include "glowpod.secretName" .) .Values.auth.oidc.existingSecret -}}
{{- end -}}

{{- define "glowpod.ldapSecret" -}}
{{- default (include "glowpod.secretName" .) .Values.auth.ldap.existingSecret -}}
{{- end -}}

{{- define "glowpod.validate" -}}
{{- if and .Values.auth.oidc.enabled (not .Values.publicURL) -}}
{{- fail "auth.oidc needs publicURL (the URL people open, for the SSO redirect)" -}}
{{- end -}}
{{- if and .Values.auth.oidc.enabled (or (not .Values.auth.oidc.issuer) (not .Values.auth.oidc.clientID)) -}}
{{- fail "auth.oidc needs issuer and clientID" -}}
{{- end -}}
{{- if and .Values.auth.ldap.enabled (or (not .Values.auth.ldap.url) (not .Values.auth.ldap.userBase)) -}}
{{- fail "auth.ldap needs url and userBase" -}}
{{- end -}}
{{- if not (or .Values.auth.token.enabled .Values.auth.oidc.enabled .Values.auth.ldap.enabled) -}}
{{- fail "turn on at least one of auth.token, auth.oidc, auth.ldap" -}}
{{- end -}}
{{- if and (gt (int .Values.replicaCount) 1) (not .Values.allowManyReplicas) -}}
{{- fail "sessions live in memory: keep replicaCount at 1, or set allowManyReplicas with sticky sessions" -}}
{{- end -}}
{{- end -}}
