#!/usr/bin/env bash

# Copyright 2026 Stefan Prodan
# SPDX-License-Identifier: Apache-2.0

set -o errexit

reg_localhost_port='5555'
repo_root=$(git rev-parse --show-toplevel)
timoni_bin="${repo_root}/bin/timoni"

if [ ! -x "${timoni_bin}" ]; then
  echo "timoni binary not found at ${timoni_bin}, run 'make build' first"
  exit 1
fi

"${timoni_bin}" mod push "${repo_root}/blueprints/starter" \
  "oci://localhost:${reg_localhost_port}/modules/blueprint" \
  --version=1.0.0 --resolve-symlinks
