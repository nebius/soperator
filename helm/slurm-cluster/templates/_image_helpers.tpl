{{/* Populate jail Job image */}}
{{- define "slurm-cluster.image.populateJail" -}}
    {{- printf "%s:%s" (default (printf "%s/populate_jail" (include "slurm-cluster.containerRegistry" .)) .Values.images.populateJail) .Chart.Version | quote -}}
{{- end }}

{{/* NCCL benchmark CronJob image */}}
{{- define "slurm-cluster.image.ncclBenchmark" -}}
    {{- printf "%s:%s" (default (printf "%s/nccl_benchmark" (include "slurm-cluster.containerRegistry" .)) .Values.periodicChecks.ncclBenchmark.image) .Chart.Version | quote -}}
{{- end }}

{{/* Slurmctld image */}}
{{- define "slurm-cluster.image.slurmctld" -}}
    {{- printf "%s:%s" (default (printf "%s/controller_slurmctld" (include "slurm-cluster.containerRegistry" .)) .Values.images.slurmctld) .Chart.Version | quote -}}
{{- end }}

{{/* Slurmd image */}}
{{- define "slurm-cluster.image.slurmd" -}}
    {{- printf "%s:%s" (default (printf "%s/worker_slurmd" (include "slurm-cluster.containerRegistry" .)) .Values.images.slurmd) .Chart.Version | quote -}}
{{- end }}

{{/* Sshd image */}}
{{- define "slurm-cluster.image.sshd" -}}
    {{- printf "%s:%s" (default (printf "%s/login_sshd" (include "slurm-cluster.containerRegistry" .)) .Values.images.sshd) .Chart.Version | quote -}}
{{- end }}

{{/* Munge image */}}
{{- define "slurm-cluster.image.munge" -}}
    {{- printf "%s:%s" (default (printf "%s/munge" (include "slurm-cluster.containerRegistry" .)) .Values.images.munge) .Chart.Version | quote -}}
{{- end }}
