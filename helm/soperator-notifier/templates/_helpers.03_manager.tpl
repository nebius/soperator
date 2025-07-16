{{/* AlertManager name */}}
{{- define "son.alertManager.name" -}}
{{- include "son.name" . }}
{{- end }}

{{/* AlertManager replicas */}}
{{- define "son.alertManager.replicas" -}}
{{- default .Values.alertManager.replicas 1 }}
{{- end }}

{{/* AlertManager port */}}
{{- define "son.alertManager.port" -}}
{{- default .Values.alertManager.port 9093 }}
{{- end }}

{{/* AlertManager URL. */}}
{{- define "son.alertManager.url" -}}
{{ printf "http://vmalertmanager-%s:%s" (include "son.alertManager.name" .) (include "son.alertManager.port" .) }}
{{- end }}
