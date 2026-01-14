{{/*
Expand the name of the chart.
*/}}
{{- define "soperator-custom-configmaps.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soperator-custom-configmaps.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "soperator-custom-configmaps.labels" -}}
helm.sh/chart: {{ include "soperator-custom-configmaps.chart" . }}
{{ include "soperator-custom-configmaps.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "soperator-custom-configmaps.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soperator-custom-configmaps.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
