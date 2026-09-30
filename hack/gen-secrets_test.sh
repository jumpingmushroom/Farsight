#!/usr/bin/env bash
# hack/gen-secrets_test.sh — smoke test for hack/gen-secrets.sh.
#
# Runs the generator against a throwaway HOME and asserts the file mode,
# key set, passphrase format, token length and idempotency. Never prints a
# secret value: only pass/fail (and, on failure, which assertion tripped).
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
GEN="$SCRIPT_DIR/gen-secrets.sh"

TMPHOME=$(mktemp -d)
trap 'rm -rf "$TMPHOME"' EXIT

fail() {
  echo "FAIL: $1"
  exit 1
}

secrets_file=$(HOME="$TMPHOME" "$GEN") || fail "first run exited non-zero"
[[ -f "$secrets_file" ]] || fail "secrets file was not created"

# File mode is 0600.
mode=$(stat -c '%a' "$secrets_file" 2>/dev/null || stat -f '%Lp' "$secrets_file")
[[ "$mode" == "600" ]] || fail "secrets file mode is $mode, want 600"

# Key set is exactly the expected one.
actual_keys=$(cut -d= -f1 "$secrets_file" | sort)
expected_keys=$(
  printf '%s\n' \
    FARSIGHT_COOKIE_KEY \
    TOKEN_mulevikings PASSPHRASE_mulevikings SHARE_LINK_mulevikings \
    TOKEN_mulevikings_old PASSPHRASE_mulevikings_old SHARE_LINK_mulevikings_old \
    TOKEN_muleadventure PASSPHRASE_muleadventure SHARE_LINK_muleadventure \
    | sort
)
[[ "$actual_keys" == "$expected_keys" ]] || fail "key set mismatch"

# Passphrase format: four 3-7 letter lowercase words joined with '-'.
for id in mulevikings mulevikings_old muleadventure; do
  val=$(grep "^PASSPHRASE_${id}=" "$secrets_file" | cut -d= -f2-)
  [[ "$val" =~ ^[a-z]{3,7}(-[a-z]{3,7}){3}$ ]] || fail "passphrase for $id has bad format"
done

# Token length: at least 43 base64url chars (32 random bytes, no padding).
for id in mulevikings mulevikings_old muleadventure; do
  val=$(grep "^TOKEN_${id}=" "$secrets_file" | cut -d= -f2-)
  len=${#val}
  [[ $len -ge 43 ]] || fail "token for $id is $len chars, want >= 43"
done

# Cookie key: also base64url, sanity-check it isn't empty.
cookie=$(grep '^FARSIGHT_COOKIE_KEY=' "$secrets_file" | cut -d= -f2-)
[[ -n "$cookie" ]] || fail "FARSIGHT_COOKIE_KEY is empty"

# A second run is idempotent: same path, unchanged file.
sum_before=$(sha256sum "$secrets_file" | awk '{print $1}')
second_path=$(HOME="$TMPHOME" "$GEN") || fail "second run exited non-zero"
[[ "$second_path" == "$secrets_file" ]] || fail "second run printed a different path"
sum_after=$(sha256sum "$secrets_file" | awk '{print $1}')
[[ "$sum_before" == "$sum_after" ]] || fail "second run changed the secrets file"

echo "PASS"
