#!/usr/bin/env bash
# hack/gen-secrets.sh — generate local Farsight deployment secrets.
#
# Writes ~/.config/farsight/secrets.txt (mode 0600, directory mode 0700)
# with a cookie signing key plus one agent token, passphrase and share
# link per server. Never prints a secret value: only the file path.
#
# Idempotent by default: if the secrets file already exists, this script
# leaves it untouched and just prints its path. Pass --force to regenerate;
# even then it prompts for confirmation before overwriting.
set -euo pipefail

SECRETS_DIR="${FARSIGHT_SECRETS_DIR:-$HOME/.config/farsight}"
SECRETS_FILE="$SECRETS_DIR/secrets.txt"
SHARE_BASE_URL="https://farsight.apps.jumpingmushroom.com"
SERVER_IDS=(mulevikings mulevikings-old muleadventure)

FORCE=0
for arg in "$@"; do
  case "$arg" in
    --force) FORCE=1 ;;
    *)
      echo "usage: $0 [--force]" >&2
      exit 2
      ;;
  esac
done

# 256 short, common, lowercase English words (3-6 letters, easy to type on
# a phone) for four-word passphrases. Deliberately free of Valheim/Norse
# mythology terms and real player names, and nothing offensive. A single
# random byte (0-255) selects one word with no modulo bias, since the list
# has exactly 256 entries. Four words give 8 bits * 4 = 32 bits of entropy
# per passphrase — modest on its own, but acceptable here: passphrases are
# only ever checked through bcrypt behind the per-IP rate limiter, which
# makes both offline and bulk online guessing impractical.
WORDLIST=(
  able acid acorn actor adapt adept admit adobe
  adult after again agile alarm alert algae alike
  alive amber amount ample anchor angle ankle apple
  apron arena argue armor arrow ashen aspen atlas
  atom attic autumn avid awake azure badge baker
  bald ball balmy bamboo banjo barn basil basin
  bead beam bean bear beech beet begin bell
  belt bench berry bike bind birch bird bison
  blade blank blaze bliss block bloom blue blunt
  boat body bold bolt bonus boost boot border
  boss bowl brave bread brick bridge brief bright
  brisk broad brook brown brush bubble buck bugle
  bulb bundle bunny burro busy cabin cable cactus
  calm camel camp candy canoe canyon cargo carrot
  cedar cellar chain chair chalk charm chart cheap
  check cheek cherry chess chick chief chill chimp
  chirp choir chore civic civil clamp clan clap
  clasp claw clay clean clear clerk cliff climb
  cling clock cloth cloud clove clown club clue
  coach coast cobra cocoa coder coin comet comic
  coral corn cotton couch county cove crab craft
  crane crawl cream creek crest crew crisp crop
  crow crumb crush curl curry curve cycle daisy
  dance daring dash dawn deck deep delta dense
  derby diary dice diner dizzy dock donor dove
  draft drain drama draw dream drift drill drink
  drive drop drum dusk dusty eagle early earth
  easel east easy eave ebony edge eight elbow
  elder elk elm ember empty enjoy epic equal
  event exact extra fable facet fairy fall fancy
  fast fawn fence fern fiber field fifty finch
  fine finger fire first fish fizz flag flame
)

if [[ ${#WORDLIST[@]} -ne 256 ]]; then
  echo "gen-secrets: internal error: word list has ${#WORDLIST[@]} entries, want 256" >&2
  exit 1
fi

# random_b64url N — N random bytes from /dev/urandom, base64url, no padding.
random_b64url() {
  local n="$1"
  head -c "$n" /dev/urandom | base64 | tr -d '\n' | tr '+/' '-_' | tr -d '='
}

# random_passphrase — four random words from WORDLIST, joined with '-'.
# Uses /dev/urandom via od for the random byte stream.
random_passphrase() {
  local bytes b words=()
  bytes=$(od -An -tu1 -N4 /dev/urandom)
  for b in $bytes; do
    words+=("${WORDLIST[$b]}")
  done
  local IFS='-'
  echo "${words[*]}"
}

# urlencode STR — percent-encode STR for a URL fragment. Prefers python3,
# then jq, then a pure-bash fallback, whichever is found first on this
# machine. Passphrases only ever contain lowercase letters and '-' (both
# unreserved in RFC 3986), so no percent-encoding is actually emitted
# today, but we still route every share link through this function.
urlencode() {
  local s="$1"
  if command -v python3 >/dev/null 2>&1; then
    python3 -c 'import sys, urllib.parse; sys.stdout.write(urllib.parse.quote(sys.argv[1], safe=""))' "$s"
  elif command -v jq >/dev/null 2>&1; then
    printf '%s' "$s" | jq -sRr '@uri'
  else
    local out="" i c
    for (( i = 0; i < ${#s}; i++ )); do
      c="${s:i:1}"
      case "$c" in
        [a-zA-Z0-9.~_-]) out+="$c" ;;
        *) out+=$(printf '%%%02X' "'$c") ;;
      esac
    done
    printf '%s' "$out"
  fi
}

if [[ -f "$SECRETS_FILE" && "$FORCE" -eq 0 ]]; then
  echo "$SECRETS_FILE"
  exit 0
fi

if [[ -f "$SECRETS_FILE" && "$FORCE" -eq 1 ]]; then
  reply=""
  read -r -p "Overwrite existing $SECRETS_FILE? Type 'yes' to continue: " reply || true
  if [[ "$reply" != "yes" ]]; then
    echo "gen-secrets: aborted, file left unchanged" >&2
    echo "$SECRETS_FILE"
    exit 0
  fi
fi

umask 077
mkdir -p "$SECRETS_DIR"
chmod 700 "$SECRETS_DIR"

COOKIE_KEY=$(random_b64url 48)

declare -A TOKENS PASSPHRASES SHARE_LINKS
for id in "${SERVER_IDS[@]}"; do
  TOKENS["$id"]=$(random_b64url 32)
  PASSPHRASES["$id"]=$(random_passphrase)
  encoded=$(urlencode "${PASSPHRASES[$id]}")
  SHARE_LINKS["$id"]="$SHARE_BASE_URL/#s=$id&k=$encoded"
done

tmp=$(mktemp "$SECRETS_DIR/.secrets.XXXXXX")
# Remove the partial file if anything fails before it replaces secrets.txt.
trap 'rm -f "$tmp"' EXIT
chmod 600 "$tmp"
{
  echo "FARSIGHT_COOKIE_KEY=$COOKIE_KEY"
  for id in "${SERVER_IDS[@]}"; do
    key_id="${id//-/_}"
    echo "TOKEN_${key_id}=${TOKENS[$id]}"
    echo "PASSPHRASE_${key_id}=${PASSPHRASES[$id]}"
    echo "SHARE_LINK_${key_id}=${SHARE_LINKS[$id]}"
  done
} > "$tmp"
mv -f "$tmp" "$SECRETS_FILE"
trap - EXIT

echo "$SECRETS_FILE"
