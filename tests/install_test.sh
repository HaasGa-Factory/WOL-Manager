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
assert_not_contains() { ! grep -Fq "$2" "$1" || fail "'$2' présent dans $1"; }

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
cat >"$MOCK_BIN/curl" <<'EOF'
#!/bin/sh
exit 0
EOF
cat >"$MOCK_BIN/systemctl" <<'EOF'
#!/bin/sh
state=${SYSTEMD_STATE:?}
case "${1:-}" in
  is-active) test -f "$state/active" ;;
  is-enabled) test -f "$state/enabled" ;;
  stop) rm -f "$state/active" ;;
  restart) : >"$state/active" ;;
  enable)
    : >"$state/enabled"
    case " $* " in
      *' --now '*)
        grep -Fq '"id":"admin"' "${WOL_ROOT:?}/etc/wol-manager/secrets/tokens.json" || exit 42
        : >"$state/active"
        ;;
    esac
    ;;
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
  [ -z "$mode" ] || chmod "$mode" "$@"
  exit 0
fi
[ "$#" -eq 2 ] || exit 2
mkdir -p "$(dirname "$2")"
cp "$1" "$2"
[ -z "$mode" ] || chmod "$mode" "$2"
EOF
chmod +x "$MOCK_BIN/apt" "$MOCK_BIN/apt-get" "$MOCK_BIN/go" "$MOCK_BIN/curl" "$MOCK_BIN/systemctl" "$MOCK_BIN/install"

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
if [ "\${1:-}" = completion ] && [ "\${2:-}" = bash ]; then
  printf '_wol() { :; }\ncomplete -F _wol wol\n'
  exit 0
fi
if [ "\${1:-}" = --config ] && [ "\${3:-}" = setup ]; then
  mkdir -p "\$(dirname "\$2")"
  printf 'version: 1\n' >"\$2"
  exit 0
fi
if [ "\${1:-}" = --config ] && [ "\${3:-}" = token ] && [ "\${4:-}" = create ]; then
  mkdir -p "\$(dirname "\$2")/secrets"
  printf '{"tokens":[{"id":"admin","sha256":"%064d","permissions":["admin"]}]}\n' 0 >"\$(dirname "\$2")/secrets/tokens.json"
  chmod 0600 "\$(dirname "\$2")/secrets/tokens.json"
  echo 'JETON (simulation): test-only'
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
  cp "$REPO/install.sh" "$directory/install.sh"
  chmod +x "$directory/install.sh"
  local installer_sha
  installer_sha=$(sha256sum "$directory/install.sh" | awk '{print $1}')
  cat >"$directory/manifest.json" <<EOF
{
  "manifest_version": 2,
  "version": "$version",
  "channel": "$channel",
  "source_revision": "$revision",
  "key_id": "wol-manager-2026-01",
  "installer_file": "install.sh",
  "installer_sha256": "$installer_sha",
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

make_legacy_distribution() {
  local directory=$1 version=$2 revision=$3 channel=$4 format=${5:-1}
  mkdir -p "$directory"
  make_binary "$directory/wol-linux-amd64" "$version" "$revision" "$channel"
  make_binary "$directory/wol-linux-arm64" "$version" "$revision" "$channel"
  local amd64_sha arm64_sha
  amd64_sha=$(sha256sum "$directory/wol-linux-amd64" | awk '{print $1}')
  arm64_sha=$(sha256sum "$directory/wol-linux-arm64" | awk '{print $1}')
  cat >"$directory/manifest.json" <<EOF
{
  "manifest_version": $format,
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
  mkdir -p "$root/etc/ssl/certs" "$root/etc/bash_completion.d" "$root/usr/share/bash-completion/completions" "$root/systemd-state"
  printf 'ID=debian\nVERSION_ID="%s"\n' "$debian" >"$root/os-release"
  : >"$root/etc/ssl/certs/ca-certificates.crt"
  : >"$root/usr/share/bash-completion/bash_completion"
  printf '# fichier Debian pour une autre commande wol\n_comp_cmd_wol() { :; }\ncomplete -F _comp_cmd_wol wol\n' >"$root/usr/share/bash-completion/completions/wol"
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

echo "[1/16] Debian 12 AMD64, binaire local, sans Go"
ROOT12="$WORK/root12"
prepare_root "$ROOT12" 12
DEBIAN_COMPLETION_SHA=$(sha256sum "$ROOT12/usr/share/bash-completion/completions/wol" | awk '{print $1}')
: >"$WORK/go.log"
run_installer "$PAYLOAD" "$ROOT12" amd64 --non-interactive --yes --service cli
assert_file "$ROOT12/usr/local/bin/wol"
assert_file "$ROOT12/usr/local/lib/wol-manager/install.sh"
assert_file "$ROOT12/usr/local/lib/wol-manager/packaging/release-public.pem"
assert_file "$ROOT12/etc/bash_completion.d/wol-manager"
assert_contains "$ROOT12/etc/bash_completion.d/wol-manager" "complete -F _wol wol"
[[ $(stat -c %a "$ROOT12/etc/bash_completion.d/wol-manager") = 644 ]] || fail "mode incorrect pour la complétion WoL Manager"
[[ $(sha256sum "$ROOT12/usr/share/bash-completion/completions/wol" | awk '{print $1}') = "$DEBIAN_COMPLETION_SHA" ]] || fail "le fichier de complétion Debian a été modifié"
bash --noprofile --norc -c 'source "$1"; complete -p wol | grep -Fq "complete -F _wol wol"' _ "$ROOT12/etc/bash_completion.d/wol-manager" || fail "la complétion WoL Manager ne se charge pas"
assert_file "$ROOT12/etc/wol-manager/secrets/tokens.json"
[[ $(stat -c %a "$ROOT12/etc/wol-manager/secrets/tokens.json") = 600 ]] || fail "permissions dangereuses sur le fichier de jetons"
[[ ! -s "$WORK/go.log" ]] || fail "Go a été utilisé pendant l'installation"

echo "[2/16] binaire local et curl manquant"
ROOT_CURL="$WORK/root-curl"
prepare_root "$ROOT_CURL" 12
CURL_MARKER="$WORK/curl-installed"
: >"$WORK/apt.log"
APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT_CURL/systemd-state" \
WOL_ROOT="$ROOT_CURL" WOL_OS_RELEASE="$ROOT_CURL/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
WOL_TEST_FORCE_MISSING_CURL=1 WOL_TEST_DEP_MARKER="$CURL_MARKER" \
PATH="$MOCK_BIN:$PATH" "$PAYLOAD/install.sh" --non-interactive --yes --service cli
assert_file "$CURL_MARKER"
assert_contains "$WORK/apt.log" "curl"

echo "[3/16] installation existante non remplacée"
if run_installer "$PAYLOAD" "$ROOT12" amd64 --non-interactive --yes --service cli >"$WORK/reinstall.log" 2>&1; then
  fail "une réinstallation silencieuse a été acceptée"
fi
assert_contains "$WORK/reinstall.log" "utilisez sudo wol update"

echo "[4/16] Debian 13 ARM64"
ROOT13="$WORK/root13"
prepare_root "$ROOT13" 13
run_installer "$PAYLOAD" "$ROOT13" arm64 --non-interactive --yes --service cli
[[ $("$ROOT13/usr/local/bin/wol" version --channel) = main ]] || fail "mauvais canal ARM64"

echo "[5/16] dépendance manquante avec consentement"
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

echo "[sécurité PATH] vérification sans /usr/sbin et modification sans root refusée"
UNTRUSTED_BIN="$WORK/untrusted-bin"
mkdir -p "$UNTRUSTED_BIN"
cat >"$UNTRUSTED_BIN/useradd" <<'EOF'
#!/bin/sh
: >"${UNTRUSTED_USERADD_MARKER:?}"
exit 0
EOF
chmod +x "$UNTRUSTED_BIN/useradd"
TRUSTED_TEST_PATH="$MOCK_BIN:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
UNTRUSTED_USERADD_MARKER="$WORK/untrusted-useradd-used" WOL_TEST_SYSTEM_PATH="$TRUSTED_TEST_PATH" \
  APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT12/systemd-state" \
  REMOTE_DIR="$REMOTE" REMOTE_CHANNEL=dev PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
  WOL_ROOT="$ROOT12" WOL_OS_RELEASE="$ROOT12/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$UNTRUSTED_BIN:/usr/bin:/bin" \
  "$ROOT12/usr/local/lib/wol-manager/install.sh" --update --check --channel dev --non-interactive >"$WORK/path-check.log"
assert_not_contains "$WORK/path-check.log" "Dépendances manquantes: passwd"
[[ ! -e "$WORK/untrusted-useradd-used" ]] || fail "un useradd provenant du PATH utilisateur a été exécuté"
if WOL_TEST_UID=1000 WOL_TEST_SYSTEM_PATH="$TRUSTED_TEST_PATH" \
  APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT12/systemd-state" \
  REMOTE_DIR="$REMOTE" REMOTE_CHANNEL=dev PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
  WOL_ROOT="$ROOT12" WOL_OS_RELEASE="$ROOT12/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$UNTRUSTED_BIN:/usr/bin:/bin" \
  "$ROOT12/usr/local/lib/wol-manager/install.sh" --update --channel dev --yes --non-interactive >"$WORK/non-root.log" 2>&1; then
  fail "une mise à jour modificatrice sans root a été acceptée"
fi
assert_contains "$WORK/non-root.log" "Exécutez cette action avec sudo"
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.1 ]] || fail "la tentative sans root a modifié le binaire"

run_update() {
  local root=$1 channel=$2
  shift 2
  run_update_from "$REMOTE" "$root" "$channel" --channel "$channel" "$@"
}

run_update_from() {
  local remote=$1 root=$2 remote_channel=$3
  shift 3
  APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$root/systemd-state" \
  REMOTE_DIR="$remote" REMOTE_CHANNEL="$remote_channel" PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
  WOL_ROOT="$root" WOL_OS_RELEASE="$root/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$MOCK_BIN:$PATH" \
  "$root/usr/local/lib/wol-manager/install.sh" --update "$@"
}

echo "[6/16] vérification de mise à jour sans curl et sans installation APT"
: >"$WORK/apt.log"
if APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT12/systemd-state" \
  WOL_ROOT="$ROOT12" WOL_OS_RELEASE="$ROOT12/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  WOL_TEST_FORCE_MISSING_CURL=1 WOL_TEST_DEP_MARKER="$WORK/curl-must-not-install" \
  PATH="$MOCK_BIN:$PATH" "$ROOT12/usr/local/lib/wol-manager/install.sh" --update --check --non-interactive >"$WORK/check-no-curl.log" 2>&1; then
  fail "wol update --check a accepté l'absence de curl"
fi
assert_contains "$WORK/check-no-curl.log" "curl"
[[ ! -s "$WORK/apt.log" ]] || fail "wol update --check a installé un paquet"

echo "[7/16] première installation depuis GitHub simulé"
PAYLOAD_REMOTE="$WORK/payload-remote"
prepare_payload "$PAYLOAD_REMOTE" 1.0.0-beta.1 "$REV1"
rm -f "$PAYLOAD_REMOTE/dist/wol-linux-amd64" "$PAYLOAD_REMOTE/dist/wol-linux-arm64"
ROOT_REMOTE="$WORK/root-remote"
prepare_root "$ROOT_REMOTE" 12
APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT_REMOTE/systemd-state" \
REMOTE_DIR="$REMOTE" REMOTE_CHANNEL=main PUBLICATION_COMMIT="$PUBLICATION_COMMIT" \
WOL_ROOT="$ROOT_REMOTE" WOL_OS_RELEASE="$ROOT_REMOTE/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
WOL_GITHUB_API=https://api.test WOL_RAW_BASE=https://raw.test PATH="$MOCK_BIN:$PATH" \
"$PAYLOAD_REMOTE/install.sh" --non-interactive --yes --service cli
[[ $("$ROOT_REMOTE/usr/local/bin/wol" version) = 1.0.0-beta.2 ]] || fail "installation distante absente"

echo "[8/16] vérification main sans installation"
run_update "$ROOT12" main --check --non-interactive >"$WORK/check.log"
assert_contains "$WORK/check.log" "Mise à jour disponible"
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.1 ]] || fail "--check a modifié le binaire"

echo "[9/16] mise à jour main et préservation des données"
mkdir -p "$ROOT12/etc/wol-manager/secrets" "$ROOT12/var/lib/wol-manager" "$ROOT12/var/log/wol-manager"
cp "$REPO/configs/wol.example.yaml" "$ROOT12/etc/wol-manager/wol.yaml"
echo secret >"$ROOT12/etc/wol-manager/secrets/token"
echo state >"$ROOT12/var/lib/wol-manager/state.keep"
echo log >"$ROOT12/var/log/wol-manager/audit.keep"
run_update "$ROOT12" main --yes --non-interactive
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.2 ]] || fail "mise à jour main absente"
cmp -s "$REMOTE/main/install.sh" "$ROOT12/usr/local/lib/wol-manager/install.sh" || fail "l'installateur signé n'a pas été mis à jour"
assert_file "$ROOT12/etc/wol-manager/secrets/token"
assert_file "$ROOT12/var/lib/wol-manager/state.keep"
assert_file "$ROOT12/var/log/wol-manager/audit.keep"
assert_contains "$ROOT12/etc/wol-manager/release.conf" "default_channel=main"

echo "[10/16] absence de mise à jour"
run_update "$ROOT12" main --check --non-interactive >"$WORK/current.log"
assert_contains "$WORK/current.log" "est à jour"

echo "[11/16] canal dev temporaire"
run_update "$ROOT12" dev --yes --non-interactive >"$WORK/dev.log" 2>&1
assert_contains "$WORK/dev.log" "CANAL DEV/BETA"
[[ $("$ROOT12/usr/local/bin/wol" version) = 1.0.0-beta.3 ]] || fail "mise à jour dev absente"
assert_contains "$ROOT12/etc/wol-manager/release.conf" "default_channel=main"

echo "[transition canal] même version et même révision de dev vers main"
REMOTE_CHANNEL_SWITCH="$WORK/remote-channel-switch"
mkdir -p "$REMOTE_CHANNEL_SWITCH/main"
make_distribution "$REMOTE_CHANNEL_SWITCH/main" 1.0.0-beta.3 "$REV3" main
ROOT_CHANNEL="$WORK/root-channel"
cp -a "$ROOT12" "$ROOT_CHANNEL"

run_update_from "$REMOTE_CHANNEL_SWITCH" "$ROOT_CHANNEL" main --channel main --check --non-interactive >"$WORK/channel-switch-check.log"
assert_contains "$WORK/channel-switch-check.log" "Canal installé    : dev"
assert_contains "$WORK/channel-switch-check.log" "passage du canal dev vers main"
[[ $("$ROOT_CHANNEL/usr/local/bin/wol" version --channel) = dev ]] || fail "--check a modifié le canal installé"
assert_contains "$ROOT_CHANNEL/etc/wol-manager/release.conf" "installed_channel=dev"

run_update_from "$REMOTE_CHANNEL_SWITCH" "$ROOT_CHANNEL" main --channel main --yes --non-interactive >"$WORK/channel-switch-update.log"
[[ $("$ROOT_CHANNEL/usr/local/bin/wol" version --channel) = main ]] || fail "le binaire main n'a pas remplacé le binaire dev"
assert_contains "$ROOT_CHANNEL/etc/wol-manager/release.conf" "installed_channel=main"
assert_contains "$ROOT_CHANNEL/etc/wol-manager/release.conf" "default_channel=main"
assert_file "$ROOT_CHANNEL/etc/wol-manager/secrets/token"
assert_file "$ROOT_CHANNEL/var/lib/wol-manager/state.keep"
assert_file "$ROOT_CHANNEL/var/log/wol-manager/audit.keep"

echo "[protection transition] version dev et manifeste main signé de format 1"
run_update "$ROOT12" dev --check --non-interactive >"$WORK/dev-check.log"
assert_contains "$WORK/dev-check.log" "WoL Manager est à jour"

REMOTE_LEGACY="$WORK/remote-legacy"
mkdir -p "$REMOTE_LEGACY/main"
make_legacy_distribution "$REMOTE_LEGACY/main" 1.0.0-beta.2 "$REV2" main
ROOT12_BEFORE_LEGACY="$WORK/root12-before-legacy"
cp -a "$ROOT12" "$ROOT12_BEFORE_LEGACY"

run_update_from "$REMOTE_LEGACY" "$ROOT12" main --check --non-interactive >"$WORK/legacy-main-default.log"
assert_contains "$WORK/legacy-main-default.log" "Une version du canal dev est actuellement installée."
assert_contains "$WORK/legacy-main-default.log" 'wol update --check --channel dev'
assert_contains "$WORK/legacy-main-default.log" "Aucun changement n'a été effectué."

run_update_from "$REMOTE_LEGACY" "$ROOT12" main --channel main --check --non-interactive >"$WORK/legacy-main-explicit.log"
assert_contains "$WORK/legacy-main-explicit.log" "Une version du canal dev est actuellement installée."
diff -r "$ROOT12_BEFORE_LEGACY" "$ROOT12" >/dev/null || fail "le contrôle du manifeste main ancien a modifié l'installation dev"
assert_contains "$ROOT12/etc/wol-manager/release.conf" "default_channel=main"
assert_contains "$ROOT12/etc/wol-manager/release.conf" "installed_channel=dev"

if run_update_from "$REMOTE_LEGACY" "$ROOT_REMOTE" main --check --non-interactive >"$WORK/legacy-main-from-main.log" 2>&1; then
  fail "une installation main a accepté un manifeste de format 1 incompatible"
fi
assert_contains "$WORK/legacy-main-from-main.log" "Format de manifeste non pris en charge"

REMOTE_LEGACY_TAMPERED="$WORK/remote-legacy-tampered"
mkdir -p "$REMOTE_LEGACY_TAMPERED"
cp -a "$REMOTE_LEGACY/main" "$REMOTE_LEGACY_TAMPERED/main"
sed -i 's/beta.2/beta.9/' "$REMOTE_LEGACY_TAMPERED/main/manifest.json"
if run_update_from "$REMOTE_LEGACY_TAMPERED" "$ROOT12" main --check --non-interactive >"$WORK/legacy-main-tampered.log" 2>&1; then
  fail "un ancien manifeste altéré a déclenché le message de transition"
fi
assert_contains "$WORK/legacy-main-tampered.log" "Authenticité du manifeste non vérifiée"

REMOTE_UNKNOWN="$WORK/remote-unknown"
mkdir -p "$REMOTE_UNKNOWN/main"
make_legacy_distribution "$REMOTE_UNKNOWN/main" 9.9.9 "$REV2" main 99
if run_update_from "$REMOTE_UNKNOWN" "$ROOT12" main --check --non-interactive >"$WORK/unknown-format.log" 2>&1; then
  fail "un format de manifeste inconnu a été accepté"
fi
assert_contains "$WORK/unknown-format.log" "Format de manifeste non pris en charge"

echo "[12/16] manifeste altéré refusé"
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

echo "[complément] installateur distribué altéré refusé"
cp "$REMOTE/main/install.sh" "$WORK/install.sh.valid"
printf '\n# altération de test\n' >>"$REMOTE/main/install.sh"
if run_update "$ROOT12" main --check --non-interactive >"$WORK/tampered-installer.log" 2>&1; then
  fail "un installateur dont le SHA-256 est invalide a été accepté"
fi
assert_contains "$WORK/tampered-installer.log" "SHA-256 invalide pour install.sh"
mv "$WORK/install.sh.valid" "$REMOTE/main/install.sh"

echo "[13/16] rollback et état systemd"
ROOT_ROLL="$WORK/root-roll"
prepare_root "$ROOT_ROLL" 12
run_installer "$PAYLOAD" "$ROOT_ROLL" amd64 --non-interactive --yes --service install
printf 'ancienne complétion WoL Manager\n' >"$ROOT_ROLL/etc/bash_completion.d/wol-manager"
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
assert_contains "$ROOT_ROLL/etc/bash_completion.d/wol-manager" "ancienne complétion WoL Manager"
assert_contains "$ROOT_ROLL/usr/share/bash-completion/completions/wol" "_comp_cmd_wol"
assert_contains "$WORK/rollback.log" "Rollback terminé"

echo "[14/16] échec initial sans installation partielle"
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

echo "[15/16] première configuration avant activation systemd"
ROOT_ENABLE="$WORK/root-enable"
prepare_root "$ROOT_ENABLE" 13
WOL_TEST_SYSTEMD=1 run_installer "$PAYLOAD" "$ROOT_ENABLE" amd64 --non-interactive --yes --service enable --setup
assert_file "$ROOT_ENABLE/etc/wol-manager/wol.yaml"
assert_contains "$ROOT_ENABLE/etc/wol-manager/secrets/tokens.json" '"id":"admin"'
assert_file "$ROOT_ENABLE/systemd-state/enabled"
assert_file "$ROOT_ENABLE/systemd-state/active"
ENABLE_LOG=$(find "$ROOT_ENABLE/var/log/wol-manager" -type f -name 'install-*.log' -print -quit)
assert_file "$ENABLE_LOG"
assert_not_contains "$ENABLE_LOG" 'test-only'

echo "[16/16] configuration existante et jeton vide avant activation systemd"
ROOT_ENABLE_EXISTING="$WORK/root-enable-existing"
prepare_root "$ROOT_ENABLE_EXISTING" 13
mkdir -p "$ROOT_ENABLE_EXISTING/etc/wol-manager"
cp "$REPO/configs/wol.example.yaml" "$ROOT_ENABLE_EXISTING/etc/wol-manager/wol.yaml"
WOL_TEST_SYSTEMD=1 run_installer "$PAYLOAD" "$ROOT_ENABLE_EXISTING" amd64 --non-interactive --yes --service enable
assert_contains "$ROOT_ENABLE_EXISTING/etc/wol-manager/secrets/tokens.json" '"id":"admin"'
assert_file "$ROOT_ENABLE_EXISTING/systemd-state/enabled"
assert_file "$ROOT_ENABLE_EXISTING/systemd-state/active"
EXISTING_LOG=$(find "$ROOT_ENABLE_EXISTING/var/log/wol-manager" -type f -name 'install-*.log' -print -quit)
assert_file "$EXISTING_LOG"
assert_not_contains "$EXISTING_LOG" 'test-only'

echo "[complément] ancienne collision de complétion détectée sans modification"
ROOT_LEGACY="$WORK/root-legacy"
prepare_root "$ROOT_LEGACY" 12
printf '# bash completion for WoL Manager\n_wol() { :; }\ncomplete -F _wol wol\n' >"$ROOT_LEGACY/usr/share/bash-completion/completions/wol"
LEGACY_SHA=$(sha256sum "$ROOT_LEGACY/usr/share/bash-completion/completions/wol" | awk '{print $1}')
run_installer "$PAYLOAD" "$ROOT_LEGACY" amd64 --non-interactive --yes --service cli >"$WORK/legacy.log" 2>&1
assert_contains "$WORK/legacy.log" "restaurez-le si nécessaire"
[[ $(sha256sum "$ROOT_LEGACY/usr/share/bash-completion/completions/wol" | awk '{print $1}') = "$LEGACY_SHA" ]] || fail "l'ancienne collision a été modifiée automatiquement"
assert_file "$ROOT_LEGACY/etc/bash_completion.d/wol-manager"

echo "[complément] mise à jour Debian simulée et désinstallation ciblée"
printf '# fichier Debian mis à jour\n_comp_cmd_wol() { :; }\ncomplete -F _comp_cmd_wol wol\n' >"$ROOT_REMOTE/usr/share/bash-completion/completions/wol"
assert_file "$ROOT_REMOTE/etc/bash_completion.d/wol-manager"
APT_LOG="$WORK/apt.log" GO_LOG="$WORK/go.log" SYSTEMD_STATE="$ROOT_REMOTE/systemd-state" \
  WOL_ROOT="$ROOT_REMOTE" WOL_OS_RELEASE="$ROOT_REMOTE/os-release" WOL_ARCH=amd64 WOL_TEST_MODE=1 WOL_TEST_UNAME=Linux \
  PATH="$MOCK_BIN:$PATH" "$ROOT_REMOTE/usr/local/lib/wol-manager/install.sh" --uninstall --non-interactive
[[ ! -e "$ROOT_REMOTE/etc/bash_completion.d/wol-manager" ]] || fail "la complétion WoL Manager n'a pas été supprimée"
assert_contains "$ROOT_REMOTE/usr/share/bash-completion/completions/wol" "fichier Debian mis à jour"

echo "Tous les tests simulés de l'installateur ont réussi."
