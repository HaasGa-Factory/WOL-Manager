#!/usr/bin/env bash
set -Eeuo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
DIST="$ROOT/dist"
CHANNEL=""
SIGNING_KEY=${WOL_SIGNING_KEY:-"$ROOT/.keys/release-private.pem"}
PUBLIC_KEY="$ROOT/packaging/release-public.pem"

usage() {
  cat <<'EOF'
Usage: ./scripts/build.sh [--channel main|dev] [--signing-key FILE]

Compile et signe les binaires Linux AMD64 et ARM64 depuis un commit source
propre. Ce script ne publie rien et ne modifie aucune branche distante.
EOF
}

while (($#)); do
  case "$1" in
    --channel) CHANNEL=${2:-}; shift 2 ;;
    --signing-key) SIGNING_KEY=${2:-}; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Option inconnue: $1" >&2; usage >&2; exit 2 ;;
  esac
done

for command in go git sha256sum file openssl tar; do
  command -v "$command" >/dev/null 2>&1 || {
    echo "Prérequis développeur manquant: $command" >&2
    exit 1
  }
done

cd "$ROOT"
git rev-parse --is-inside-work-tree >/dev/null 2>&1 || {
  echo "Le projet doit être compilé depuis un dépôt Git." >&2
  exit 1
}

branch=$(git branch --show-current)
if [[ -z "$CHANNEL" ]]; then
  case "$branch" in
    main) CHANNEL=main ;;
    dev|develop) CHANNEL=dev ;;
    *) echo "Canal indéterminé pour la branche '$branch'; utilisez --channel." >&2; exit 1 ;;
  esac
fi
[[ "$CHANNEL" == main || "$CHANNEL" == dev ]] || {
  echo "Canal invalide: utilisez main ou dev." >&2
  exit 1
}

# dist/ est produit par ce script. Tout autre écart rendrait la provenance
# source ambiguë, car le manifeste référence le commit HEAD exact.
dirty=$(git status --porcelain --untracked-files=all | awk 'substr($0,4) !~ /^dist\// {print}')
if [[ -n "$dirty" ]]; then
  echo "Le dépôt source doit être propre avant compilation:" >&2
  echo "$dirty" >&2
  echo "Enregistrez d'abord le code source, puis relancez ce script." >&2
  exit 1
fi

[[ -r "$SIGNING_KEY" ]] || {
  echo "Clé privée de signature absente: $SIGNING_KEY" >&2
  echo "Définissez WOL_SIGNING_KEY ou utilisez --signing-key." >&2
  exit 1
}
[[ -r "$PUBLIC_KEY" ]] || {
  echo "Clé publique de distribution absente: $PUBLIC_KEY" >&2
  exit 1
}

mkdir -p "$DIST"
work=$(mktemp -d "$DIST/.build.XXXXXX")
trap 'rm -rf -- "$work"' EXIT

openssl pkey -in "$SIGNING_KEY" -pubout -outform DER -out "$work/derived-public.der" >/dev/null 2>&1
openssl pkey -pubin -in "$PUBLIC_KEY" -outform DER -out "$work/approved-public.der" >/dev/null 2>&1
cmp -s "$work/derived-public.der" "$work/approved-public.der" || {
  echo "La clé privée ne correspond pas à la clé publique approuvée." >&2
  exit 1
}

version=$(sed -n 's/^[[:space:]]*version[[:space:]]*=[[:space:]]*"\([^"]*\)".*/\1/p' cmd/wol/main.go | head -n1)
[[ -n "$version" ]] || { echo "Version du projet introuvable." >&2; exit 1; }
source_revision=$(git rev-parse HEAD)
export SOURCE_DATE_EPOCH
SOURCE_DATE_EPOCH=$(git show -s --format=%ct HEAD)

echo "Tests Go..."
source_tree="$work/source"
mkdir -p "$source_tree"
git archive HEAD -- cmd api go.mod go.sum | tar -xf - -C "$source_tree"
(
  cd "$source_tree"
  go mod verify
  go test ./...
)

ldflags="-s -w -X main.version=$version -X main.sourceRevision=$source_revision -X main.buildChannel=$CHANNEL"
for arch in amd64 arm64; do
  output="$work/wol-linux-$arch"
  echo "Compilation linux/$arch..."
  (
    cd "$source_tree"
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -buildvcs=false -ldflags="$ldflags" -o "$output" ./cmd/wol
  )
done

file "$work/wol-linux-amd64" | grep -Eq 'ELF 64-bit.*x86-64' || {
  echo "Le binaire AMD64 n'a pas le format attendu." >&2; exit 1;
}
file "$work/wol-linux-arm64" | grep -Eq 'ELF 64-bit.*(ARM aarch64|aarch64)' || {
  echo "Le binaire ARM64 n'a pas le format attendu." >&2; exit 1;
}

if [[ $(uname -s) == Linux && $(uname -m) == x86_64 ]]; then
  [[ $("$work/wol-linux-amd64" version) == "$version" ]]
  [[ $("$work/wol-linux-amd64" version --source-revision) == "$source_revision" ]]
  [[ $("$work/wol-linux-amd64" version --channel) == "$CHANNEL" ]]
else
  echo "Note: exécution AMD64 non testée sur cet hôte; format ELF seulement."
fi

amd64_sha=$(sha256sum "$work/wol-linux-amd64" | awk '{print $1}')
arm64_sha=$(sha256sum "$work/wol-linux-arm64" | awk '{print $1}')
install -m 0755 install.sh "$work/install.sh"
installer_sha=$(sha256sum "$work/install.sh" | awk '{print $1}')
cat >"$work/manifest.json" <<EOF
{
  "manifest_version": 2,
  "version": "$version",
  "channel": "$CHANNEL",
  "source_revision": "$source_revision",
  "key_id": "wol-manager-2026-01",
  "installer_file": "install.sh",
  "installer_sha256": "$installer_sha",
  "linux_amd64_file": "wol-linux-amd64",
  "linux_amd64_sha256": "$amd64_sha",
  "linux_arm64_file": "wol-linux-arm64",
  "linux_arm64_sha256": "$arm64_sha"
}
EOF

openssl pkeyutl -sign -rawin -inkey "$SIGNING_KEY" \
  -in "$work/manifest.json" -out "$work/manifest.sig.bin"
openssl base64 -A -in "$work/manifest.sig.bin" -out "$work/manifest.json.sig"
printf '\n' >>"$work/manifest.json.sig"
openssl pkeyutl -verify -rawin -pubin -inkey "$PUBLIC_KEY" \
  -in "$work/manifest.json" -sigfile "$work/manifest.sig.bin" >/dev/null

(
  cd "$work"
  sha256sum wol-linux-amd64 wol-linux-arm64 install.sh manifest.json manifest.json.sig >SHA256SUMS
)

for artifact in wol-linux-amd64 wol-linux-arm64 install.sh manifest.json manifest.json.sig SHA256SUMS; do
  install -m "$(case "$artifact" in wol-*|install.sh) echo 0755;; *) echo 0644;; esac)" \
    "$work/$artifact" "$DIST/$artifact"
done

echo "Distribution $version ($CHANNEL) créée dans dist/."
echo "Révision source: $source_revision"
echo "ARM64: format vérifié; exécution réelle à valider sur une machine ARM64."
