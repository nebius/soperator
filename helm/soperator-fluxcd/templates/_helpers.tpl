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
Convert a Kubernetes memory quantity to bytes.
*/}}
{{- define "soperator-fluxcd.memoryQuantityToBytes" -}}
{{- $quantity := toString . | trim -}}
{{- if not (regexMatch "^[0-9]+(\\.[0-9]+)?(Ki|Mi|Gi|Ti|Pi|Ei|[kKMGTP])?$" $quantity) -}}
{{- fail (printf "unsupported memory quantity %q; use bytes, decimal units (500G), or binary units (500Gi)" $quantity) -}}
{{- end -}}
{{- $number := regexFind "^[0-9]+(\\.[0-9]+)?" $quantity -}}
{{- $suffix := regexReplaceAll "^[0-9]+(\\.[0-9]+)?" $quantity "" -}}
{{- $multiplier := 1 -}}
{{- if eq $suffix "Ki" -}}
{{- $multiplier = 1024 -}}
{{- else if eq $suffix "Mi" -}}
{{- $multiplier = 1048576 -}}
{{- else if eq $suffix "Gi" -}}
{{- $multiplier = 1073741824 -}}
{{- else if eq $suffix "Ti" -}}
{{- $multiplier = 1099511627776 -}}
{{- else if or (eq $suffix "k") (eq $suffix "K") -}}
{{- $multiplier = 1000 -}}
{{- else if eq $suffix "M" -}}
{{- $multiplier = 1000000 -}}
{{- else if eq $suffix "G" -}}
{{- $multiplier = 1000000000 -}}
{{- else if eq $suffix "T" -}}
{{- $multiplier = 1000000000000 -}}
{{- end -}}
{{- if regexMatch "^[0-9]+$" $number -}}
{{- mul ($number | int) $multiplier -}}
{{- else -}}
{{- int (mulf ($number | float64) ($multiplier | float64)) -}}
{{- end -}}
{{- end -}}

{{/*
Return N percent of a Kubernetes memory quantity as a GOMEMLIMIT value, rounded up to MiB.
*/}}
{{- define "soperator-fluxcd.memoryQuantityPercentGoMemLimit" -}}
{{- $bytes := include "soperator-fluxcd.memoryQuantityToBytes" .quantity | int -}}
{{- $percent := .percent | default 85 | int -}}
{{- $mib := 1048576 -}}
{{- $divisor := mul 100 $mib -}}
{{- $whole := mul (div $bytes $divisor) $percent -}}
{{- $remainder := mul (mod $bytes $divisor) $percent -}}
{{- $roundedRemainder := div (add $remainder (sub $divisor 1)) $divisor -}}
{{- $limitMiB := max 1 (add $whole $roundedRemainder) -}}
{{- if eq (mod $limitMiB 1024) 0 -}}
{{- printf "%dGiB" (div $limitMiB 1024) -}}
{{- else -}}
{{- printf "%dMiB" $limitMiB -}}
{{- end -}}
{{- end -}}

{{/*
Render Go runtime env vars for opentelemetry-collector from container resources.
*/}}
{{- define "soperator-fluxcd.otelCollectorGoEnv" -}}
{{- $resources := .resources | default dict -}}
{{- $requests := get $resources "requests" | default dict -}}
{{- $limits := get $resources "limits" | default dict -}}
{{- $cpu := coalesce (get $limits "cpu") (get $requests "cpu") -}}
- name: GOMAXPROCS
  value: {{ include "soperator-fluxcd.cpuQuantityToGOMAXPROCS" $cpu | quote }}
{{- if .useGoMemLimit }}
{{- $memory := coalesce (get $limits "memory") (get $requests "memory") -}}
{{- if not $memory -}}
{{- fail "GOMEMLIMIT is enabled but no resources.limits.memory or resources.requests.memory is configured" -}}
{{- end }}
- name: GOMEMLIMIT
  value: {{ include "soperator-fluxcd.memoryQuantityPercentGoMemLimit" (dict "quantity" $memory "percent" 85) | quote }}
{{- end -}}
{{- end -}}
