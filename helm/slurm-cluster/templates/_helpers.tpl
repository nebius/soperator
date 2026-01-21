{{/* Name of the cluster */}}
{{- define "slurm-cluster.name" -}}
    {{- default .Chart.Name .Values.clusterName | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Create chart name and version as used by the chart label */}}
{{- define "slurm-cluster.chart" -}}
    {{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Selector labels */}}
{{- define "slurm-cluster.selectorLabels" -}}
    {{- println (cat "app.kubernetes.io/name:" (include "slurm-cluster.name" .)) }}
    {{- println (cat "app.kubernetes.io/instance:" .Release.Name) }}
{{- end }}

{{/* Common labels */}}
{{- define "slurm-cluster.labels" -}}
    {{- println (cat "helm.sh/chart:" (include "slurm-cluster.chart" .)) }}
    {{- println (cat "app.kubernetes.io/managed-by:" .Release.Service) }}
    {{- if .Chart.AppVersion }}
        {{- cat "app.kubernetes.io/version:" .Chart.AppVersion }}
    {{- end }}
    {{- include "slurm-cluster.selectorLabels" . | trim | nindent 0 }}
{{- end }}

{{- define "validateAccountingConfig" -}}
{{- if .Values.slurmNodes.accounting.enabled -}}
  {{- if not (or .Values.slurmNodes.accounting.externalDB.enabled .Values.slurmNodes.accounting.mariadbOperator.enabled) -}}
    {{- fail "If slurmNodes.accounting.enabled is true, either slurmNodes.accounting.externalDB.enabled or slurmNodes.accounting.mariadbOperator.enabled must be true." -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create the name of the service account to use for sconfigcontroller
*/}}
{{- define "slurm-cluster.sconfigcontroller.serviceAccountName" -}}
{{- if .Values.sConfigController.serviceAccount.create -}}
    {{- default (printf "%s-sconfigcontroller" (include "slurm-cluster.name" .)) .Values.sConfigController.serviceAccount.name }}
{{- else -}}
    {{- default "default" .Values.sConfigController.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Create the name of the role for sconfigcontroller
*/}}
{{- define "slurm-cluster.sconfigcontroller.roleName" -}}
{{- printf "%s-sconfigcontroller" (include "slurm-cluster.name" .) }}
{{- end -}}

{{/*
Create the name of the role binding for sconfigcontroller
*/}}
{{- define "slurm-cluster.sconfigcontroller.roleBindingName" -}}
{{- printf "%s-sconfigcontroller" (include "slurm-cluster.name" .) }}
{{- end -}}

{{/*
Create the name of the service account to use for exporter
*/}}
{{- define "slurm-cluster.exporter.serviceAccountName" -}}
{{- if .Values.slurmNodes.exporter.serviceAccount.create -}}
    {{- default "slurm-exporter-sa" .Values.slurmNodes.exporter.serviceAccount.name }}
{{- else -}}
    {{- default "default" .Values.slurmNodes.exporter.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Create the name of the role for exporter
*/}}
{{- define "slurm-cluster.exporter.roleName" -}}
{{- printf "%s-exporter-role" (include "slurm-cluster.name" .) }}
{{- end -}}

{{/*
Create the name of the role binding for exporter
*/}}
{{- define "slurm-cluster.exporter.roleBindingName" -}}
{{- printf "%s-exporter-role-binding" (include "slurm-cluster.name" .) }}
{{- end -}}
