{{/*
Expand the name of the chart.
*/}}
{{- define "soperator-fluxcd.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "soperator-fluxcd.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soperator-fluxcd.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a Kubernetes resource name with a version suffix.
*/}}
{{- define "soperator-fluxcd.versionedName" -}}
{{- $name := index . 0 -}}
{{- $version := index . 1 -}}
{{- printf "%s-%s" $name $version | lower | replace "+" "-" | replace "_" "-" | trunc 253 | trimSuffix "-" }}
{{- end }}

{{/*
Base name for the custom supervisord ConfigMap created by the custom-configmaps chart.
*/}}
{{- define "soperator-fluxcd.customSupervisordConfigMapBaseName" -}}
{{- $name := .Values.customConfigmaps.supervisordConfigMapName | default "custom-supervisord-config" -}}
{{- with .Values.customConfigmaps.values -}}
{{- with .configMaps -}}
{{- with .supervisord -}}
{{- with .name -}}
{{- $name = . -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- with .Values.customConfigmaps.overrideValues -}}
{{- with .configMaps -}}
{{- with .supervisord -}}
{{- with .name -}}
{{- $name = . -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- end -}}
{{- $name -}}
{{- end }}

{{/*
Versioned name for the custom supervisord ConfigMap.
*/}}
{{- define "soperator-fluxcd.customSupervisordConfigMapName" -}}
{{- include "soperator-fluxcd.versionedName" (list (include "soperator-fluxcd.customSupervisordConfigMapBaseName" .) .Values.customConfigmaps.version) -}}
{{- end }}

{{/*
Common labels
*/}}
{{- define "soperator-fluxcd.labels" -}}
helm.sh/chart: {{ include "soperator-fluxcd.chart" . }}
{{ include "soperator-fluxcd.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "soperator-fluxcd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soperator-fluxcd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "soperator-fluxcd.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "soperator-fluxcd.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Validate observability.publicEndpointTokenKind is one of: secret, hostPath
*/}}
{{- define "soperator-fluxcd.validatePublicEndpointTokenKind" -}}
{{- $kind := .Values.observability.publicEndpointTokenKind | default "" -}}

{{- if not (or (eq $kind "secret") (eq $kind "hostPath")) -}}
  {{- fail (printf "observability.publicEndpointTokenKind must be one of: secret, hostPath (got %q)" $kind) -}}
{{- end -}}
{{- end -}}
