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
Render script content from a file with optional tpl evaluation.
*/}}
{{- define "soperator-activechecks.renderScript" -}}
{{- $content := .ctx.Files.Get .path -}}
{{- tpl $content .ctx -}}
{{- end -}}

{{/*
Render munge container with defaults that match the existing checks.
*/}}
{{- define "soperator-activechecks.renderMungeContainer" -}}
{{- $ctx := default .ctx .renderCtx -}}
{{- $raw := default dict .container -}}
{{- $container := fromYaml (tpl (toYaml $raw) $ctx) -}}
{{- $image := default $ctx.Values.images.munge $container.image -}}
{{- $appArmor := default "unconfined" $container.appArmorProfile -}}
appArmorProfile: {{ $appArmor }}
image: {{ tpl $image $ctx | quote }}
{{- end -}}

{{/*
Render slurmJobSpec for an ActiveCheck.
*/}}
{{- define "soperator-activechecks.slurmJobSpec" -}}
{{- $ctx := .ctx -}}
{{- $spec := default dict .check.slurmJobSpec -}}
{{- $jobContainerRaw := default dict $spec.jobContainer -}}
{{- $baseContainer := dict "appArmorProfile" "unconfined" "image" $ctx.Values.images.slurmJob "env" $ctx.Values.jobContainer.env "volumeMounts" $ctx.Values.jobContainer.volumeMounts "volumes" $ctx.Values.jobContainer.volumes -}}
{{- $jobContainer := mustMerge $baseContainer (omit $jobContainerRaw "extraEnv" "extraVolumeMounts" "extraVolumes") -}}
{{- $env := default (list) $jobContainer.env -}}
{{- with $jobContainerRaw.extraEnv }}{{- $env = concat $env . -}}{{- end }}
{{- $volumeMounts := default (list) $jobContainer.volumeMounts -}}
{{- with $jobContainerRaw.extraVolumeMounts }}{{- $volumeMounts = concat $volumeMounts . -}}{{- end }}
{{- $volumes := default (list) $jobContainer.volumes -}}
{{- with $jobContainerRaw.extraVolumes }}{{- $volumes = concat $volumes . -}}{{- end }}
sbatchScript: |
{{ include "soperator-activechecks.renderScript" (dict "path" $spec.sbatchScriptFile "ctx" $ctx) | indent 2 }}
{{- if hasKey $spec "eachWorkerJobs" }}
eachWorkerJobs: {{ $spec.eachWorkerJobs }}
{{- end }}
{{- with $spec.maxNumberOfJobs }}
maxNumberOfJobs: {{ . }}
{{- end }}
jobContainer:
  appArmorProfile: {{ $jobContainer.appArmorProfile }}
  image: {{ tpl $jobContainer.image $ctx | quote }}
{{- with $jobContainer.command }}
  command:
{{ toYaml . | indent 4 }}
{{- end }}
{{- with $jobContainer.args }}
  args:
{{ toYaml . | indent 4 }}
{{- end }}
{{- if $env }}
  env:
{{ toYaml $env | indent 4 }}
{{- end }}
{{- if $volumeMounts }}
  volumeMounts:
{{ toYaml $volumeMounts | indent 4 }}
{{- end }}
{{- if $volumes }}
  volumes:
{{ toYaml $volumes | indent 4 }}
{{- end }}
mungeContainer:
{{ include "soperator-activechecks.renderMungeContainer" (dict "ctx" $ctx "container" $spec.mungeContainer) | indent 2 }}
{{- end -}}

{{/*
Render k8sJobSpec for an ActiveCheck.
*/}}
{{- define "soperator-activechecks.k8sJobSpec" -}}
{{- $ctx := .ctx -}}
{{- $spec := default dict .check.k8sJobSpec -}}
{{- $jobContainerRaw := default dict $spec.jobContainer -}}
{{- $useCommonVolumeMounts := default true $spec.useCommonVolumeMounts -}}
{{- $useCommonVolumes := default true $spec.useCommonVolumes -}}
{{- $includeCommonEnv := default false $spec.includeCommonEnv -}}
{{- $baseContainer := dict "image" $ctx.Values.images.k8sJob -}}
{{- if $useCommonVolumeMounts }}{{- $_ := set $baseContainer "volumeMounts" $ctx.Values.jobContainer.volumeMounts -}}{{- end }}
{{- if $includeCommonEnv }}{{- $_ := set $baseContainer "env" $ctx.Values.jobContainer.env -}}{{- end }}
{{- $jobContainer := mustMerge $baseContainer (omit $jobContainerRaw "extraEnv" "extraVolumeMounts" "extraVolumes") -}}
{{- $env := default (list) $jobContainer.env -}}
{{- with $jobContainerRaw.extraEnv }}{{- $env = concat $env . -}}{{- end }}
{{- $volumeMounts := default (list) $jobContainer.volumeMounts -}}
{{- with $jobContainerRaw.extraVolumeMounts }}{{- $volumeMounts = concat $volumeMounts . -}}{{- end }}
{{- $volumes := list -}}
{{- if $useCommonVolumes }}{{- $volumes = $ctx.Values.jobContainer.volumes -}}{{- end }}
{{- if $spec.volumes }}{{- $volumes = $spec.volumes -}}{{- end }}
{{- with $spec.extraVolumes }}{{- $volumes = concat $volumes . -}}{{- end }}
{{- $command := $jobContainer.command -}}
{{- if and (not $command) $spec.scriptFile }}
{{- $command = list "bash" "-c" (include "soperator-activechecks.renderScript" (dict "path" $spec.scriptFile "ctx" $ctx)) -}}
{{- end }}
{{- if and (not $command) $spec.pythonScriptFile }}
{{- $command = list "bash" "-c" (printf "python3 - <<'PY'\n%s\nPY" (include "soperator-activechecks.renderScript" (dict "path" $spec.pythonScriptFile "ctx" $ctx))) -}}
{{- end }}
{{- $args := $jobContainer.args -}}
{{- $image := tpl (default $ctx.Values.images.k8sJob $jobContainer.image) $ctx }}
{{- if $spec.appArmorProfile }}
appArmorProfile: {{ $spec.appArmorProfile }}
{{- end }}
jobContainer:
  image: {{ $image | quote }}
{{- with $jobContainer.appArmorProfile }}
  appArmorProfile: {{ . }}
{{- end }}
{{- if $command }}
  command:
{{ toYaml $command | indent 4 }}
{{- end }}
{{- if $args }}
  args:
{{ toYaml $args | indent 4 }}
{{- end }}
{{- if $env }}
  env:
{{ toYaml $env | indent 4 }}
{{- end }}
{{- if $volumeMounts }}
  volumeMounts:
{{ toYaml $volumeMounts | indent 4 }}
{{- end }}
{{- if $volumes }}
  volumes:
{{ toYaml $volumes | indent 4 }}
{{- end }}
{{- if and $spec.mungeContainer $spec.mungeContainer.enabled }}
mungeContainer:
{{ include "soperator-activechecks.renderMungeContainer" (dict "ctx" $ctx "container" $spec.mungeContainer) | indent 2 }}
{{- end }}
{{- end -}}
