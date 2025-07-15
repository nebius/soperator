{{/* Rule group label key. */}}
{{- define "son.rule.group" -}}
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
{{- define "son.rule.groupLabel" -}}
{{ include "son.rule.group" .context }}: {{ .value }}
{{- end }}

{{/* Function to build Rule name. */}}
{{- define "son.rule.name" -}}
{{ printf "%s-%s-%s" (include "son.name" .context) "slurm-job" .value }}
{{- end }}
