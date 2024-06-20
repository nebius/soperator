#!/bin/bash

dd if=/dev/random bs=1024 count=1 | base64
