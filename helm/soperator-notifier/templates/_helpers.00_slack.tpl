{{/* Slack webhook secret name. */}}
{{- define "son.slack.webhook.secret.name" -}}
{{- printf "%s-%s" (include "son.name" .) "slack-webhook" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Slack webhook secret key. */}}
{{- define "son.slack.webhook.secret.key" -}}
url
{{- end }}

{{/* Slack webhook secret name. */}}
{{- define "son.slack.webhook.url" -}}
{{- required "Slack Webhook URL must be provided." .Values.slack.webhookUrl }}
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

{{/* --- */}}

{{/* Message prefix for particular job. */}}
{{- define "son.slack.msg.jobPrefix" -}}
- Job *{{ include "son.wrapTemplate" "$job" }}* (ID `{{ include "son.wrapTemplate" "$id" }}`){{ include "son.wrapTemplate" "if $user" }}, submitted by *{{ include "son.wrapTemplate" "$user" }}*,{{ include "son.wrapTemplate" "end" }}
{{- end }}

{{/* Message text for job state reason. */}}
{{- define "son.slack.msg.jobReason" -}}
(reason: `{{ include "son.wrapTemplate" "$reason" }}`)
{{- end }}

{{/* --- */}}

{{/* Color for group with error severity. */}}
{{- define "son.slack.msg.color.error" -}}
{{ default .Values.slack.severityColor.error "danger" }}
{{- end }}

{{/* Color for group with warning severity. */}}
{{- define "son.slack.msg.color.warning" -}}
{{ default .Values.slack.severityColor.warning "#F28B30" }}
{{- end }}

{{/* Color for group with good severity. */}}
{{- define "son.slack.msg.color.good" -}}
{{ default .Values.slack.severityColor.good "good" }}
{{- end }}

{{/* Color for group with unknown severity. */}}
{{- define "son.slack.msg.color.unknown" -}}
{{ default .Values.slack.severityColor.unknown "#807F83" }}
{{- end }}
