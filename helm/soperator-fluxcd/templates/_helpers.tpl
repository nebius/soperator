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
{{- .Values.observability.vmStack.namespace | default "monitoring-system" -}}
{{- else -}}
{{- .Values.observability.opentelemetry.namespace | default "logs-system" -}}
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
{{- $namespaces = append $namespaces (.Values.observability.vmStack.namespace | default "monitoring-system") -}}
{{- end -}}
{{- if .Values.observability.opentelemetry.enabled -}}
{{- $namespaces = append $namespaces (.Values.observability.opentelemetry.namespace | default "logs-system") -}}
{{- end -}}
{{- $namespaces | uniq | toYaml -}}
{{- end -}}
{{- end -}}

{{/*
Convert a Kubernetes CPU quantity to a positive GOMAXPROCS value.
*/}}
{{- define "soperator-fluxcd.cpuQuantityToGOMAXPROCS" -}}
{{- $quantity := toString . | trim -}}
{{- if eq $quantity "" -}}
1
{{- else if regexMatch "^[0-9]+(\\.[0-9]+)?m$" $quantity -}}
{{- max 1 (int (ceil (divf (trimSuffix "m" $quantity | float64) 1000.0))) -}}
{{- else if regexMatch "^[0-9]+(\\.[0-9]+)?$" $quantity -}}
{{- max 1 (int (ceil ($quantity | float64))) -}}
{{- else -}}
{{- fail (printf "unsupported CPU quantity %q; use cores (2) or millicores (500m)" $quantity) -}}
{{- end -}}
{{- end -}}

{{/*
Render GOMAXPROCS for opentelemetry-collector from container resources.
*/}}
{{- define "soperator-fluxcd.otelCollectorGoMaxProcsEnv" -}}
{{- $resources := .resources | default dict -}}
{{- $requests := get $resources "requests" | default dict -}}
{{- $limits := get $resources "limits" | default dict -}}
{{- $cpu := coalesce (get $limits "cpu") (get $requests "cpu") -}}
- name: GOMAXPROCS
  value: {{ include "soperator-fluxcd.cpuQuantityToGOMAXPROCS" $cpu | quote }}
{{- end -}}

{{/*
Render exporter sending_queue settings with exporter-side batching.
*/}}
{{- define "soperator-fluxcd.otelExporterSendingQueue" -}}
{{- $batch := .batch | default dict -}}
{{- $queue := .queue | default dict -}}
{{- $timeout := get $batch "timeout" | default "1s" -}}
{{- $minSize := get $batch "sendBatchSize" | default 2000 | int -}}
{{- $maxSize := get $batch "sendBatchMaxSize" | default 5000 | int -}}
{{- $queueSize := get $queue "queueSize" | default 30000 | int -}}
{{- $numConsumers := get $queue "numConsumers" | default 10 | int -}}
sending_queue:
  enabled: true
  sizer: items
  queue_size: {{ max $queueSize $minSize }}
  num_consumers: {{ $numConsumers }}
  {{- if .storage }}
  storage: {{ .storage }}
  {{- end }}
  batch:
    sizer: items
    flush_timeout: {{ $timeout }}
    min_size: {{ $minSize }}
    max_size: {{ $maxSize }}
{{- end -}}
