#!/usr/bin/env bash
# Decompiles the live Valheim server's assemblies into reference/ (gitignored)
# so world-generation ports can cite exact C# line ranges. Read-only on the cluster.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG
ROOT=$(cd "$(dirname "$0")/.." && pwd)
TOOLS=$HOME/.local/farsight-tools
export DOTNET_SYSTEM_GLOBALIZATION_INVARIANT=1 DOTNET_ROLL_FORWARD=Major DOTNET_CLI_TELEMETRY_OPTOUT=1
export DOTNET_ROOT=$TOOLS/dotnet PATH=$TOOLS/dotnet:$TOOLS/bin:$PATH
if ! command -v ilspycmd >/dev/null; then
  mkdir -p "$TOOLS"
  curl -sSL https://dot.net/v1/dotnet-install.sh -o "$TOOLS/dotnet-install.sh"
  bash "$TOOLS/dotnet-install.sh" --channel 8.0 --install-dir "$TOOLS/dotnet" >/dev/null
  dotnet tool install ilspycmd --tool-path "$TOOLS/bin" --version 8.2.0.7535 >/dev/null
fi
DLL=$(mktemp -d)
trap 'rm -rf "$DLL"' EXIT
kubectl -n gameservers exec deploy/mulevikings-valheim -c valheim -- \
  tar cf - -C /opt/valheim/server/valheim_server_Data/Managed . | tar xf - -C "$DLL"
mkdir -p "$ROOT/reference/valheim" "$ROOT/reference/utils"
ilspycmd -p -o "$ROOT/reference/valheim" -r "$DLL" "$DLL/assembly_valheim.dll" >/dev/null
ilspycmd -p -o "$ROOT/reference/utils" -r "$DLL" "$DLL/assembly_utils.dll" >/dev/null
ls "$ROOT/reference/valheim/WorldGenerator.cs" "$ROOT/reference/utils/FastNoise.cs" "$ROOT/reference/utils/DUtils.cs"
