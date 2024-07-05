{{/* Local storage class */}}
{{- define "slurm-cluster-storage.class.local.name" -}}
    {{- default "slurm-local-pv" $.Values.storageClass.local.name | trim | kebabcase | quote -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail storage type */}}
{{- define "slurm-cluster-storage.volume.jail.type" -}}
    {{- default "glusterfs" .Values.volume.jail.type | trim -}}
{{- end }}

{{/* Jail filestore device name */}}
{{- define "slurm-cluster-storage.volume.jail.device" -}}
    {{- default "jail" .Values.volume.jail.filestoreDeviceName | trim | kebabcase -}}
{{- end }}

{{/* Jail GlusterFS host name */}}
{{- define "slurm-cluster-storage.volume.jail.hostname" -}}
    {{- .Values.volume.jail.glusterfsHostName | trim | kebabcase -}}
{{- end }}

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

{{/* Controller spool device name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.device" -}}
    {{- default "controller-spool" .Values.volume.controllerSpool.filestoreDeviceName | trim | kebabcase -}}
{{- end }}

{{/* Controller spool volume */}}
{{- define "slurm-cluster-storage.volume.controller-spool.name" -}}
    {{- default "controller-spool" .Values.volume.controllerSpool.name | trim | kebabcase -}}
{{- end }}

{{/* Controller spool PVC name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.pvc" -}}
    {{- cat (include "slurm-cluster-storage.volume.controller-spool.name" .) "pvc" | kebabcase | quote -}}
{{- end }}

{{/* Controller spool PV name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.controller-spool.name" .) "pv" | kebabcase | quote -}}
{{- end }}

{{/* Controller spool mount name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.controller-spool.name" .) "mount" | kebabcase | quote -}}
{{- end }}

{{/* Controller spool storage class name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.storageClass" -}}
    {{- include "slurm-cluster-storage.class.local.name" . -}}
{{- end }}

{{/* Controller spool size */}}
{{- define "slurm-cluster-storage.volume.controller-spool.size" -}}
    {{- required "Spool volume size is required." .Values.volume.controllerSpool.size -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail submount device name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.device" -}}
    {{- required "Jail submount device name is required." .filestoreDeviceName | trim | kebabcase -}}
{{- end }}

{{/* Jail submount volume */}}
{{- define "slurm-cluster-storage.volume.jail-submount.name" -}}
    {{- required "Jail submount name is required." .name | trim | kebabcase -}}
{{- end }}

{{/* Jail submount PVC name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.pvc" -}}
  {{- cat (include "slurm-cluster-storage.volume.jail-submount.name" .) "pvc" | kebabcase | quote -}}
{{- end }}

{{/* Jail submount PV name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail-submount.name" .) "pv" | kebabcase | quote -}}
{{- end }}

{{/* Jail submount mount name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail-submount.name" .) "mount" | kebabcase | quote -}}
{{- end }}

{{/* Jail submount storage class name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.storageClass" -}}
    {{- include "slurm-cluster-storage.class.local.name" . -}}
{{- end }}

{{/* Jail submount size */}}
{{- define "slurm-cluster-storage.volume.jail-submount.size" -}}
    {{- required "Jail submount volume size is required." .size -}}
{{- end }}

{{/*
---
*/}}

{{/* GPU node group */}}
{{- define "slurm-cluster-storage.nodeGroup.gpu" -}}
    {{- required "GPU node group ID is required." $.Values.nodeGroup.gpu.id | quote -}}
{{- end }}

{{/* Non-GPU node group */}}
{{- define "slurm-cluster-storage.nodeGroup.nonGpu" -}}
    {{- required "Non-GPU node group ID is required." $.Values.nodeGroup.nonGpu.id | quote -}}
{{- end }}
