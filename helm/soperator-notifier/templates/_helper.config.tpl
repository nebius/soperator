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
job-id
{{- end }}

{{/* Job name label key. */}}
{{- define "son.config.label.job.name" -}}
job-name
{{- end }}

{{/* Job state label key. */}}
{{- define "son.config.label.job.state" -}}
job-state
{{- end }}

{{/* Job state reason label key. */}}
{{- define "son.config.label.job.stateReason" -}}
job-state-reason
{{- end }}

{{/* Job user label key. */}}
{{- define "son.config.label.job.user" -}}
job-user
{{- end }}

{{/* Alert key label key. */}}
{{- define "son.config.label.alertKey" -}}
alert-key
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
