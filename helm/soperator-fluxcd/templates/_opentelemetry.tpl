{{/* Helm coalesces nulls in umbrella values before the child chart can use them to disable defaults. */}}
{{- define "soperator-fluxcd.otelCollectorValues" -}}
{{- $values := .values | default dict -}}
{{- if $values.config -}}
  {{- $defaults := .root.Files.Get "files/opentelemetry-disabled-defaults.yaml" | fromYaml -}}
  {{- $values = mustMergeOverwrite (index $defaults .collector) $values -}}
{{- end -}}
{{- toYaml $values -}}
{{- end -}}
