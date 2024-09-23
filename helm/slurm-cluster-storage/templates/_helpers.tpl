{{/* Local storage class */}}
{{- define "slurm-cluster-storage.class.local.name" -}}
    {{- required "Local storage class name is required." .Values.storageClass.local.name | trim | kebabcase | quote -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail volume */}}
{{- define "slurm-cluster-storage.volume.jail.name" -}}
    {{- required "Jail volume name is required." .Values.volume.jail.name | trim | kebabcase -}}
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

{{/* Jail storage type */}}
{{- define "slurm-cluster-storage.volume.jail.type" -}}
    {{- if not (or (eq .Values.volume.jail.type "filestore") (eq .Values.volume.jail.type "glusterfs")) -}}
        {{- fail "Jail volume type must be one of 'filestore' or 'glusterfs'." -}}
    {{- end }}
    {{- required "Jail volume type is required." .Values.volume.jail.type | trim -}}
{{- end }}

{{/* Jail filestore device name */}}
{{- define "slurm-cluster-storage.volume.jail.device" -}}
    {{- if eq .Values.volume.jail.type "filestore" -}}
        {{- required "Jail volume filestore device name is required." .Values.volume.jail.filestoreDeviceName | trim | kebabcase -}}
    {{- else }}
        {{- "" -}}
    {{- end }}
{{- end }}

{{/* Jail GlusterFS host name */}}
{{- define "slurm-cluster-storage.volume.jail.hostname" -}}
    {{- if eq .Values.volume.jail.type "glusterfs" -}}
        {{- required "Jail volume GlusterFS hostname is required." .Values.volume.jail.glusterfsHostName | trim | kebabcase -}}
    {{- else }}
        {{- "" -}}
    {{- end }}
{{- end }}

{{/*
---
*/}}

{{/* Controller spool volume */}}
{{- define "slurm-cluster-storage.volume.controller-spool.name" -}}
    {{- required "Controller spool volume name is required." .Values.volume.controllerSpool.name | trim | kebabcase -}}
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
    {{- required "Controller spool volume size is required." .Values.volume.controllerSpool.size -}}
{{- end }}

{{/* Controller spool device name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.device" -}}
    {{- required "Controller spool Filestore device name is required." .Values.volume.controllerSpool.filestoreDeviceName | trim | kebabcase -}}
{{- end }}

{{/*
---
*/}}

{{/* Controller spool volume */}}
{{- define "slurm-cluster-storage.volume.accounting.name" -}}
    {{- required "Controller spool volume name is required." .Values.volume.accounting.name | trim | kebabcase -}}
{{- end }}

{{/* Controller spool PV name */}}
{{- define "slurm-cluster-storage.volume.accounting.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.accounting.name" .) "pv" | kebabcase | quote -}}
{{- end }}

{{/* Controller spool mount name */}}
{{- define "slurm-cluster-storage.volume.accounting.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.accounting.name" .) "mount" | kebabcase | quote -}}
{{- end }}

{{/* Controller spool storage class name */}}
{{- define "slurm-cluster-storage.volume.accounting.storageClass" -}}
    {{- include "slurm-cluster-storage.class.local.name" . -}}
{{- end }}

{{/* Controller spool size */}}
{{- define "slurm-cluster-storage.volume.accounting.size" -}}
    {{- required "Controller spool volume size is required." .Values.volume.accounting.size -}}
{{- end }}

{{/* Controller spool device name */}}
{{- define "slurm-cluster-storage.volume.accounting.device" -}}
    {{- required "Controller spool Filestore device name is required." .Values.volume.accounting.filestoreDeviceName | trim | kebabcase -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail submount volume */}}
{{- define "slurm-cluster-storage.volume.jail-submount.name" -}}
    {{- cat "jail-submount" (required "Jail submount name is required." .name) | trim | kebabcase -}}
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

{{/* Jail submount device name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.device" -}}
    {{- required "Jail submount Filestore device name is required." .filestoreDeviceName | trim | kebabcase -}}
{{- end }}
