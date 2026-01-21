{{- define "soperator-monitoring-dashboards.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "soperator-monitoring-dashboards.fullname" -}}
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

{{- define "soperator-monitoring-dashboards.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "soperator-monitoring-dashboards.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
grafana_dashboard: "1"
{{- end }}

{{- define "soperator-monitoring-dashboards.namePrefix" -}}
{{- default .Release.Name .Values.namePrefix -}}
{{- end }}

{{- define "soperator-monitoring-dashboards.dashboardName" -}}
{{- $base := base . -}}
{{- $id := trimSuffix ".json" $base -}}
{{- replace "_" "-" $id -}}
{{- end }}

{{- define "soperator-monitoring-dashboards.cmName" -}}
{{- printf "%s-%s" .namePrefix .dashboardName | trunc 63 | trimSuffix "-" -}}
{{- end }}
