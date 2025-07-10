#!/bin/bash
set -e

codename="$(. /etc/os-release && echo "$VERSION_CODENAME")"

if [[ "$codename" == "jammy" ]]; then
    echo "[*] Detected Ubuntu Jammy — installing Python 3.10 from deadsnakes PPA"
    add-apt-repository ppa:deadsnakes/ppa -y
    apt-get update
    PYVER=3.10

elif [[ "$codename" == "noble" ]]; then
    echo "[*] Detected Ubuntu Noble — using system Python 3.12"
    apt-get update
    PYVER=3.12

else
    echo "[!] Unsupported Ubuntu codename: $codename"
    exit 1
fi

apt-get install -y \
    python${PYVER} \
    python${PYVER}-dev \
    python${PYVER}-venv \
    python${PYVER}-dbg



if [[ "$codename" == "jammy" ]]; then
  curl -sS https://bootstrap.pypa.io/get-pip.py | python${PYVER}
else
  apt-get install python3-pip -y
fi

ln -sf /usr/bin/python${PYVER} /usr/bin/python
ln -sf /usr/bin/python${PYVER} /usr/bin/python3

apt-get clean
rm -rf /var/lib/apt/lists/*
