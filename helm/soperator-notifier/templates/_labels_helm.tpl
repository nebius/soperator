{{/* Common labels */}}
{{- define "son.labels" -}}
helm.sh/chart: {{ include "son.chart" . }}
{{ include "son.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Selector labels */}}
{{- define "son.selectorLabels" -}}
app.kubernetes.io/name: {{ include "son.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}
