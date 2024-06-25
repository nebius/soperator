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

{{/*
---
*/}}

{{/* SSH root public keys secret name */}}
{{- define "slurm-cluster.secret.sshRootPublicKeys.name" -}}
{{- if .Values.secrets.sshRootPublicKeys.create }}
{{- "ssh-root-public-keys" | quote -}}
{{- else }}
{{- required ".Values.secrets.sshRootPublicKeys.name must be provided if .Values.secrets.sshRootPublicKeys.create is disabled" .Values.secrets.sshRootPublicKeys.name | quote -}}
{{- end }}
{{- end }}

{{/* SSH root public keys secret key */}}
{{- define "slurm-cluster.secret.sshRootPublicKeys.key" -}}
{{- if .Values.secrets.sshRootPublicKeys.create }}
{{- "authorized_keys" | quote -}}
{{- else }}
{{- required ".Values.secrets.sshRootPublicKeys.key must be provided if .Values.secrets.sshRootPublicKeys.create is disabled" .Values.secrets.sshRootPublicKeys.key | quote -}}
{{- end }}
{{- end }}
