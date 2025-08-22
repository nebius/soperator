{{/*
Expand the name of the chart.
*/}}
{{- define "nfs-server.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "nfs-server.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "nfs-server.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "nfs-server.labels" -}}
helm.sh/chart: {{ include "nfs-server.chart" . }}
{{ include "nfs-server.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "nfs-server.selectorLabels" -}}
app.kubernetes.io/name: {{ include "nfs-server.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "nfs-server.serviceAccountName" -}}
{{- if .Values.serviceAccount.enabled }}
{{- default (include "nfs-server.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the priority class to use
*/}}
{{- define "nfs-server.priorityClassName" -}}
{{- if .Values.priorityClass.enabled }}
{{- default (printf "%s-priority" (include "nfs-server.fullname" .)) .Values.priorityClass.name }}
{{- else }}
{{- .Values.priorityClass.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the storage class to use
*/}}
{{- define "nfs-server.storageClassName" -}}
{{- if .Values.storageClass.enabled }}
{{- default (printf "%s-nfs" (include "nfs-server.fullname" .)) .Values.storageClass.name }}
{{- else }}
{{- .Values.storageClass.name }}
{{- end }}
{{- end }}

{{/*
Create the image name
*/}}
{{- define "nfs-server.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- printf "%s:%s" .Values.image.repository $tag }}
{{- end }}

{{/*
Create NFS server address for storage class
*/}}
{{- define "nfs-server.nfsServer" -}}
{{- printf "%s.%s.svc.cluster.local" (include "nfs-server.fullname" .) .Release.Namespace }}
{{- end }}
