#!/usr/bin/env bash
# Copies the Valheim server's stdout log(s) and supervisord.log from a live
# instance, for local replay against farsight-logreplay (read-only; only
# `ls` and `cat` are run in the pod, nothing is written or deleted there).
#
# Usage: hack/pull-logs.sh [instance]   (default: mulevikings)
#
# The log timestamps are in the game container's TZ (mulevikings sets
# Europe/Oslo; the others leave TZ unset, i.e. UTC). The script prints the
# pod's TZ and the matching farsight-logreplay command.
set -euo pipefail
: "${KUBECONFIG:=$HOME/.kube/cloudcluster.yaml}"; export KUBECONFIG

INSTANCE="${1:-mulevikings}"
NS=gameservers
DEPLOY="deploy/${INSTANCE}-valheim"
OUT="$(cd "$(dirname "$0")/.." && pwd)/testdata-golden/logs/${INSTANCE}"
mkdir -p "$OUT"

FILES=$(kubectl -n "$NS" exec "$DEPLOY" -c valheim -- \
  sh -c 'ls /var/log/supervisor/valheim-server-stdout---supervisor-*.log /var/log/supervisor/supervisord.log 2>/dev/null')

for f in $FILES; do
  base=$(basename "$f")
  kubectl -n "$NS" exec "$DEPLOY" -c valheim -- cat "$f" > "$OUT/$base"
done

TZ_POD=$(kubectl -n "$NS" exec "$DEPLOY" -c valheim -- sh -c 'echo "${TZ:-UTC}"')

echo "Pulled logs for $INSTANCE into $OUT:" >&2
ls -la "$OUT" >&2
echo "Container TZ: $TZ_POD. Replay with:" >&2
echo "  go run ./cmd/farsight-logreplay -dir testdata-golden/logs/$INSTANCE -tz $TZ_POD" >&2
