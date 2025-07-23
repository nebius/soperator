#!/bin/bash

echo "Waiting for Slurm controller to be ready..."
controller_service="${CONTROLLER_SERVICE}"
controller_port="${CONTROLLER_PORT:-6817}"  # Default to 6817 if not set
max_attempts=60
attempt=0

# Create symlink to slurm configs (same as worker entrypoint)
echo "Creating symlink to slurm configs..."
rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm

# Wait for controller service to be resolvable via DNS
echo "Checking controller service DNS resolution..."
attempt=0
while [ $attempt -lt $max_attempts ]; do
	if timeout 1 bash -c "</dev/tcp/$controller_service/$controller_port" >/dev/null 2>&1; then
		echo "Controller service is reachable on port $controller_port"
		break
	fi
	echo "Attempt $((attempt + 1))/$max_attempts: Waiting for controller service TCP port $controller_port..."
	attempt=$((attempt + 1))
	sleep 5
done

if ! timeout 1 bash -c "</dev/tcp/$controller_service/$controller_port" >/dev/null 2>&1; then
	echo "ERROR: Controller service TCP port $controller_port not reachable after $max_attempts attempts"
	exit 1
fi

# Now try to ping the controller using scontrol
echo "Checking slurmctld readiness..."
attempt=0
while [ $attempt -lt $max_attempts ]; do
	echo "Attempt $((attempt + 1))/$max_attempts: Checking controller readiness..."

	# Try to ping the controller using scontrol
	echo "Running: scontrol ping"
	if scontrol_output=$(scontrol ping 2>&1); then
		echo "Controller is ready!"
		echo "scontrol ping output: $scontrol_output"
		exit 0
	else
		echo "scontrol ping failed with output: $scontrol_output"
	fi

	attempt=$((attempt + 1))
	sleep 5
done

echo "ERROR: Controller did not become ready after $max_attempts attempts"
exit 1
