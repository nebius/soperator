{{/*
Expand the name of the chart.
*/}}
{{- define "soperatorchecks.name" -}}
{{- default .Release.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "soperatorchecks.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soperatorchecks.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "soperatorchecks.labels" -}}
helm.sh/chart: {{ include "soperatorchecks.chart" . }}
{{ include "soperatorchecks.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "soperatorchecks.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soperatorchecks.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "soperatorchecks.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "soperatorchecks.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{- define "soperatorchecks.controllersAvailable" -}}
slurmapiclients,slurmnodes,k8snodes,activecheck,activecheckjob,serviceaccount,activecheckprolog,podephemeralstoragecheck
{{- end }}

{{- define "soperatorchecks.controllersSpec" -}}
{{- $controllers := .Values.checks.manager.controllersEnabled -}}
{{- $available := include "soperatorchecks.controllersAvailable" . | trim | splitList "," -}}
{{- if $controllers -}}
{{- /* validate */}}
{{- range $name, $_ := $controllers -}}
{{- if not (has $name $available) -}}
{{- fail (printf "unknown controller %q in checks.manager.controllersEnabled, available controllers: %q" $name $available) -}}
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
