#!/bin/bash

set -euxo pipefail

VERSION="0.2.1"

echo "V$VERSION"

fail_if_lib_dne () {
	checklib=$1
	if python -c "import ctypes; ctypes.CDLL('$checklib')"; then
		echo "...found $checklib, continuing"
	else
		echo "ERROR: missing $checklib"
		exit 1
	fi
}

# first check if I can load libnvidia-ml.so.1, because nothing works without it
# (also not a typo, looks specifically for .1)

fail_if_lib_dne "libnvidia-ml.so.1"

# get list of base Nvidia libraries to check from nvidia-container-cli
full_library_list=$(nvidia-container-cli list -l | grep \\.so)
for x in $full_library_list; do
	fail_if_lib_dne "$x"
done

# check against a list of commonly needed CUDA libraries that are common in a
# standard Pytorch training session

cudalibs="libcuda libcudart libcublas libcublasLt libcufft libcurand libcusolver libcusparse libcudnn libnccl libucp libucs libuct libgdrapi libmpi libnuma"
libs_w_version="libibverbs.so.1 librdmacm.so.1 libgomp.so.1"

for x in $cudalibs; do
	fail_if_lib_dne "$x.so"
done
for x in $libs_w_version; do
	fail_if_lib_dne "$x"
done

echo "OK: all libraries exist"
exit 0 
