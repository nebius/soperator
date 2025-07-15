{{/* Label for matching AlertManager with its config */}}
{{- define "son.label.configMatcher" -}}
{{ printf "%s-%s" (include "son.name" .) "slack-route" | trunc 63 | trimSuffix "-" }}: "enabled"
{{- end }}
