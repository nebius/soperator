{{/* Data source for Alert metrics. */}}
{{- define "son.alert.dataSourceUrl" -}}
{{ default .Values.dataSourceUrl "http://vmsingle-metrics-victoria-metrics-k8s-stack:8429" }}
{{- end }}

{{/* Metric evaluation interval for Alert. */}}
{{- define "son.alert.evaluationInterval" -}}
{{ default .Values.interval.evaluation "30s" }}
{{- end }}
