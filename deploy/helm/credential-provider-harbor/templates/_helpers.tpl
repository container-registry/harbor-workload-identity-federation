{{/*
Chart name, truncated to 63 chars.
*/}}
{{- define "credential-provider-harbor.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name. Release + chart name, max 63 chars.
*/}}
{{- define "credential-provider-harbor.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart label.
*/}}
{{- define "credential-provider-harbor.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "credential-provider-harbor.labels" -}}
helm.sh/chart: {{ include "credential-provider-harbor.chart" . }}
{{ include "credential-provider-harbor.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "credential-provider-harbor.selectorLabels" -}}
app.kubernetes.io/name: {{ include "credential-provider-harbor.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "credential-provider-harbor.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "credential-provider-harbor.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Namespace for namespaced resources.
*/}}
{{- define "credential-provider-harbor.namespace" -}}
{{- .Release.Namespace }}
{{- end }}

{{/*
Registry audience (defaults to registry.host).
*/}}
{{- define "credential-provider-harbor.audience" -}}
{{- default .Values.registry.host .Values.registry.audience }}
{{- end }}

{{/*
Node audience RBAC resource name.
*/}}
{{- define "credential-provider-harbor.nodeAudienceRoleName" -}}
{{- default (printf "%s-node-audience-token" (include "credential-provider-harbor.fullname" .)) .Values.nodeAudienceRbac.name | trunc 63 | trimSuffix "-" }}
{{- end }}
