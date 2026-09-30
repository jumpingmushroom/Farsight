#!/usr/bin/env bash
# Copies the SoftRef manifests (prefab name source) from a running Valheim pod.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG
OUT=${1:-/tmp/farsight-manifests}
mkdir -p "$OUT"
SRC=/opt/valheim/server/valheim_server_Data/StreamingAssets/SoftRef
for f in manifest manifest_extended; do
  kubectl -n gameservers exec deploy/mulevikings-valheim -c valheim -- cat "$SRC/$f" > "$OUT/$f"
done
ls -la "$OUT"
