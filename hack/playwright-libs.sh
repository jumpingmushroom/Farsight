#!/usr/bin/env bash
# playwright-libs.sh — make Playwright's headless Chromium runnable without
# root on Debian trixie.
#
# Installs Playwright's chromium-headless-shell (from web/), then repeatedly
# runs ldd on the shell, its bundled .so files and every library extracted
# so far. Each missing soname is mapped to a trixie/main package via the
# Contents index; the .deb is downloaded and unpacked (dpkg-deb -x) into
# ~/.local/pwlibs, until nothing is missing. Indexes and .debs are cached in
# ~/.cache/farsight-pwlibs/. When nothing is missing it exits without any
# download.
#
# It also installs a fallback system font (fonts-dejavu-core) and a
# fontconfig file: with no system fonts and no fontconfig config, headless
# Chromium fails to load CSS @font-face web fonts and text lays out with zero
# height. Point FONTCONFIG_FILE at the file printed by --fontconfig.
#
# Usage:
#   hack/playwright-libs.sh              install / top up libraries and fonts
#   hack/playwright-libs.sh --print      print the LD_LIBRARY_PATH value only
#   hack/playwright-libs.sh --fontconfig print the FONTCONFIG_FILE value only
#
#   LD_LIBRARY_PATH="$(hack/playwright-libs.sh --print)" \
#   FONTCONFIG_FILE="$(hack/playwright-libs.sh --fontconfig)" npx playwright test
set -euo pipefail

ROOT="${FARSIGHT_PWLIBS_DIR:-$HOME/.local/pwlibs}"
CACHE="${FARSIGHT_PWLIBS_CACHE:-$HOME/.cache/farsight-pwlibs}"
MIRROR="http://deb.debian.org/debian"
SUITE="trixie"
LIBPATH="$ROOT/usr/lib/x86_64-linux-gnu:$ROOT/lib/x86_64-linux-gnu"
FONTCONF="$ROOT/etc/fonts/fonts.conf"

case "${1:-}" in
--print)
	printf '%s\n' "$LIBPATH"
	exit 0
	;;
--fontconfig)
	printf '%s\n' "$FONTCONF"
	exit 0
	;;
"") ;;
*)
	echo "usage: $0 [--print | --fontconfig]" >&2
	exit 2
	;;
esac

for tool in ldd dpkg-deb python3 npx; do
	command -v "$tool" >/dev/null || { echo "playwright-libs: $tool not found" >&2; exit 1; }
done

WEB="$(cd "$(dirname "${BASH_SOURCE[0]}")/../web" && pwd)"
cd "$WEB"
[[ -d node_modules/@playwright/test ]] || { echo "playwright-libs: run 'npm ci' in web/ first" >&2; exit 1; }

npx playwright install chromium-headless-shell
SHELL_DIR="$(npx playwright install --dry-run chromium-headless-shell |
	awk '/Install location:/ && /chromium_headless_shell/ {print $3; exit}')"
SHELL_BIN="$SHELL_DIR/chrome-headless-shell-linux64/chrome-headless-shell"
[[ -x "$SHELL_BIN" ]] || { echo "playwright-libs: headless shell not found at $SHELL_BIN" >&2; exit 1; }

mkdir -p "$ROOT" "$CACHE"

python3 - "$SHELL_BIN" "$ROOT" "$CACHE" "$MIRROR" "$SUITE" "$LIBPATH" "$FONTCONF" <<'PY'
import gzip, lzma, os, re, subprocess, sys, urllib.request

shell_bin, root, cache, mirror, suite, libpath, fontconf = sys.argv[1:8]
env = dict(os.environ, LD_LIBRARY_PATH=libpath)

# Unversioned sonames that the Contents regex below does not match.
HARDCODED = {
    'libnss3.so': 'libnss3', 'libnssutil3.so': 'libnss3', 'libsmime3.so': 'libnss3',
    'libnspr4.so': 'libnspr4', 'libplc4.so': 'libnspr4', 'libplds4.so': 'libnspr4',
}

def elf_files():
    """The shell, its bundled libraries and every .so extracted so far."""
    yield shell_bin
    dirs = [os.path.dirname(shell_bin)] + libpath.split(':')
    for d in dirs:
        if not os.path.isdir(d):
            continue
        for f in sorted(os.listdir(d)):
            p = os.path.join(d, f)
            if '.so' in f and os.path.isfile(p) and not os.path.islink(p):
                yield p

def missing():
    out = set()
    for f in elf_files():
        r = subprocess.run(['ldd', f], capture_output=True, text=True, env=env)
        out.update(re.findall(r'^\s*(\S+) => not found', r.stdout, re.M))
    return out

def fetch(url, dest):
    if not os.path.exists(dest):
        print(f'playwright-libs: downloading {url}', flush=True)
        tmp = dest + '.part'
        urllib.request.urlretrieve(url, tmp)
        os.replace(tmp, dest)
    return dest

_index = None
def index():
    """(soname → package, package → pool filename), built lazily."""
    global _index
    if _index is None:
        contents = fetch(f'{mirror}/dists/{suite}/main/Contents-amd64.gz',
                         os.path.join(cache, f'{suite}-Contents-amd64.gz'))
        packages = fetch(f'{mirror}/dists/{suite}/main/binary-amd64/Packages.xz',
                         os.path.join(cache, f'{suite}-Packages.xz'))
        so2pkg = {}
        so_re = re.compile(r'^(?:usr/)?lib/x86_64-linux-gnu/([^/ ]+\.so\.[0-9]+)\s+(\S+)$')
        with gzip.open(contents, 'rt', errors='replace') as fh:
            for line in fh:
                m = so_re.match(line.rstrip('\n'))
                if m:
                    so2pkg.setdefault(m.group(1), m.group(2).split(',')[0].split('/')[-1])
        so2pkg.update(HARDCODED)
        pkg2file, cur = {}, None
        with lzma.open(packages, 'rt', errors='replace') as fh:
            for line in fh:
                if line.startswith('Package: '):
                    cur = line[9:].strip()
                elif line.startswith('Filename: ') and cur:
                    pkg2file[cur] = line[10:].strip()
        _index = (so2pkg, pkg2file)
    return _index

def install(pkg):
    _, pkg2file = index()
    deb = fetch(f'{mirror}/{pkg2file[pkg]}', os.path.join(cache, os.path.basename(pkg2file[pkg])))
    subprocess.run(['dpkg-deb', '-x', deb, root], check=True)

tried = set()
for _ in range(50):
    todo = sorted(missing() - tried)
    if not todo:
        break
    so2pkg, pkg2file = index()
    for so in todo:
        tried.add(so)
        pkg = so2pkg.get(so)
        if not pkg or pkg not in pkg2file:
            print(f'playwright-libs: no package for {so}', file=sys.stderr)
            continue
        install(pkg)
        print(f'playwright-libs: {so} <- {pkg}', flush=True)

left = sorted(missing())
if left:
    sys.exit('playwright-libs: still missing: ' + ' '.join(left))

# Fallback font + fontconfig file (see the header comment).
fontdir = os.path.join(root, 'usr/share/fonts')
if not os.path.exists(os.path.join(fontdir, 'truetype/dejavu/DejaVuSans.ttf')):
    install('fonts-dejavu-core')
    print('playwright-libs: fallback font <- fonts-dejavu-core', flush=True)
conf = f'''<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "urn:fontconfig:fonts.dtd">
<!-- Written by hack/playwright-libs.sh for headless Chromium. -->
<fontconfig>
  <dir>{fontdir}</dir>
  <cachedir>{os.path.join(cache, 'fontconfig')}</cachedir>
</fontconfig>
'''
os.makedirs(os.path.dirname(fontconf), exist_ok=True)
if not os.path.exists(fontconf) or open(fontconf).read() != conf:
    with open(fontconf, 'w') as fh:
        fh.write(conf)
    print(f'playwright-libs: wrote {fontconf}', flush=True)
print('playwright-libs: all libraries and fonts present')
PY
