{{/* sshdKeysName secret */}}
{{- define "slurm-cluster.secret.sshdKeysName" -}}
{{- .Values.secrets.sshdKeysName }}
{{- end }}

{{/*
---
*/}}
