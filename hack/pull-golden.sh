#!/usr/bin/env bash
# Copies real world saves from the cluster for golden tests (read-only).
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG
ROOT=$(cd "$(dirname "$0")/.." && pwd)/testdata-golden
mkdir -p "$ROOT/chunked" "$ROOT/legacy"
kubectl -n gameservers exec deploy/mulevikings-valheim -c valheim -- \
  tar cf - -C /config/worlds_local MuleVikings | tar xf - -C "$ROOT/chunked"
for f in Mulennials.db Mulennials.fwl; do
  kubectl -n gameservers exec deploy/mulevikings-old-valheim -c valheim -- \
    cat "/config/worlds_local/$f" > "$ROOT/legacy/$f"
done
du -sh "$ROOT"/*
