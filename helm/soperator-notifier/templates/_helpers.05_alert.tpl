{{/* Data source for Alert metrics. */}}
{{- define "son.alert.dataSourceUrl" -}}
{{ .Values.dataSourceUrl | default "http://vmsingle-metrics-victoria-metrics-k8s-stack:8429" }}
{{- end }}

{{/* Metric evaluation interval for Alert. */}}
{{- define "son.alert.evaluationInterval" -}}
{{ .Values.interval.evaluation | default "30s" }}
{{- end }}
