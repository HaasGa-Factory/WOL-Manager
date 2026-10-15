#!/usr/bin/env bash
set -Eeuo pipefail

REPO=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
WORK=$(mktemp -d)
trap 'rm -rf -- "$WORK"' EXIT
PRIVATE_KEY="$WORK/release-private.pem"
PUBLIC_KEY="$WORK/release-public.pem"
PUBLICATION_COMMIT=1111111111111111111111111111111111111111

fail() { echo "ECHEC: $*" >&2; exit 1; }
assert_file() { [[ -f "$1" ]] || fail "fichier absent: $1"; }
assert_contains() { grep -Fq "$2" "$1" || fail "'$2' absent de $1"; }

openssl genpkey -algorithm Ed25519 -out "$PRIVATE_KEY"
openssl pkey -in "$PRIVATE_KEY" -pubout -out "$PUBLIC_KEY"

MOCK_BIN="$WORK/mock-bin"
mkdir -p "$MOCK_BIN"
for command in ip ping getent useradd groupadd dpkg-query; do
  cat >"$MOCK_BIN/$command" <<'EOF'
#!/bin/sh
exit 0
EOF
  chmod +x "$MOCK_BIN/$command"
done
cat >"$MOCK_BIN/apt" <<'EOF'
#!/bin/sh
if [ "${1:-}" = list ]; then echo 'Listing...'; fi
EOF
cat >"$MOCK_BIN/apt-get" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >>"${APT_LOG:?}"
[ -z "${WOL_TEST_DEP_MARKER:-}" ] || : >"$WOL_TEST_DEP_MARKER"
EOF
cat >"$MOCK_BIN/go" <<'EOF'
#!/bin/sh
echo "Go ne doit jamais être appelé" >>"${GO_LOG:?}"
exit 99
EOF
cat >"$MOCK_BIN/systemctl" <<'EOF'
#!/bin/sh
state=${SYSTEMD_STATE:?}
case "${1:-}" in
  is-active) test -f "$state/active" ;;
  is-enabled) test -f "$state/enabled" ;;
  stop) rm -f "$state/active" ;;
  restart) : >"$state/active" ;;
  enable) : >"$state/enabled"; case " $* " in *' --now '*) : >"$state/active";; esac ;;
  disable) rm -f "$state/enabled" "$state/active" ;;
  daemon-reload) : ;;
  *) : ;;
esac
EOF
cat >"$MOCK_BIN/install" <<'EOF'
#!/bin/sh
directory=0
mode=
while [ "$#" -gt 0 ]; do
  case "$1" in
    -d) directory=1; shift ;;
    -m) mode=$2; shift 2 ;;
    *) break ;;
  esac
done
if [ "$directory" = 1 ]; then
  mkdir -p "$@"
  exit 0
fi
[ "$#" -eq 2 ] || exit 2
mkdir -p "$(dirname "$2")"
cp "$1" "$2"
chmod +x "$2" 2>/dev/null || true
EOF
chmod +x "$MOCK_BIN/apt" "$MOCK_BIN/apt-get" "$MOCK_BIN/go" "$MOCK_BIN/systemctl" "$MOCK_BIN/install"

make_binary() {
  local path=$1 version=$2 revision=$3 channel=$4 post_fail=${5:-0}
  cat >"$path" <<EOF
#!/bin/sh
if [ "\${1:-}" = version ]; then
  if [ "$post_fail" = 1 ] && printf '%s' "\$0" | grep -q '/usr/local/bin/wol\$'; then exit 42; fi
  case "\${2:-}" in
    --source-revision|--commit) echo '$revision' ;;
    --channel) echo '$channel' ;;
    *) echo '$version' ;;
  esac
  exit 0
fi
if [ "\${3:-}" = config ] && [ "\${4:-}" = check ]; then exit 0; fi
exit 0
EOF
  chmod +x "$path"
}

make_distribution() {
  local directory=$1 version=$2 revision=$3 channel=$4 post_fail=${5:-0}
  mkdir -p "$directory"
  make_binary "$directory/wol-linux-amd64" "$version" "$revision" "$channel" "$post_fail"
  make_binary "$directory/wol-linux-arm64" "$version" "$revision" "$channel" "$post_fail"
  local amd64_sha arm64_sha
  amd64_sha=$(sha256sum "$directory/wol-linux-amd64" | awk '{print $1}')
  arm64_sha=$(sha256sum "$directory/wol-linux-arm64" | awk '{print $1}')
  cat >"$directory/manifest.json" <<EOF
{
  "manifest_version": 1,
  "version": "$version",
  "channel": "$channel",
  "source_revision": "$revision",
  "key_id": "wol-manager-2026-01",
  "linux_amd64_file": "wol-linux-amd64",
  "linux_amd64_sha256": "$amd64_sha",
  "linux_arm64_file": "wol-linux-arm64",
  "linux_arm64_sha256": "$arm64_sha"
}
EOF
  openssl pkeyutl -sign -rawin -inkey "$PRIVATE_KEY" -in "$directory/manifest.json" -out "$directory/signature.bin"
  openssl base64 -A -in "$directory/signature.bin" -out "$directory/manifest.json.sig"
  printf '\n' >>"$directory/manifest.json.sig"
}

prepare_payload() {
  local payload=$1 version=$2 revision=$3 channel=${4:-main}
  mkdir -p "$payload/configs" "$payload/packaging/systemd" "$payload/dist"
  cp "$REPO/install.sh" "$payload/install.sh"
  cp "$REPO/configs/wol.example.yaml" "$payload/configs/wol.example.yaml"
  cp "$REPO/packaging/systemd/wol-manager.service" "$payload/packaging/systemd/wol-manager.service"
  cp "$PUBLIC_KEY" "$payload/packaging/release-public.pem"
  make_distribution "$payload/dist" "$version" "$revision" "$channel"
  chmod +x "$payload/install.sh"
}

prepare_root() {
  local root=$1 debian=$2
  mkdir -p "$root/etc/ssl/certs" "$root/systemd-state"
  printf 'ID=debian\nVERSION_ID="%s"\n' "$debian" >"$root/os-release"
  : >"$root/etc/ssl/certs/ca-certificates.crt"
}

run_installer() {
  local payload=$1 root=$2 arch=$3
  shift 3
  APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$root/systemd-state" \
  WOL_ROOT="$root" WOL_OS_RELEASE="$root/os-release" WOL_ARCH="$arch" WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  PATH="$MOCK_BIN:$PATH" "$payload/install.sh" "$@"
}

REV1=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
REV2=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
REV3=cccccccccccccccccccccccccccccccccccccccc
PAYLOAD="$WORK/payload"
prepare_payload "$PAYLOAD" 1.0.0-beta.1 "$REV1"

echo "[1/11] Debian 12 AMD64, binaire local, sans Go"
ROOT12="$WORK/root12"
prepare_root "$ROOT12" 12
: >"$WORK/go.log"
run_installer "$PAYLOAD" "$ROOT12" amd64 --non-interactive --yes --service cli
assert_file "$ROOT12/usr/local/bin/wol"
assert_file "$ROOT12/usr/local/lib/wol-manager/install.sh"
assert_file "$ROOT12/usr/local/lib/wol-manager/packaging/release-public.pem"
[[ ! -s "$WORK/go.log" ]] || fail "Go a été utilisé pendant l'installation"

echo "[2/11] installation existante non remplacée"
if run_installer "$PAYLOAD" "$ROOT12" amd64 --non-interactive --yes --service cli >"$WORK/reinstall.log" 2>&1; then
  fail "une réinstallation silencieuse a été acceptée"
fi
assert_contains "$WORK/reinstall.log" "utilisez sudo wol update"

echo "[3/11] Debian 13 ARM64"
ROOT13="$WORK/root13"
prepare_root "$ROOT13" 13
run_installer "$PAYLOAD" "$ROOT13" arm64 --non-interactive --yes --service cli
[[ $("$ROOT13/usr/local/bin/wol" version --channel) = main ]] || fail "mauvais canal ARM64"

echo "[4/11] dépendance manquante avec consentement"
ROOT_DEP="$WORK/root-dep"
prepare_root "$ROOT_DEP" 12
DEP_MARKER="$WORK/dependency-installed"
APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT_DEP/systemd-state" \
WOL_ROOT="$ROOT_DEP" WOL_OS_RELEASE="$ROOT_DEP/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
WOL_TEST_MISSING_PACKAGE=wol-test-runtime WOL_TEST_DEP_MARKER="$DEP_MARKER" \
PATH="$MOCK_BIN:$PATH" "$PAYLOAD/install.sh" --non-interactive --yes --service cli
assert_file "$DEP_MARKER"
assert_contains "$WORK/apt.log" "wol-test-runtime"

REMOTE="$WORK/remote"
mkdir -p "$REMOTE/main" "$REMOTE/dev"
make_distribution "$REMOTE/main" 1.0.0-beta.2 "$REV2" main
make_distribution "$REMOTE/dev" 1.0.0-beta.3 "$REV3" dev
cat >"$MOCK_BIN/curl" <<'EOF'
#!/bin/sh
output=
url=
while [ "$#" -gt 0 ]; do
  case "$1" in -o) output=$2; shift ;; http*) url=$1 ;; esac
  shift
done
case "$url" in
  */commits/*) printf '{\n  "sha": "%s"\n}\n' "${PUBLICATION_COMMIT:?}" >"$output" ;;
  *) cp "${REMOTE_DIR:?}/${REMOTE_CHANNEL:?}/$(basename "$url")" "$output" ;;
esac
EOF
chmod +x "$MOCK_BIN/curl"

run_update() {
  local root=$1 channel=$2
  shift 2
  APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$root/systemd-state" \
  REMOTE_DIR="$REMOTE" REMOTE_CHANNEL="$channel" PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
  WOL_ROOT="$root" WOL_OS_RELEASE="$root/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$MOCK_BIN:$PATH" \
  "$root/usr/local/lib/wol-manager/install.sh" --update --channel "$channel" "$@"
}

echo "[5/11] vérification main sans installation"
run_update "$ROOT12" main --check --non-interactive >"$WORK/check.log"
assert_contains "$WORK/check.log" "Mise à jour disponible"
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.1 ]] || fail "--check a modifié le binaire"

echo "[6/11] mise à jour main et préservation des données"
mkdir -p "$ROOT12/etc/wol-manager/secrets" "$ROOT12/var/lib/wol-manager" "$ROOT12/var/log/wol-manager"
cp "$REPO/configs/wol.example.yaml" "$ROOT12/etc/wol-manager/wol.yaml"
echo secret >"$ROOT12/etc/wol-manager/secrets/token"
echo state >"$ROOT12/var/lib/wol-manager/state.keep"
echo log >"$ROOT12/var/log/wol-manager/audit.keep"
run_update "$ROOT12" main --yes --non-interactive
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.2 ]] || fail "mise à jour main absente"
assert_file "$ROOT12/etc/wol-manager/secrets/token"
assert_file "$ROOT12/var/lib/wol-manager/state.keep"
assert_file "$ROOT12/var/log/wol-manager/audit.keep"
assert_contains "$ROOT12/etc/wol-manager/release.conf" "default_channel=main"

echo "[7/11] absence de mise à jour"
run_update "$ROOT12" main --check --non-interactive >"$WORK/current.log"
assert_contains "$WORK/current.log" "est à jour"

echo "[8/11] canal dev temporaire"
run_update "$ROOT12" dev --yes --non-interactive >"$WORK/dev.log" 2>&1
assert_contains "$WORK/dev.log" "CANAL DEV/BETA"
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.3 ]] || fail "mise à jour dev absente"
assert_contains "$ROOT12/etc/wol-manager/release.conf" "default_channel=main"

echo "[9/11] manifeste altéré refusé"
cp -R "$REMOTE/main" "$WORK/tampered"
sed -i 's/beta.2/beta.9/' "$WORK/tampered/manifest.json"
if REMOTE_DIR="$WORK" REMOTE_CHANNEL=tampered PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
  APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT12/systemd-state" \
  WOL_ROOT="$ROOT12" WOL_OS_RELEASE="$ROOT12/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$MOCK_BIN:$PATH" \
  "$ROOT12/usr/local/lib/wol-manager/install.sh" --update --check --non-interactive >"$WORK/tampered.log" 2>&1; then
  fail "un manifeste altéré a été accepté"
fi
assert_contains "$WORK/tampered.log" "Authenticité du manifeste non vérifiée"

echo "[10/11] rollback et état systemd"
ROOT_ROLL="$WORK/root-roll"
prepare_root "$ROOT_ROLL" 12
run_installer "$PAYLOAD" "$ROOT_ROLL" amd64 --non-interactive --yes --service install
cp "$REPO/configs/wol.example.yaml" "$ROOT_ROLL/etc/wol-manager/wol.yaml"
: >"$ROOT_ROLL/systemd-state/enabled"
: >"$ROOT_ROLL/systemd-state/active"
make_distribution "$REMOTE/main" 1.0.0-beta.4 dddddddddddddddddddddddddddddddddddddddd main 1
if APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT_ROLL/systemd-state" \
  REMOTE_DIR="$REMOTE" REMOTE_CHANNEL=main PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
  WOL_ROOT="$ROOT_ROLL" WOL_OS_RELEASE="$ROOT_ROLL/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux WOL_TEST_SYSTEMD=1 \
  WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$MOCK_BIN:$PATH" \
  "$ROOT_ROLL/usr/local/lib/wol-manager/install.sh" --update --yes --non-interactive >"$WORK/rollback.log" 2>&1; then
  fail "la vérification post-installation devait échouer"
fi
[[ $("$ROOT_ROLL/usr/local/bin/wol" version) = 1.0.0-beta.1 ]] || fail "rollback du binaire absent"
assert_file "$ROOT_ROLL/systemd-state/enabled"
assert_file "$ROOT_ROLL/systemd-state/active"
assert_contains "$WORK/rollback.log" "Rollback terminé"

echo "[11/11] échec initial sans installation partielle"
PAYLOAD_FAIL="$WORK/payload-fail"
prepare_payload "$PAYLOAD_FAIL" 1.0.0-beta.1 "$REV1"
make_distribution "$PAYLOAD_FAIL/dist" 1.0.0-beta.1 "$REV1" main 1
ROOT_FAIL="$WORK/root-fail"
prepare_root "$ROOT_FAIL" 12
if run_installer "$PAYLOAD_FAIL" "$ROOT_FAIL" amd64 --non-interactive --yes --service cli >"$WORK/initial-fail.log" 2>&1; then
  fail "la vérification initiale devait échouer"
fi
[[ ! -e "$ROOT_FAIL/usr/local/bin/wol" ]] || fail "binaire partiel conservé"
[[ ! -e "$ROOT_FAIL/usr/local/lib/wol-manager" ]] || fail "installateur partiel conservé"
assert_contains "$WORK/initial-fail.log" "Rollback terminé"

echo "Tous les tests simulés de l'installateur ont réussi."
