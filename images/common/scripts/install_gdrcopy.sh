#!/bin/bash

pushd /tmp || exit 1
  git clone https://github.com/NVIDIA/gdrcopy.git
  pushd gdrcopy || exit 1
    make lib_install DESTLIB=/usr/lib/x86_64-linux-gnu/
    make exes_install DESTBIN=/usr/bin/
  popd || exit
  rm -rf /tmp/*
popd || exit 1
