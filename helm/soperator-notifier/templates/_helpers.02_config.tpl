{{/* AlertManager config name. */}}
{{- define "son.config.name" -}}
{{- include "son.name" . }}
{{- end }}

{{/* How long to wait before sending the initial notification. */}}
{{- define "son.config.route.groupWait" -}}
{{ .Values.interval.groupWait | default "30s" }}
{{- end }}

{{/* How long to wait before sending the initial notification. */}}
{{- define "son.config.route.groupInterval" -}}
{{ .Values.interval.group | default "5m" }}
{{- end }}

{{/* How long to wait before sending the initial notification. */}}
{{- define "son.config.route.repeatInterval" -}}
{{ .Values.interval.repeat | default "25h" }}
{{- end }}

{{/* Labels */}}

{{/* Label for matching AlertManager with its config */}}
{{- define "son.config.label.match" -}}
{{ printf "%s-%s" (include "son.name" .) "slack-route" | trunc 63 | trimSuffix "-" }}: "enabled"
{{- end }}

{{/* Severity label key. */}}
{{- define "son.config.label.severity" -}}
severity
{{- end }}

{{/* Job ID label key. */}}
{{- define "son.config.label.job.id" -}}
job_id
{{- end }}

{{/* Job name label key. */}}
{{- define "son.config.label.job.name" -}}
job_name
{{- end }}

{{/* Job state label key. */}}
{{- define "son.config.label.job.state" -}}
job_state
{{- end }}

{{/* Job state reason label key. */}}
{{- define "son.config.label.job.stateReason" -}}
job_state_reason
{{- end }}

{{/* Job user label key. */}}
{{- define "son.config.label.job.user" -}}
job_user
{{- end }}

{{/* Job user_mail label key. */}}
{{- define "son.config.label.job.user_mail" -}}
job_user_mail
{{- end }}

{{/* Alert key label key. */}}
{{- define "son.config.label.alertKey" -}}
alert_key
{{- end }}

{{/* --- */}}
{{/* Job statuses */}}

{{/* FAILED job status. */}}
{{- define "son.config.jobStatus.failed" -}}
FAILED
{{- end }}

{{/* NODE_FAIL job status. */}}
{{- define "son.config.jobStatus.nodeFail" -}}
NODE_FAIL
{{- end }}

{{/* OUT_OF_MEMORY job status. */}}
{{- define "son.config.jobStatus.oom" -}}
OUT_OF_MEMORY
{{- end }}

{{/* BOOT_FAIL job status. */}}
{{- define "son.config.jobStatus.bootFail" -}}
BOOT_FAIL
{{- end }}

{{/* CANCELLED job status. */}}
{{- define "son.config.jobStatus.cancelled" -}}
CANCELLED
{{- end }}

{{/* DEADLINE job status. */}}
{{- define "son.config.jobStatus.deadline" -}}
DEADLINE
{{- end }}

{{/* PREEMPTED job status. */}}
{{- define "son.config.jobStatus.preempted" -}}
PREEMPTED
{{- end }}

{{/* SUSPENDED job status. */}}
{{- define "son.config.jobStatus.suspended" -}}
SUSPENDED
{{- end }}

{{/* TIMEOUT job status. */}}
{{- define "son.config.jobStatus.timeout" -}}
TIMEOUT
{{- end }}

{{/* COMPLETED job status. */}}
{{- define "son.config.jobStatus.completed" -}}
COMPLETED
{{- end }}
