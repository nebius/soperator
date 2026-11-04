{{- define "soperator-monitoring-dashboards.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "soperator-monitoring-dashboards.fullname" -}}
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

{{- define "soperator-monitoring-dashboards.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
app.kubernetes.io/name: {{ include "soperator-monitoring-dashboards.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
grafana_dashboard: "1"
{{- end }}

{{- define "soperator-monitoring-dashboards.namePrefix" -}}
{{- default .Release.Name .Values.namePrefix -}}
{{- end }}

{{- define "soperator-monitoring-dashboards.dashboardName" -}}
{{- $base := base . -}}
{{- $id := trimSuffix ".json" $base -}}
{{- replace "_" "-" $id -}}
{{- end }}

{{- define "soperator-monitoring-dashboards.cmName" -}}
{{- printf "%s-%s" .namePrefix .dashboardName | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/* Require every panel query to filter on the portable cluster variable. */}}
{{- define "soperator-monitoring-dashboards.validateClusterFilteredExpressions" -}}
{{- $path := .path -}}
{{- $value := .value -}}
{{- if kindIs "map" $value -}}
  {{- range $key, $nested := $value -}}
    {{- $nestedPath := printf "%s.%s" $path $key -}}
    {{- if eq $key "expr" -}}
      {{- if not (contains "cluster=~\"$cluster\"" (toString $nested)) -}}
        {{- fail (printf "%s: %s must filter metrics with cluster=~\"$cluster\"" $path $nestedPath) -}}
      {{- end -}}
    {{- else -}}
      {{- include "soperator-monitoring-dashboards.validateClusterFilteredExpressions" (dict "path" $nestedPath "value" $nested) -}}
    {{- end -}}
  {{- end -}}
{{- else if kindIs "slice" $value -}}
  {{- range $index, $nested := $value -}}
    {{- include "soperator-monitoring-dashboards.validateClusterFilteredExpressions" (dict "path" (printf "%s[%d]" $path $index) "value" $nested) -}}
  {{- end -}}
{{- end -}}
{{- end }}

{{/* Fail chart rendering when a dashboard violates the basic O11y portability contract. */}}
{{- define "soperator-monitoring-dashboards.validateDashboardPortability" -}}
{{- $path := .path -}}
{{- $dashboard := .dashboard -}}
{{- $variables := dig "templating" "list" (list) $dashboard -}}
{{- if lt (len $variables) 3 -}}
  {{- fail (printf "%s: expected $datasource, $o11y, and $cluster variables" $path) -}}
{{- end -}}
{{- $datasource := index $variables 0 -}}
{{- $o11y := index $variables 1 -}}
{{- $cluster := index $variables 2 -}}

{{- if ne (dig "name" "" $datasource) "datasource" -}}
  {{- fail (printf "%s: first variable must be $datasource" $path) -}}
{{- end -}}
{{- if or (ne (dig "type" "" $datasource) "datasource") (ne (toString (dig "hide" 0 $datasource)) "2") -}}
  {{- fail (printf "%s: $datasource must be a hidden datasource variable" $path) -}}
{{- end -}}
{{- if ne (dig "query" "" $datasource) "prometheus" -}}
  {{- fail (printf "%s: $datasource must select prometheus datasources" $path) -}}
{{- end -}}

{{- if ne (dig "name" "" $o11y) "o11y" -}}
  {{- fail (printf "%s: second variable must be $o11y" $path) -}}
{{- end -}}
{{- if or (ne (dig "type" "" $o11y) "adhoc") (ne (toString (dig "hide" 0 $o11y)) "2") -}}
  {{- fail (printf "%s: $o11y must be a hidden adhoc variable" $path) -}}
{{- end -}}
{{- if or (ne (len (dig "filters" (list) $o11y)) 0) (ne (len (dig "baseFilters" (list) $o11y)) 0) -}}
  {{- fail (printf "%s: $o11y must have no in-cluster filters" $path) -}}
{{- end -}}
{{- if ne (dig "datasource" "uid" "" $o11y) "${datasource}" -}}
  {{- fail (printf "%s: $o11y must use ${datasource}" $path) -}}
{{- end -}}

{{- if ne (dig "name" "" $cluster) "cluster" -}}
  {{- fail (printf "%s: third variable must be $cluster" $path) -}}
{{- end -}}
{{- if or (ne (dig "type" "" $cluster) "query") (ne (toString (dig "hide" 0 $cluster)) "2") -}}
  {{- fail (printf "%s: $cluster must be a hidden query variable" $path) -}}
{{- end -}}
{{- if or (not (dig "includeAll" false $cluster)) (ne (dig "allValue" "" $cluster) ".*") -}}
  {{- fail (printf "%s: $cluster must include All with the value .*" $path) -}}
{{- end -}}
{{- if ne (dig "current" "value" "" $cluster) "$__all" -}}
  {{- fail (printf "%s: $cluster must default to All" $path) -}}
{{- end -}}
{{- if ne (dig "datasource" "uid" "" $cluster) "${datasource}" -}}
  {{- fail (printf "%s: $cluster must use ${datasource}" $path) -}}
{{- end -}}

{{- range $variable := $variables -}}
  {{- if and (eq (dig "type" "" $variable) "query") (ne (dig "name" "" $variable) "cluster") -}}
    {{- $name := dig "name" "" $variable -}}
    {{- if not (contains "cluster=~\"$cluster\"" (dig "definition" "" $variable)) -}}
      {{- fail (printf "%s: $%s definition must filter metrics with cluster=~\"$cluster\"" $path $name) -}}
    {{- end -}}
    {{- if not (contains "cluster=~\"$cluster\"" (dig "query" "query" "" $variable)) -}}
      {{- fail (printf "%s: $%s query must filter metrics with cluster=~\"$cluster\"" $path $name) -}}
    {{- end -}}
  {{- end -}}
{{- end -}}
{{- include "soperator-monitoring-dashboards.validateClusterFilteredExpressions" (dict "path" $path "value" $dashboard) -}}
{{- end -}}
