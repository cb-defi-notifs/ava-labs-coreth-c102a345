#!/usr/bin/env bash

set -euo pipefail

# Build avalanchego binary from the target version against the current state of coreth.

# e.g.,
# ./scripts/build_avalanchego.sh
# AVALANCHE_VERSION=v1.10.x ./scripts/build_avalanchego.sh
if ! [[ "$0" =~ scripts/build_avalanchego.sh ]]; then
  echo "must be run from repository root"
  exit 255
fi

# Coreth root directory
CORETH_PATH=$( cd "$( dirname "${BASH_SOURCE[0]}" )"; cd .. && pwd )

# Load the version
source "$CORETH_PATH"/scripts/versions.sh

# Always return to the coreth path on exit
function cleanup {
  cd "${CORETH_PATH}"
}
trap cleanup EXIT

echo "checking out target AvalancheGo version ${avalanche_version}"
if [[ -d "${AVALANCHEGO_CLONE_PATH}" ]]; then
  echo "updating existing clone"
  cd "${AVALANCHEGO_CLONE_PATH}"
  git fetch
else
  echo "creating new clone"
  git clone https://github.com/ava-labs/avalanchego.git "${AVALANCHEGO_CLONE_PATH}"
  cd "${AVALANCHEGO_CLONE_PATH}"
fi
# Branch will be reset to $avalanche_version if it already exists
git checkout -B "test-${avalanche_version}" "${avalanche_version}"

echo "updating coreth dependency to point to ${CORETH_PATH}"
go mod edit -replace "github.com/ava-labs/coreth=${CORETH_PATH}"
go mod tidy

echo "building avalanchego"
./scripts/build.sh -r