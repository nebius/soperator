apiVersion: slurm.nebius.ai/v1alpha1
kind: ActiveCheck
metadata:
  name: "ib-write-bw-cpu"
spec:
  checkType: "slurmJob"
  name: "ib-write-bw-cpu"
  slurmClusterRefName: {{ .Values.slurmClusterRefName | quote }}
  schedule: "0 13 * * *" # 1 time a day at 13:00 UTC
  suspend: false
  runAfterCreation: true
  slurmJobSpec:
    sbatchScript: |
{{ .Files.Get "scripts/ib-write-bw-cpu.sh" | indent 6 }}
    eachWorkerJobArray: true
    jobContainer:
      image: {{ .Values.images.slurmJob | quote }}
      env:
{{ toYaml .Values.jobContainer.env | indent 8 }}
      volumeMounts:
{{ toYaml .Values.jobContainer.volumeMounts | indent 8 }}
      volumes:
{{ toYaml .Values.jobContainer.volumes | indent 8 }}
    mungeContainer:
      image: {{ .Values.images.munge | quote }}
  reactions:
    setCondition: true
    drainSlurmNode: true
