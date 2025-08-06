set -e

echo "Submitting k8s jobs for active check $TARGET_ACTIVE_CHECK_NAME for reserved nodes with prefix $RESERVATION_PREFIX ..."
for reservationName in $(scontrol show reservation --json | jq ".reservations.[] | select(.name | startswith(\"$RESERVATION_PREFIX\")) | .name"); do
  jobName="$TARGET_ACTIVE_CHECK_NAME-$reservationName"

  # We need to pass the reservationName to this job
  kubectl create job --from=cronjob/$TARGET_ACTIVE_CHECK_NAME $jobName
  kubectl create job --from=cronjob/$TARGET_ACTIVE_CHECK_NAME $jobName --dry-run -o "json" \
  | jq ".spec.template.spec.containers[0].env += [{ \"name\": \"RESERVATION_NAME\", value:\"$reservationName\" }]" \
  | kubectl apply -f -
done
