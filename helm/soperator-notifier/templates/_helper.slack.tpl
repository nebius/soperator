{{/* Slack webhook secret name. */}}
{{- define "son.slack.webhookSecretName" -}}
{{- printf "%s-%s" (include "son.name" .) "slack-webhook" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* --- */}}

{{/* Error severity. */}}
{{- define "son.slack.severity.error" -}}
error
{{- end }}

{{/* Warning severity. */}}
{{- define "son.slack.severity.warning" -}}
warning
{{- end }}

{{/* Good severity. */}}
{{- define "son.slack.severity.good" -}}
good
{{- end }}
