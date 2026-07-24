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

{{/* Name of the slurm-scripts ConfigMap */}}
{{- define "slurm-cluster.slurmScriptsCMName" -}}
{{- printf "%s-slurm-scripts" (include "slurm-cluster.name" .) -}}
{{- end -}}

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
    {{- default (printf "%s-exporter-sa" (include "slurm-cluster.name" .)) .Values.slurmNodes.exporter.serviceAccount.name }}
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

{{/*
Create the name of the service account to use for slurm-controller
*/}}
{{- define "slurm-cluster.controller.serviceAccountName" -}}
{{- if .Values.slurmNodes.controller.serviceAccount.create -}}
    {{- default (printf "%s-slurm-controller" (include "slurm-cluster.name" .)) .Values.slurmNodes.controller.serviceAccount.name }}
{{- else -}}
    {{- default "default" .Values.slurmNodes.controller.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
Create the name of the role for slurm-controller
*/}}
{{- define "slurm-cluster.controller.roleName" -}}
{{- printf "%s-slurm-controller" (include "slurm-cluster.name" .) }}
{{- end -}}

{{/*
Create the name of the role binding for slurm-controller
*/}}
{{- define "slurm-cluster.controller.roleBindingName" -}}
{{- printf "%s-slurm-controller" (include "slurm-cluster.name" .) }}
{{- end -}}

{{/*
Render node-exporter as a native sidecar init container.
Usage: include "slurm-cluster.nodeExporterInitContainer" (dict "root" $ "nodeExporter" .Values.path.to.nodeExporter)
*/}}
{{- define "slurm-cluster.nodeExporterInitContainer" -}}
{{- $root := .root -}}
{{- $nodeExporter := .nodeExporter | default dict -}}
{{- if $nodeExporter.enabled }}
- name: pod-node-exporter
  image: {{ default $root.Values.images.nodeExporter $nodeExporter.image | quote }}
  imagePullPolicy: {{ default "IfNotPresent" $nodeExporter.imagePullPolicy | quote }}
  restartPolicy: Always
  args:
    {{- range (default (list "--collector.disable-defaults" "--collector.netdev" "--collector.netstat" "--collector.sockstat" "--collector.conntrack") $nodeExporter.args) }}
    - {{ . | quote }}
    {{- end }}
    - {{ printf "--web.listen-address=:%v" (default 9100 $nodeExporter.port) | quote }}
  ports:
    - name: node-exporter
      containerPort: {{ default 9100 $nodeExporter.port }}
      protocol: TCP
  resources:
    requests:
      cpu: {{ default "50m" (get ($nodeExporter.resources | default dict) "cpu") | quote }}
      memory: {{ default "64Mi" (get ($nodeExporter.resources | default dict) "memory") | quote }}
      ephemeral-storage: {{ default "128Mi" (coalesce (get ($nodeExporter.resources | default dict) "ephemeralStorage") (get ($nodeExporter.resources | default dict) "ephemeral-storage")) | quote }}
    limits:
      memory: {{ default "64Mi" (get ($nodeExporter.resources | default dict) "memory") | quote }}
      ephemeral-storage: {{ default "128Mi" (coalesce (get ($nodeExporter.resources | default dict) "ephemeralStorage") (get ($nodeExporter.resources | default dict) "ephemeral-storage")) | quote }}
  securityContext:
    allowPrivilegeEscalation: false
    capabilities:
      drop:
        - ALL
  {{- with $nodeExporter.livenessProbe }}
  livenessProbe:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  {{- with $nodeExporter.readinessProbe }}
  readinessProbe:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
{{- end -}}

{{/*
Render Slurm customInitContainers with optional node-exporter sidecar appended.
Usage: include "slurm-cluster.customInitContainers" (dict "root" $ "customInitContainers" .Values.path.to.customInitContainers "nodeExporter" .Values.path.to.nodeExporter)
*/}}
{{- define "slurm-cluster.customInitContainers" -}}
{{- $customInitContainers := default list .customInitContainers -}}
{{- $nodeExporter := .nodeExporter | default dict -}}
{{- if and (not $customInitContainers) (not $nodeExporter.enabled) }}
customInitContainers: []
{{- else }}
customInitContainers:
{{- if $customInitContainers }}
{{- toYaml $customInitContainers | nindent 2 }}
{{- end }}
{{- include "slurm-cluster.nodeExporterInitContainer" . | nindent 2 }}
{{- end }}
{{- end -}}
