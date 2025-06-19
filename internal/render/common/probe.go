package common

const (
	DefaultProbeTimeoutSeconds   = 1
	DefaultProbePeriodSeconds    = 10
	DefaultProbeSuccessThreshold = 1
	DefaultProbeFailureThreshold = 3
)

const (
	SSHStartupProbeScript = `
output=$(ssh -o BatchMode=yes -o StrictHostKeyChecking=no -p 22 localhost exit 2>&1)
code=$?
if [ "$code" -eq 0 ]; then
  echo "✅ sshd ready (connected)"
  exit 0
elif [ "$code" -eq 255 ] && echo "$output" | grep -q "Permission denied"; then
  echo "✅ sshd ready (permission denied)"
  exit 0
elif echo "$output" | grep -qi "permission denied"; then
  echo "✅ sshd ready (PD found in output)"
  exit 0
else
  echo "⏳ waiting for sshd: $output"
  exit 1
fi
`
)
