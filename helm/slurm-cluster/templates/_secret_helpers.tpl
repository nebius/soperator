{{/* Munge key secret name */}}
{{- define "slurm-cluster.secret.mungeKey.name" -}}
{{- if .Values.secrets.mungeKey.create }}
{{- "munge-key" | quote -}}
{{- else }}
{{- required ".Values.secrets.mungeKey.name must be provided if .Values.secrets.mungeKey.create is disabled" .Values.secrets.mungeKey.name | quote -}}
{{- end }}
{{- end }}

{{/* Munge key secret key */}}
{{- define "slurm-cluster.secret.mungeKey.key" -}}
{{- if .Values.secrets.mungeKey.create }}
{{- "munge.key" | quote -}}
{{- else }}
{{- required ".Values.secrets.mungeKey.key must be provided if .Values.secrets.mungeKey.create is disabled" .Values.secrets.mungeKey.key | quote -}}
{{- end }}
{{- end }}

{{/* sshdKeysName secret */}}
{{- define "slurm-cluster.secret.sshdKeysName" -}}
{{- .Values.secrets.sshdKeysName }}
{{- end }}

{{/*
---
*/}}
