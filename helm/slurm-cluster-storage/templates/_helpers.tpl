{{/* Local storage class */}}
{{- define "slurm-cluster-storage.class.local.name" -}}
    {{- default "slurm-local-pv" .Values.storageClass.local.name | trim | kebabcase | quote -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail volume */}}
{{- define "slurm-cluster-storage.volume.jail.name" -}}
    {{- default "jail" .Values.volume.jail.name | trim | kebabcase -}}
{{- end }}

{{/* Jail PVC name */}}
{{- define "slurm-cluster-storage.volume.jail.pvc" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail.name" .) "pvc" | kebabcase | quote -}}
{{- end }}

{{/* Jail PV name */}}
{{- define "slurm-cluster-storage.volume.jail.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail.name" .) "pv" | kebabcase | quote -}}
{{- end }}

{{/* Jail mount name */}}
{{- define "slurm-cluster-storage.volume.jail.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail.name" .) "mount" | kebabcase | quote -}}
{{- end }}

{{/* Jail storage class name */}}
{{- define "slurm-cluster-storage.volume.jail.storageClass" -}}
    {{- include "slurm-cluster-storage.class.local.name" . -}}
{{- end }}

{{/* Jail size */}}
{{- define "slurm-cluster-storage.volume.jail.size" -}}
    {{- required "Jail volume size is required." .Values.volume.jail.size -}}
{{- end }}

{{/*
---
*/}}

{{/* Spool volume */}}
{{- define "slurm-cluster-storage.volume.spool.name" -}}
    {{- default "spool" .Values.volume.spool.name | trim | kebabcase -}}
{{- end }}

{{/* Spool PVC name */}}
{{- define "slurm-cluster-storage.volume.spool.pvc" -}}
    {{- cat (include "slurm-cluster-storage.volume.spool.name" .) "pvc" | kebabcase | quote -}}
{{- end }}

{{/* Spool PV name */}}
{{- define "slurm-cluster-storage.volume.spool.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.spool.name" .) "pv" | kebabcase | quote -}}
{{- end }}

{{/* Spool mount name */}}
{{- define "slurm-cluster-storage.volume.spool.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.spool.name" .) "mount" | kebabcase | quote -}}
{{- end }}

{{/* Spool storage class name */}}
{{- define "slurm-cluster-storage.volume.spool.storageClass" -}}
    {{- include "slurm-cluster-storage.class.local.name" . -}}
{{- end }}

{{/* Spool size */}}
{{- define "slurm-cluster-storage.volume.spool.size" -}}
    {{- required "Spool volume size is required." .Values.volume.spool.size -}}
{{- end }}

{{/*
---
*/}}

{{/* GPU node group */}}
{{- define "slurm-cluster-storage.nodeGroup.gpu" -}}
    {{- required "GPU node group ID is required." .Values.nodeGroup.gpu.id | quote -}}
{{- end }}

{{/* Non-GPU node group */}}
{{- define "slurm-cluster-storage.nodeGroup.nonGpu" -}}
    {{- required "Non-GPU node group ID is required." .Values.nodeGroup.nonGpu.id | quote -}}
{{- end }}
