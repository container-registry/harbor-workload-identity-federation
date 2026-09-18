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

{{/*
Image tag. Releases publish the deployer image as vX.Y.Z, while Chart.AppVersion
is a bare SemVer, so the default needs the prefix back or the pull 404s.

appVersion belongs to the binary train alone. The chart train releases through
release-please's helm strategy, whose ChartYaml updater writes `version` and
nothing else, so a chart-only release cannot move this tag to an image that was
never built. `task version-check` asserts that invariant on every CI run.
*/}}
{{- define "credential-provider-harbor.imageTag" -}}
{{- if .Values.image.tag }}
{{- .Values.image.tag }}
{{- else }}
{{- printf "v%s" (.Chart.AppVersion | required "Chart.appVersion is empty, so the deployer image tag cannot be resolved. Set image.tag explicitly.") }}
{{- end }}
{{- end }}

{{/*
Marker file as the installer container sees it: the host path under hostRoot.
*/}}
{{- define "credential-provider-harbor.markerPath" -}}
{{- printf "%s%s" (.Values.installer.hostRoot | trimSuffix "/") .Values.installer.installedMarker }}
{{- end }}

{{/*
Directory holding the marker, on the host. It gets its own hostPath volume: the
hostRoot mount is the node's root filesystem without its submounts, so on an
image that carries /var as a mount of its own the marker would otherwise be
written inside the container and lost with the pod.
*/}}
{{- define "credential-provider-harbor.markerDir" -}}
{{- .Values.installer.installedMarker | dir }}
{{- end }}

{{/*
Names of the environment variables the installer actually reads to decide what
lands on the node. The install ID hashes these and nothing else, so an extraEnv
entry the installer never looks at (a proxy setting, a debug flag) cannot change
the ID and force a pointless kubelet restart on every node in the fleet.
Anything here that extraEnv overrides does change the ID, PRESERVE_ECR_PROVIDER
included, because it changes the install.
*/}}
{{- define "credential-provider-harbor.installAffectingEnvNames" -}}
PROFILE HOST_ROOT SOURCE_BINARY BINARY_NAME REGISTRY_HOST REGISTRY_AUDIENCE REGISTRY_USERNAME MATCH_IMAGES CACHE_DURATION BIN_DIR CONFIG_DIR CONFIG_FILE CONFIG_PATH CONFIG_FORMAT CONFIGURE_KUBELET RESTART_KUBELET KUBELET_SERVICE SYSTEMD_DROP_IN_PATH FORCE_KUBELET_EXECSTART K3S_CONFIG_DROP_IN_PATH PRESERVE_ECR_PROVIDER
{{- end }}

{{/*
The chart's own installer environment: every variable that decides what the
installer writes to the node. Kept in one place because the install ID hashes
exactly this list, and a variable that changes the node install but not the ID
would let a stale marker satisfy the readiness probe. Pod-lifecycle settings
(SLEEP_FOREVER) and the marker path itself deliberately stay out: neither
changes the node install.
*/}}
{{- define "credential-provider-harbor.chartInstallerEnv" -}}
- name: PROFILE
  value: {{ .Values.profile | quote }}
- name: HOST_ROOT
  value: {{ .Values.installer.hostRoot | quote }}
- name: SOURCE_BINARY
  value: /usr/local/bin/credential-provider-harbor
- name: BINARY_NAME
  value: {{ .Values.credentialProvider.binaryName | quote }}
- name: REGISTRY_HOST
  value: {{ required "registry.host is required" .Values.registry.host | quote }}
- name: REGISTRY_AUDIENCE
  value: {{ include "credential-provider-harbor.audience" . | quote }}
- name: REGISTRY_USERNAME
  value: {{ .Values.registry.username | quote }}
- name: MATCH_IMAGES
  value: {{ default .Values.registry.host (join "," .Values.registry.matchImages) | quote }}
- name: CACHE_DURATION
  value: {{ .Values.registry.cacheDuration | quote }}
- name: BIN_DIR
  value: {{ .Values.credentialProvider.binDir | quote }}
- name: CONFIG_PATH
  value: {{ .Values.credentialProvider.configPath | quote }}
- name: CONFIG_FORMAT
  value: {{ .Values.credentialProvider.configFormat | quote }}
- name: CONFIGURE_KUBELET
  value: {{ .Values.kubelet.configure | quote }}
- name: RESTART_KUBELET
  value: {{ .Values.kubelet.restart | quote }}
- name: KUBELET_SERVICE
  value: {{ .Values.kubelet.serviceName | quote }}
- name: SYSTEMD_DROP_IN_PATH
  value: {{ .Values.kubelet.systemdDropInPath | quote }}
- name: FORCE_KUBELET_EXECSTART
  value: {{ .Values.kubelet.forceExecStartOverride | quote }}
- name: K3S_CONFIG_DROP_IN_PATH
  value: {{ .Values.kubelet.k3sConfigDropInPath | quote }}
{{- end }}

{{/*
What the installer container gets: the chart's variables plus everything in
extraEnv, recognized or not. extraEnv comes last, so an entry that repeats a
name above wins, which is what makes it an override.
*/}}
{{- define "credential-provider-harbor.installerEnv" -}}
{{- include "credential-provider-harbor.chartInstallerEnv" . }}
{{- with .Values.extraEnv }}
{{- toYaml . | nindent 0 }}
{{- end }}
{{- end }}

{{/*
The same list narrowed to what the install ID is allowed to depend on: the
chart's variables, plus only those extraEnv entries the installer reads. An
entry it ignores stays out, so setting one does not change the ID and does not
roll a kubelet restart across the fleet for a node install that is identical.
*/}}
{{- define "credential-provider-harbor.installIDEnv" -}}
{{- include "credential-provider-harbor.chartInstallerEnv" . }}
{{- $names := splitList " " (include "credential-provider-harbor.installAffectingEnvNames" . | trim) }}
{{- $hashed := list }}
{{- range .Values.extraEnv }}
{{- if has .name $names }}
{{- $hashed = append $hashed . }}
{{- end }}
{{- end }}
{{- with $hashed }}
{{- toYaml . | nindent 0 }}
{{- end }}
{{- end }}

{{/*
Identifier for one installed configuration: the deployer image plus every
environment variable that decides what lands on the node. The installer writes
it into the marker file and the readiness probe greps for it, so a marker from
an earlier revision cannot report this pod Ready before it has installed
anything. Kept out of the hashed material itself, or it could not be computed.
*/}}
{{- define "credential-provider-harbor.installID" -}}
{{- $material := printf "%s:%s\n%s"
      .Values.image.repository
      (include "credential-provider-harbor.imageTag" .)
      (include "credential-provider-harbor.installIDEnv" .) -}}
{{- $material | sha256sum | trunc 16 }}
{{- end }}
