{{/* Rule group label key. */}}
{{- define "son.rule.groupMatchLabelKey" -}}
{{ printf "%s-%s" (include "son.name" .) "vmrule-group" }}
{{- end }}

{{/* Rule group label value for Slack alerts. */}}
{{- define "son.rule.group.slack" -}}
slack
{{- end }}

{{/* Rule group label value for recorded alerts. */}}
{{- define "son.rule.group.records" -}}
records
{{- end }}

{{/* Function to build matching label for Rule group. */}}
{{- define "son.rule.groupMatchLabel" -}}
{{ include "son.rule.groupMatchLabelKey" .context }}: {{ .value }}
{{- end }}

{{/* Function to build Rule name. */}}
{{- define "son.rule.name" -}}
{{ printf "%s-%s-%s" (include "son.name" .context) "slurm-job" .value }}
{{- end }}

{{/* Function to build Rule group name. */}}
{{- define "son.rule.groupName" -}}
{{ printf "%s-%s" "slurm-job" . }}
{{- end }}

{{/* Function to build alerted Rule record name. */}}
{{- define "son.rule.alertedRecord" -}}
{{ printf "%s_%s_%s" "slurm_job" . "alerted" }}
{{- end }}

{{/* Alerted record keeping interval. */}}
{{- define "son.rule.alertedInterval" -}}
{{ default .Values.interval.alerted "1h" }}
{{- end }}

{{/* Function to build Rule labels. */}}
{{- define "son.rule.labels" -}}
{{ include "son.config.label.severity" . }}: {{ .severity | quote }}
namespace: {{ .context.Values.namespace | quote }}
{{ include "son.config.label.job.id" . }}: {{ include "son.wrapTemplate" "$labels.job_id" | quote }}
{{ include "son.config.label.job.name" . }}: {{ include "son.wrapTemplate" "$labels.job_name" | quote }}
{{ include "son.config.label.job.state" . }}: {{ include "son.wrapTemplate" "$labels.job_state" | quote }}
{{ include "son.config.label.job.stateReason" . }}: {{ include "son.wrapTemplate" "$labels.job_state_reason" | quote }}
{{ include "son.config.label.job.user" . }}: {{ include "son.wrapTemplate" "$labels.user_name" | quote }}
{{ include "son.config.label.alertKey" . }}: {{ printf "job_%s_%s" (include "son.wrapTemplate" "$labels.job_id") (include "son.wrapTemplate" "$labels.job_state") | quote }}
{{- end }}

{{/* MetricsQL selector for error jobs. */}}
{{- define "son.rule.jobSelector.error" -}}
job_state=~"{{ include "son.config.jobStatus.failed" . }}|{{ include "son.config.jobStatus.nodeFail" . }}|{{ include "son.config.jobStatus.oom" . }}"
{{- end }}

{{/* MetricsQL selector for warning jobs. */}}
{{- define "son.rule.jobSelector.warning" -}}
job_state=~"{{ include "son.config.jobStatus.bootFail" . }}|{{ include "son.config.jobStatus.cancelled" . }}|{{ include "son.config.jobStatus.deadline" . }}|{{ include "son.config.jobStatus.preempted" . }}|{{ include "son.config.jobStatus.suspended" . }}|{{ include "son.config.jobStatus.timeout" . }}"
{{- end }}

{{/* MetricsQL selector for good jobs. */}}
{{- define "son.rule.jobSelector.good" -}}
job_state=~"{{ include "son.config.jobStatus.completed" . }}"
{{- end }}

{{/* MetricsQL selector for good jobs. */}}
{{- define "son.rule.jobSelector.system" -}}
user_name!~"^(nebius|soperatorchecks)$"
{{- end }}
