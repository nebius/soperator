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
