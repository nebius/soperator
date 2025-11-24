#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/shadow
chown -h 0:42 /etc/gshadow

echo "Link home from jail to use SSH keys from there"
ln -s /mnt/jail/home /home

echo "Symlink slurm configs from jail(sconfigcontroller)"
rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm

whatToDo () {
  # If the a previous job exists and it's still running, leave it running
  # otherwise, delete it and create a new one.
  # For now we are not applying any time limits on it.
  # We can do that inside extensive-check using SBATCH --time=10:00

  slurmJobId=$(kubectl get job $jobName -o json | jq -r '.metadata.annotations."slurm-job-id"')
  if [[ "$slurmJobId" == "" ]]; then
    echo "create"
    return 0
  fi
  slurmJobState=$(scontrol show job $slurmJobId --json | jq -r '.jobs|.[0]|.job_state[0]')
  if [[ "$slurmJobState" == "RUNNING"  ||  "$slurmJobState" == "PENDING" ]]; then
    echo "noop"
    return 0
  fi

  echo "delete_create"
  return 0
}

create() {
  echo "creating job: $jobName"
  kubectl create job --from=cronjob/$TARGET_ACTIVE_CHECK_NAME $jobName --dry-run=client -o "json" \
    | jq --arg RESERVATION_NAME "$reservationName" '.spec.template.spec.containers[0].env += [{ name: "RESERVATION_NAME", value:$RESERVATION_NAME }]' \
    | kubectl apply -f -
}

delete() {
  echo "deleting previous job if it exists: $jobName"
  kubectl delete job $jobName || true
}

echo "Submitting k8s jobs for active check $TARGET_ACTIVE_CHECK_NAME for reserved nodes with prefix $RESERVATION_PREFIX ..."

submitted_jobs=0
for reservationName in $(scontrol show reservation --json | jq -r --arg RESERVATION_PREFIX "$RESERVATION_PREFIX" '.reservations | .[] | select(.name | startswith($RESERVATION_PREFIX)) | .name' ); do
  # Sanitize reservation name for Kubernetes Job name (replace : with - for RFC 1123 compliance)
  sanitizedReservationName="${reservationName//:/-}"
  jobName="$TARGET_ACTIVE_CHECK_NAME-$sanitizedReservationName"

  action=$(whatToDo)
  if [[ "$action" == "create" ]]; then
    create
    ((submitted_jobs++))
  elif [[ "$action" == "delete_create" ]]; then
    delete
    create
    ((submitted_jobs++))
  else
    echo "doing nothing. The job is still running"
  fi
done

echo "$submitted_jobs jobs were submitted"
