{{/*
Public endpoint enabled?
*/}}
{{- define "o11y.publicEndpoint.enabled" -}}
{{- and .Values.observability.publicEndpoint (eq (toString .Values.observability.publicEndpoint.enabled) "true") -}}
{{- end -}}

{{/*
Return authenticator name (validated).
*/}}
{{- define "o11y.publicEndpoint.authenticator" -}}
{{- $pe := .Values.observability.publicEndpoint -}}
{{- if not $pe -}}
{{- fail "Values.observability.publicEndpoint is required when referenced" -}}
{{- end -}}
{{- $a := default "" $pe.authenticator -}}
{{- if or (eq $a "nebiusiamauth") (eq $a "bearertokenauth") -}}
{{- $a -}}
{{- else -}}
{{- fail (printf "Unknown observability.publicEndpoint.authenticator: %s (allowed: nebiusiamauth, bearertokenauth)" $a) -}}
{{- end -}}
{{- end -}}

{{/*
Auth config snippet for collector config.
*/}}
{{- define "o11y.publicEndpoint.authConfig" -}}
{{- if include "o11y.publicEndpoint.enabled" . -}}
{{- $pe := .Values.observability.publicEndpoint -}}
{{- $a := include "o11y.publicEndpoint.authenticator" . -}}

{{- if eq $a "nebiusiamauth" -}}
nebiusiamauth:
  auth_scheme: {{ required "observability.publicEndpoint.nebiusiamauth.auth_scheme is required" $pe.nebiusiamauth.auth_scheme | quote }}
  iam_endpoint: {{ required "observability.publicEndpoint.nebiusiamauth.iam_endpoint is required" $pe.nebiusiamauth.iam_endpoint | quote }}
  private_key_dir: {{ required "observability.publicEndpoint.nebiusiamauth.private_key_dir is required" $pe.nebiusiamauth.private_key_dir | quote }}
  private_key_file_name: {{ required "observability.publicEndpoint.nebiusiamauth.private_key_file_name is required" $pe.nebiusiamauth.private_key_file_name | quote }}
  public_key_id: {{ required "observability.publicEndpoint.nebiusiamauth.public_key_id is required" $pe.nebiusiamauth.public_key_id | quote }}
  service_account_id: {{ required "observability.publicEndpoint.nebiusiamauth.service_account_id is required" $pe.nebiusiamauth.service_account_id | quote }}

{{- else if eq $a "bearertokenauth" -}}
bearertokenauth:
  filename: {{ required "observability.publicEndpoint.bearertokenauth.tokenFile is required" $pe.bearertokenauth.tokenFile | quote }}

{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Service extensions list item (one of).
Usage:
  extensions:
    - health_check
    {{- include "o11y.publicEndpoint.extension" . | nindent 4 }}
*/}}
{{- define "o11y.publicEndpoint.extension" -}}
{{- if include "o11y.publicEndpoint.enabled" . -}}
- {{ include "o11y.publicEndpoint.authenticator" . }}
{{- end -}}
{{- end -}}

{{/*
VolumeMount (one of).
Usage:
  volumeMounts:
    {{- include "o11y.publicEndpoint.volumeMount" . | nindent 4 }}
*/}}
{{- define "o11y.publicEndpoint.volumeMount" -}}
{{- if include "o11y.publicEndpoint.enabled" . -}}
{{- $pe := .Values.observability.publicEndpoint -}}
{{- $a := include "o11y.publicEndpoint.authenticator" . -}}

{{- if eq $a "nebiusiamauth" -}}
- name: {{ required "observability.publicEndpoint.nebiusiamauth.secret.name is required" $pe.nebiusiamauth.secret.name }}
  mountPath: {{ required "observability.publicEndpoint.nebiusiamauth.secret.mountPath is required" $pe.nebiusiamauth.secret.mountPath }}
  readOnly: true

{{- else if eq $a "bearertokenauth" -}}
- name: {{ required "observability.publicEndpoint.bearertokenauth.secret.name is required" $pe.bearertokenauth.secret.name }}
  mountPath: {{ required "observability.publicEndpoint.bearertokenauth.secret.mountPath is required" $pe.bearertokenauth.secret.mountPath }}
  readOnly: true
{{- end -}}

{{- end -}}
{{- end -}}

{{/*
Volumes (one of). Делает secret volume с optional items, если указан key.
Usage:
  volumes:
    {{- include "o11y.publicEndpoint.volume" . | nindent 4 }}
*/}}
{{- define "o11y.publicEndpoint.volume" -}}
{{- if include "o11y.publicEndpoint.enabled" . -}}
{{- $pe := .Values.observability.publicEndpoint -}}
{{- $a := include "o11y.publicEndpoint.authenticator" . -}}

{{- if eq $a "nebiusiamauth" -}}
- name: {{ required "observability.publicEndpoint.nebiusiamauth.secret.name is required" $pe.nebiusiamauth.secret.name }}
  secret:
    secretName: {{ required "observability.publicEndpoint.nebiusiamauth.secret.name is required" $pe.nebiusiamauth.secret.name }}
    {{- with $pe.nebiusiamauth.secret.key }}
    items:
      - key: {{ . }}
        path: {{ . }}
    {{- end }}

{{- else if eq $a "bearertokenauth" -}}
- name: {{ required "observability.publicEndpoint.bearertokenauth.secret.name is required" $pe.bearertokenauth.secret.name }}
  secret:
    secretName: {{ required "observability.publicEndpoint.bearertokenauth.secret.name is required" $pe.bearertokenauth.secret.name }}
{{- end -}}

{{- end -}}
{{- end -}}
