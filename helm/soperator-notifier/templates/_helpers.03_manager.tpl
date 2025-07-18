{{/* AlertManager name */}}
{{- define "son.alertManager.name" -}}
{{- include "son.name" . }}
{{- end }}

{{/* AlertManager replicas */}}
{{- define "son.alertManager.replicas" -}}
{{- .Values.alertManager.replicas | default 1 }}
{{- end }}

{{/* AlertManager port */}}
{{- define "son.alertManager.port" -}}
{{- .Values.alertManager.port | default 9093 }}
{{- end }}

{{/* AlertManager URL. */}}
{{- define "son.alertManager.url" -}}
{{ printf "http://vmalertmanager-%s:%s" (include "son.alertManager.name" .) (include "son.alertManager.port" .) }}
{{- end }}
