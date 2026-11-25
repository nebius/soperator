{{/*
Expand the name of the chart.
*/}}
{{- define "soperator-fluxcd.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "soperator-fluxcd.fullname" -}}
{{- default .Release.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "soperator-fluxcd.targetNamespace" -}}
{{- with . -}}
targetNamespace: {{ . | quote }}
{{- end -}}
{{- end -}}

{{/*
Resolve dependency names in the chart context, preserving the rest of each reference.
*/}}
{{- define "soperator-fluxcd.dependsOn" -}}
{{- if hasKey .component "dependsOn" -}}
  {{- $dependencies := .component.dependsOn -}}
  {{- if and (ne $dependencies nil) (not (kindIs "slice" $dependencies)) -}}
    {{- fail "dependsOn must be a list or null" -}}
  {{- end -}}
  {{- $references := list -}}
  {{- range $dependencies -}}
    {{- $name := tpl .name $.root | trim -}}
    {{- if $name -}}
      {{- $reference := deepCopy . -}}
      {{- $_ := set $reference "name" $name -}}
      {{- $references = append $references $reference -}}
    {{- end -}}
  {{- end -}}
  {{- toYaml (dict "dependsOn" $references) -}}
{{- end -}}
{{- end -}}

{{/*
Apply the platform defaults before the child chart values. Existing legacy maps
replace the whole inline configuration.
*/}}
{{- define "soperator-fluxcd.platformValues" -}}
{{- with .component.overrideValues -}}
  {{- toYaml . -}}
{{- else -}}
  {{- $defaults := deepCopy (.component.defaultValues | default dict) -}}
  {{- if .prometheusEnabled -}}
    {{- $defaults = mustMergeOverwrite $defaults (.component.prometheusValues | default dict) -}}
  {{- end -}}
  {{- $resolved := tpl (toYaml $defaults) .root | fromYaml -}}
  {{- mustMergeOverwrite $resolved (.component.values | default dict) | toYaml -}}
{{- end -}}
{{- end -}}

{{/*
Merge into a copy of the Kruise values using Helm's null handling for old overrides.
*/}}
{{- define "soperator-fluxcd.mergeKruiseOverrides" -}}
{{- range $key, $override := .overrides -}}
  {{- $value := get $.values $key -}}
  {{- if and (eq $override nil) (hasKey $.values $key) -}}
    {{- $_ := unset $.values $key -}}
  {{- else if and (kindIs "map" $value) (kindIs "map" $override) -}}
    {{- include "soperator-fluxcd.mergeKruiseOverrides" (dict "values" $value "overrides" $override) -}}
  {{- else -}}
    {{- $_ := set $.values $key $override -}}
  {{- end -}}
{{- end -}}
{{- end -}}

{{/*
Keep existing overrides effective until their callers can switch to values.
*/}}
{{- define "soperator-fluxcd.valuesWithLegacyOverride" -}}
{{- if ne .values nil -}}
  {{- toYaml .values -}}
{{- else if .overrideValues -}}
  {{- toYaml .overrideValues -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "soperator-fluxcd.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "soperator-fluxcd.labels" -}}
helm.sh/chart: {{ include "soperator-fluxcd.chart" . }}
{{ include "soperator-fluxcd.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "soperator-fluxcd.selectorLabels" -}}
app.kubernetes.io/name: {{ include "soperator-fluxcd.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "soperator-fluxcd.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "soperator-fluxcd.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Validate observability.publicEndpointTokenKind is one of: secret, hostPath
*/}}
{{- define "soperator-fluxcd.validatePublicEndpointTokenKind" -}}
{{- $kind := .Values.observability.publicEndpointTokenKind | default "" -}}

{{- if not (or (eq $kind "secret") (eq $kind "hostPath")) -}}
  {{- fail (printf "observability.publicEndpointTokenKind must be one of: secret, hostPath (got %q)" $kind) -}}
{{- end -}}
{{- end -}}

{{/*
Resolve o11y TSA token settings. observability.vmStack.tsaToken is kept as a
legacy override path for existing installations.
*/}}
{{- define "soperator-fluxcd.o11yTsaToken" -}}
{{- $global := .Values.observability.tsaToken | default dict -}}
{{- $legacy := .Values.observability.vmStack.tsaToken | default dict -}}
{{- mergeOverwrite (deepCopy $global) $legacy | toYaml -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenSecretName" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $token.secretName | default "o11y-writer-sa-token" -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenSecretKey" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $token.secretKey | default "accessToken" -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenMountPath" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $token.mountPath | default "/o11ytoken" -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenHostPath" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $token.hostPath | default "/mnt/cloud-metadata" -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenHostPathFilename" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $token.hostPathFilename | default "tsa-token" -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenWriterName" -}}
{{- printf "%s-tsa-token-writer" (include "soperator-fluxcd.fullname" .) -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenWriterNamespace" -}}
{{- if .Values.observability.vmStack.enabled -}}
{{- .Values.observability.vmStack.namespace | default .Release.Namespace -}}
{{- else -}}
{{- .Values.observability.opentelemetry.namespace | default .Release.Namespace -}}
{{- end -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenWriterEnabled" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $writer := $token.writer | default dict -}}
{{- $enabled := true -}}
{{- if hasKey $writer "enabled" -}}
{{- $enabled = $writer.enabled -}}
{{- end -}}
{{- if $enabled -}}true{{- end -}}
{{- end -}}

{{- define "soperator-fluxcd.o11yTsaTokenWriterNamespaces" -}}
{{- $token := include "soperator-fluxcd.o11yTsaToken" . | fromYaml -}}
{{- $writer := $token.writer | default dict -}}
{{- $configured := $writer.namespaces | default list -}}
{{- if gt (len $configured) 0 -}}
{{- $configured | uniq | toYaml -}}
{{- else -}}
{{- $namespaces := list -}}
{{- if .Values.observability.vmStack.enabled -}}
{{- $namespaces = append $namespaces (.Values.observability.vmStack.namespace | default .Release.Namespace) -}}
{{- end -}}
{{- if .Values.observability.opentelemetry.enabled -}}
{{- $namespaces = append $namespaces (.Values.observability.opentelemetry.namespace | default .Release.Namespace) -}}
{{- end -}}
{{- $namespaces | uniq | toYaml -}}
{{- end -}}
{{- end -}}
