#!/usr/bin/env bash
# hack/hash-secrets.sh — bcrypt-hash the passphrases and agent tokens from
# ~/.config/farsight/secrets.txt (see hack/gen-secrets.sh) for the
# ConfigMap. Prints one "id passphraseHash agentTokenHash" line per server.
# Bcrypt hashes aren't secret; the plaintext values never appear in output.
set -euo pipefail

SECRETS_FILE="${FARSIGHT_SECRETS_FILE:-$HOME/.config/farsight/secrets.txt}"
SERVER_IDS=(mulevikings mulevikings-old muleadventure)

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

if [[ ! -f "$SECRETS_FILE" ]]; then
  echo "hash-secrets: $SECRETS_FILE not found; run hack/gen-secrets.sh first" >&2
  exit 1
fi

# get_value KEY — the value for KEY=... in the secrets file.
get_value() {
  local key="$1" line
  line=$(grep -m1 "^${key}=" "$SECRETS_FILE") || {
    echo "hash-secrets: missing key $key in $SECRETS_FILE" >&2
    exit 1
  }
  printf '%s' "${line#*=}"
}

# hash_value VALUE — VALUE's bcrypt hash, via `farsight hash`.
hash_value() {
  printf '%s\n' "$1" | (cd "$REPO_ROOT" && go run ./cmd/farsight hash)
}

for id in "${SERVER_IDS[@]}"; do
  key_id="${id//-/_}"
  passphrase=$(get_value "PASSPHRASE_${key_id}")
  token=$(get_value "TOKEN_${key_id}")
  passphrase_hash=$(hash_value "$passphrase")
  token_hash=$(hash_value "$token")
  echo "$id $passphrase_hash $token_hash"
done
