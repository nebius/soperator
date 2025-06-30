{{/* Works as the sprig/kebabcase without putting dash before the first digit in the word */}}
{{- define "mashedkebab" -}}
    {{- /* CamelCase -> kebab */ -}}
    {{- $s := regexReplaceAll "([a-z])([A-Z])" . "${1}-${2}" -}}
    {{- $s = regexReplaceAll "([A-Z])([A-Z][a-z])" $s "${1}-${2}" -}}
    {{- /* Lower uppercases */ -}}
    {{- $s = lower $s -}}
    {{- /* Turn alphanumericals into dash */ -}}
    {{- $s = regexReplaceAll "[^a-z0-9]+" $s "-" -}}
    {{- /* Compress sequence of dashes into one dash */ -}}
    {{- regexReplaceAll "-{2,}" $s "-" -}}
{{- end }}

{{/*
---
*/}}

{{/* Local storage class */}}
{{- define "slurm-cluster-storage.class.local.name" -}}
    {{- required "Local storage class name is required." .Values.storageClass.local.name | trim | include "mashedkebab" | quote -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail volume */}}
{{- define "slurm-cluster-storage.volume.jail.name" -}}
    {{- required "Jail volume name is required." .Values.volume.jail.name | trim | include "mashedkebab" -}}
{{- end }}

{{/* Jail PVC name */}}
{{- define "slurm-cluster-storage.volume.jail.pvc" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail.name" .) "pvc" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Jail PV name */}}
{{- define "slurm-cluster-storage.volume.jail.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail.name" .) "pv" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Jail mount name */}}
{{- define "slurm-cluster-storage.volume.jail.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail.name" .) "mount" | include "mashedkebab" | quote -}}
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
        {{- required "Jail volume filestore device name is required." .Values.volume.jail.filestoreDeviceName | trim | include "mashedkebab" -}}
    {{- else }}
        {{- "" -}}
    {{- end }}
{{- end }}

{{/* Jail GlusterFS host name */}}
{{- define "slurm-cluster-storage.volume.jail.hostname" -}}
    {{- if eq .Values.volume.jail.type "glusterfs" -}}
        {{- required "Jail volume GlusterFS hostname is required." .Values.volume.jail.glusterfsHostName | trim | include "mashedkebab" -}}
    {{- else }}
        {{- "" -}}
    {{- end }}
{{- end }}

{{/*
---
*/}}

{{/* Controller spool volume */}}
{{- define "slurm-cluster-storage.volume.controller-spool.name" -}}
    {{- required "Controller spool volume name is required." .Values.volume.controllerSpool.name | trim | include "mashedkebab" -}}
{{- end }}

{{/* Controller spool PVC name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.pvc" -}}
    {{- cat (include "slurm-cluster-storage.volume.controller-spool.name" .) "pvc" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Controller spool PV name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.controller-spool.name" .) "pv" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Controller spool mount name */}}
{{- define "slurm-cluster-storage.volume.controller-spool.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.controller-spool.name" .) "mount" | include "mashedkebab" | quote -}}
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
    {{- required "Controller spool Filestore device name is required." .Values.volume.controllerSpool.filestoreDeviceName | trim | include "mashedkebab" -}}
{{- end }}

{{/*
---
*/}}

{{/* Accounting database volume */}}
{{- define "slurm-cluster-storage.volume.accounting.name" -}}
    {{- required "Accounting volume name is required." .Values.volume.accounting.name | trim | include "mashedkebab" -}}
{{- end }}

{{/* Accounting database  PV name */}}
{{- define "slurm-cluster-storage.volume.accounting.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.accounting.name" .) "pv" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Accounting database  mount name */}}
{{- define "slurm-cluster-storage.volume.accounting.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.accounting.name" .) "mount" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Accounting database  storage class name */}}
{{- define "slurm-cluster-storage.volume.accounting.storageClass" -}}
    {{- include "slurm-cluster-storage.class.local.name" . -}}
{{- end }}

{{/* Accounting database  size */}}
{{- define "slurm-cluster-storage.volume.accounting.size" -}}
    {{- required "Accounting volume size is required." .Values.volume.accounting.size -}}
{{- end }}

{{/* Accounting database  device name */}}
{{- define "slurm-cluster-storage.volume.accounting.device" -}}
    {{- required "Accounting Filestore device name is required." .Values.volume.accounting.filestoreDeviceName | trim | include "mashedkebab" -}}
{{- end }}

{{/*
---
*/}}

{{/* Jail submount volume */}}
{{- define "slurm-cluster-storage.volume.jail-submount.name" -}}
    {{- cat "jail-submount" (required "Jail submount name is required." .name) | trim | include "mashedkebab" -}}
{{- end }}

{{/* Jail submount PVC name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.pvc" -}}
  {{- cat (include "slurm-cluster-storage.volume.jail-submount.name" .) "pvc" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Jail submount PV name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.pv" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail-submount.name" .) "pv" | include "mashedkebab" | quote -}}
{{- end }}

{{/* Jail submount mount name */}}
{{- define "slurm-cluster-storage.volume.jail-submount.mount" -}}
    {{- cat (include "slurm-cluster-storage.volume.jail-submount.name" .) "mount" | include "mashedkebab" | quote -}}
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
    {{- required "Jail submount Filestore device name is required." .filestoreDeviceName | trim | include "mashedkebab" -}}
{{- end }}
