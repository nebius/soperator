#!/bin/bash

echo "Waiting for Slurm controller to be ready..."
max_attempts=60
attempt=0

# Create symlink to slurm configs (same as worker entrypoint)
echo "Creating symlink to slurm configs..."
rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm

# Now try to ping the controller using scontrol
echo "Checking slurmctld readiness..."
attempt=0
while [ $attempt -lt $max_attempts ]; do
	echo "Attempt $((attempt + 1))/$max_attempts: Checking controller readiness..."

	# Try to ping the controller using scontrol
	echo "Running: scontrol ping"
	if scontrol_output=$(scontrol ping 2>&1); then
		echo "Controller is ready!"
		echo -e "scontrol ping output:\n$scontrol_output"
		exit 0
	else
		echo -e "scontrol ping failed with output:\n$scontrol_output"
	fi

	attempt=$((attempt + 1))
	sleep 5
done

echo "ERROR: Controller did not become ready after $max_attempts attempts"
exit 1
