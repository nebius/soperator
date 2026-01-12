{{/*
Expand the name of the chart.
*/}}
{{- define "soperator-activechecks.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "soperator-activechecks.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}
{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soperator-activechecks.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "soperator-activechecks.labels" -}}
helm.sh/chart: {{ include "soperator-activechecks.chart" . }}
{{ include "soperator-activechecks.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "soperator-activechecks.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soperator-activechecks.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "soperator-activechecks.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "soperator-activechecks.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}


{{/*
Pyxis format for active check image.
*/}}
{{- define "activecheck.image.pyxis" -}}
{{- .Values.activeCheckImage -}}
{{- end -}}


{{/*
Docker format for active check image.
Converts from format "reg#repo:tag" to format "reg/repo:tag".
*/}}
{{- define "activecheck.image.docker" -}}
{{- .Values.activeCheckImage | replace "#" "/" -}}
{{- end -}}
 
{{/*
Validate that a check does not set both commentPrefix and drainReasonPrefix
in values under a single check. This helper should be invoked with a dict
containing keys: "comment", "drain", "name".
*/}}
{{- define "soperator-activechecks.checkReactionsConflict" -}}
{{- $c := . -}}
{{- if and $c.comment $c.drain -}}
{{- fail (printf "%s: cannot set both commentPrefix and drainReasonPrefix simultaneously" $c.name) -}}
{{- end -}}
{{- end -}}
