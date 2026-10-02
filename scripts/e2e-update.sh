#!/usr/bin/env bash
# End-to-end test of `ghost update`, on a clean machine.
#
#   scripts/e2e-update.sh
#
# "Clean machine" is a private mount namespace (bubblewrap) with an empty
# /usr/local, /var/ghost, /var/lib/ghost and /etc/systemd/system, a private
# HOME, and a PATH that has NO go and NO git on it, so the update cannot build
# anything or read a checkout. systemctl and sudo are shims. The release host is
# a local web server speaking the GitHub release API.
#
# It installs release v0.0.1, then runs the real `ghost update` against:
#   - a good, signed v0.0.2          -> must install, binaries and unit file
#   - the same again                 -> must say it is current
#   - a tampered binary              -> must be refused, nothing replaced
#   - a swapped checksum list        -> must be refused
#   - a release signed by a stranger -> must be refused
#   - a release with no signature    -> must be refused
# Nothing here touches the real machine: only the sandbox's own directories are
# written, and the real services are never contacted.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
W="${E2E_DIR:-$HOME/.cache/ghost-e2e-update}"
PORT="${E2E_PORT:-18999}"
ARCH="$(go env GOARCH)"
TOOLS="$W/tools"
fail() { echo "FAIL: $*" >&2; exit 1; }
command -v bwrap >/dev/null || fail "bubblewrap (bwrap) is required"

rm -rf "$W"
mkdir -p "$W"/{box/usr-local/bin,box/var-ghost,box/var-lib-ghost,box/systemd,box/home,shims,tools,rel,build}

echo "== build two versions (v0.0.1, v0.0.2) of ghost and ghost-web, no cgo"
build() { # version binary out
  (cd "$ROOT" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -ldflags "-s -w -X main.version=$1" -o "$3" "./cmd/$2")
}
for v in v0.0.1 v0.0.2; do
  mkdir -p "$W/build/$v"
  for b in ghost ghost-web; do build "$v" "$b" "$W/build/$v/${b}_linux_${ARCH}"; done
done
(cd "$ROOT" && go build -o "$W/ghost-release" ./cmd/ghost-release)

echo "== keys: the release signer, and a stranger"
"$W/ghost-release" keygen "$W/signer.key" > "$W/signer.pub"
"$W/ghost-release" keygen "$W/stranger.key" > "$W/stranger.pub"

# release <dir> <version> <signing key or -> [tamper|swapsums|nosig]
release() {
  local dir="$1" ver="$2" key="$3" mode="${4:-}"
  rm -rf "$dir"; mkdir -p "$dir"
  cp "$W/build/$ver/ghost_linux_$ARCH" "$W/build/$ver/ghost-web_linux_$ARCH" "$dir/"
  "$W/ghost-release" checksums "$dir/checksums.txt" "$dir/ghost_linux_$ARCH" "$dir/ghost-web_linux_$ARCH"
  [ "$key" = "-" ] || "$W/ghost-release" sign "$key" "$dir/checksums.txt"
  case "$mode" in
    tamper)   printf '\necho pwned\n' >> "$dir/ghost_linux_$ARCH" ;;
    swapsums) printf '\necho pwned\n' >> "$dir/ghost_linux_$ARCH"
              "$W/ghost-release" checksums "$dir/checksums.txt" "$dir/ghost_linux_$ARCH" "$dir/ghost-web_linux_$ARCH" ;;
  esac
  python3 - "$dir" "$ver" "$PORT" <<'PY'
import json, os, sys
d, ver, port = sys.argv[1:4]
assets = [{"name": n, "browser_download_url": f"http://127.0.0.1:{port}/{os.path.basename(d)}/{n}", "size": os.path.getsize(os.path.join(d, n))}
          for n in sorted(os.listdir(d)) if n != "latest.json"]
json.dump({"tag_name": ver, "name": ver, "body": f"- the {ver} release", "prerelease": False, "assets": assets}, open(os.path.join(d, "latest.json"), "w"))
PY
}
release "$W/rel/good"      v0.0.2 "$W/signer.key"
release "$W/rel/tamper"    v0.0.2 "$W/signer.key" tamper
release "$W/rel/swapsums"  v0.0.2 "$W/signer.key" swapsums
release "$W/rel/stranger"  v0.0.2 "$W/stranger.key"
release "$W/rel/nosig"     v0.0.2 -

# The release host: /repos/<repo>/releases/latest returns whichever scenario is
# current (the inner script copies the scenario's latest.json into place).
mkdir -p "$W/host/repos/ianclemence/ghost/releases"
ln -s "$W/rel"/* "$W/host/" 2>/dev/null || true

echo "== a clean machine: v0.0.1 installed, no go, no git"
cp "$W/build/v0.0.1/ghost_linux_$ARCH" "$W/box/usr-local/bin/ghost"
cp "$W/build/v0.0.1/ghost-web_linux_$ARCH" "$W/box/usr-local/bin/ghost-web"
chmod 755 "$W/box/usr-local/bin/"*
# Units as an older install left them, so the refresh has something to replace.
for u in ghost ghost-web; do
  sed -e 's/__USER__/root/g;s/__GROUP__/root/g;s#__GHOST_DIR__#/var/ghost#g;s#__BIN_DIR__#/usr/local/bin#g' "$ROOT/$u.service.template" > "$W/box/systemd/$u.service"
  echo "# an older unit" >> "$W/box/systemd/$u.service"
done
printf '#!/bin/sh\nwhile [ "${1#-}" != "$1" ]; do shift; done\nexec "$@"\n' > "$W/shims/sudo"
# A system install: the system unit exists (but is not running), there is no per-user unit.
printf '#!/bin/sh\ncase "$1" in --user) exit 1;; is-active) exit 3;; esac\nexit 0\n' > "$W/shims/systemctl"
chmod +x "$W/shims/"*
for t in sh bash env cat cmp cp rm mv mkdir install chmod mktemp date sleep head tail grep sed cut tr sort uniq wc ls readlink dirname basename sha256sum true false test ln; do
  p="$(command -v "$t" || true)"; [ -n "$p" ] && ln -sf "$p" "$TOOLS/$t"
done

cat > "$W/inner.sh" <<'INNER'
#!/bin/bash
set -uo pipefail
W="$1"; PORT="$2"
H="$W/host"
run_update() { GHOST_RELEASE_API="http://127.0.0.1:$PORT" GHOST_RELEASE_PUBKEY="$(cat "$W/signer.pub")" GHOST_UPDATE_SANDBOX=1 /usr/local/bin/ghost update "$@"; }
installed() { /usr/local/bin/ghost version | grep -o 'Ghost [^ ]*' | head -1 | cut -d' ' -f2; }
scenario() { cp "$W/rel/$1/latest.json" "$H/repos/ianclemence/ghost/releases/latest"; }
ok=0; bad=0
check() { if eval "$2"; then echo "  PASS  $1"; ok=$((ok+1)); else echo "  FAIL  $1"; bad=$((bad+1)); fi; }

/usr/bin/python3 -m http.server "$PORT" --bind 127.0.0.1 --directory "$H" >/dev/null 2>&1 &
SRV=$!; trap 'kill $SRV 2>/dev/null' EXIT; sleep 1

check "there is no go on this machine"  '! command -v go >/dev/null'
check "there is no git on this machine" '! command -v git >/dev/null'
check "starts at v0.0.1" '[ "$(installed)" = v0.0.1 ]'

echo "-- good signed release"
scenario good
out="$(run_update 2>&1)"; echo "$out" | sed 's/^/     | /'
check "update succeeds"                       '[ -n "$(echo "$out" | grep "Update complete")" ]'
check "ghost is now v0.0.2"                   '[ "$(installed)" = v0.0.2 ]'
check "the console binary was replaced too"   'cmp -s /usr/local/bin/ghost-web "$W/rel/good/ghost-web_linux_'"$ARCH"'"'
check "the signature was verified"            'echo "$out" | grep -q "Verified the signature"'
check "the unit file was refreshed from the release" '! grep -q "an older unit" /etc/systemd/system/ghost.service'
check "the old unit was kept as .bak"         'grep -q "an older unit" /etc/systemd/system/ghost.service.bak'
check "the release notes were shown"          'echo "$out" | grep -q "the v0.0.2 release"'
check "no staging files are left behind"      '[ -z "$(ls -d /tmp/ghost-rel-* 2>/dev/null)" ]'

echo "-- same release again"
out="$(run_update 2>&1)"
check "says it is current"                    'echo "$out" | grep -q "Already current"'

echo "-- refusals (installed binary must stay v0.0.2)"
for sc in tamper swapsums stranger nosig; do
  scenario $sc
  out="$(run_update --force 2>&1)"; rc=$?
  echo "$out" | grep -E 'refus|Error' | head -2 | sed "s/^/     [$sc] /"
  check "$sc: update fails"                   '[ $rc -ne 0 ]'
  check "$sc: nothing was replaced"           '[ "$(installed)" = v0.0.2 ] && cmp -s /usr/local/bin/ghost-web "$W/rel/good/ghost-web_linux_'"$ARCH"'"'
  check "$sc: no staging files left"          '[ -z "$(ls -d /tmp/ghost-rel-* 2>/dev/null)" ]'
done

echo "-- the real embedded key rejects a stranger even with no extra key configured"
scenario good
out="$(GHOST_RELEASE_API="http://127.0.0.1:$PORT" GHOST_UPDATE_SANDBOX=1 /usr/local/bin/ghost update --force 2>&1)"; rc=$?
check "a release signed by a throwaway key is refused by the built-in key" '[ $rc -ne 0 ] && echo "$out" | grep -q "trusted key"'

echo; echo "passed: $ok  failed: $bad"
[ "$bad" -eq 0 ]
INNER
chmod +x "$W/inner.sh"

echo "== run inside the clean machine"
bwrap --bind / / --dev-bind /dev /dev --proc /proc --unshare-pid \
  --bind "$W/box/usr-local" /usr/local --bind "$W/box/var-ghost" /var/ghost \
  --bind "$W/box/var-lib-ghost" /var/lib/ghost --bind "$W/box/systemd" /etc/systemd/system \
  --tmpfs /tmp \
  --setenv HOME "$W/box/home" --setenv ARCH "$ARCH" --setenv PATH "$W/shims:$TOOLS" \
  --setenv GHOST_SYSTEM_BIN_DIR /usr/local/bin \
  --chdir "$W/box/home" "$W/tools/bash" "$W/inner.sh" "$W" "$PORT"
