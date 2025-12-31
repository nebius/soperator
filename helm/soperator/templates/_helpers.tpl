{{/*
Expand the name of the chart.
*/}}
{{- define "soperator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "soperator.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soperator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "soperator.labels" -}}
helm.sh/chart: {{ include "soperator.chart" . }}
{{ include "soperator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "soperator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soperator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "soperator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "soperator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "soperator.controllersAvailable" -}}
cluster,nodeconfigurator,nodeset,topology
{{- end }}

{{- define "soperator.controllersSpec" -}}
{{- $controllers := .Values.controllerManager.manager.controllersEnabled -}}
{{- $available := include "soperator.controllersAvailable" . | trim | splitList "," -}}
{{- if $controllers -}}
{{- /* validate */}}
{{- range $name, $_ := $controllers -}}
{{- if not (has $name $available) -}}
{{- fail (printf "unknown controller %q in controllerManager.manager.controllersEnabled, available controllers: %q" $name $available) -}}
{{- end -}}
{{- end -}}
{{- /* generate comma spearated list */}}
{{- $spec := list -}}
{{- range $available -}}
{{- if hasKey $controllers . -}}
{{- if (get $controllers .) -}}
{{- $spec = append $spec . -}}
{{- else -}}
{{- $spec = append $spec (printf "-%s" .) -}}
{{- end -}}
{{- else -}}
{{- $spec = append $spec . -}}
{{- end -}}
{{- end -}}
{{- join "," $spec -}}
{{- end -}}
{{- end }}
