#!/usr/bin/env bash
set -Eeuo pipefail
umask 077

REPOSITORY="HaasGa-Factory/WOL-Manager"
MINIMUM_INSTALLER_VERSION="1.4.5"
CHANNEL=dev
DRY_RUN=0
TEST_MODE=${WOL_MIGRATION_TEST_MODE:-0}
ROOT=${WOL_MIGRATION_ROOT:-}
PUBLICATION_COMMIT=${WOL_MIGRATION_PUBLICATION_COMMIT:-}
REMOTE_ROOT=${WOL_MIGRATION_REMOTE_ROOT:-}

usage() {
  cat <<'EOF'
Usage: sudo ./scripts/migration/upgrade-network-validation.sh [OPTIONS]

Remplace uniquement l'ancien installateur système après vérification du
manifeste officiel, de sa signature Ed25519 et du SHA-256 de l'installateur.
La configuration, les secrets, les états, le binaire et les services ne sont
pas modifiés. Exécutez ensuite la commande wol update habituelle.

Options:
  --channel main|dev   Canal à amorcer (défaut: dev)
  --dry-run            Vérifier sans remplacer l'installateur
  --help, -h           Afficher cette aide
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --channel)
      [ "$#" -ge 2 ] || { echo "ERREUR: valeur manquante pour --channel" >&2; exit 2; }
      CHANNEL=$2
      shift 2
      ;;
    --dry-run) DRY_RUN=1; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "ERREUR: option inconnue: $1" >&2; usage >&2; exit 2 ;;
  esac
done

[ "$CHANNEL" = main ] || [ "$CHANNEL" = dev ] || {
  echo "ERREUR: canal invalide; utilisez main ou dev" >&2
  exit 2
}
if [ "$TEST_MODE" != 1 ] && [ "$(id -u)" -ne 0 ]; then
  echo "ERREUR: exécutez ce script avec sudo" >&2
  exit 1
fi

path() { printf '%s%s' "$ROOT" "$1"; }
need() { command -v "$1" >/dev/null 2>&1 || { echo "ERREUR: commande requise absente: $1" >&2; exit 1; }; }
for command in bash cp curl date grep head id install mktemp mv openssl rm sed sha256sum sort stat tr awk; do
  need "$command"
done

BIN=$(path /usr/local/bin/wol)
INSTALLER=$(path /usr/local/lib/wol-manager/install.sh)
PUBLIC_KEY=$(path /usr/local/lib/wol-manager/packaging/release-public.pem)
BACKUP_ROOT=$(path /var/lib/wol-manager-installer/backups)

[ -x "$BIN" ] || { echo "ERREUR: binaire WoL Manager installé introuvable: $BIN" >&2; exit 1; }
[ -f "$PUBLIC_KEY" ] && [ ! -L "$PUBLIC_KEY" ] || {
  echo "ERREUR: clé publique officielle installée introuvable ou non fiable: $PUBLIC_KEY" >&2
  exit 1
}
if [ "$TEST_MODE" != 1 ]; then
  PUBLIC_KEY_UID=$(stat -c '%u' "$PUBLIC_KEY")
  PUBLIC_KEY_MODE=$(stat -c '%a' "$PUBLIC_KEY")
  PUBLIC_KEY_MODE_DEC=$((8#$PUBLIC_KEY_MODE))
  if [ "$PUBLIC_KEY_UID" -ne 0 ] || ((PUBLIC_KEY_MODE_DEC & 0022)); then
    echo "ERREUR: clé publique officielle non protégée: propriétaire root et écriture groupe/autres refusée requis" >&2
    exit 1
  fi
fi
if [ -e "$INSTALLER" ] && { [ -L "$INSTALLER" ] || [ ! -f "$INSTALLER" ]; }; then
  echo "ERREUR: installateur système non fiable: $INSTALLER" >&2
  exit 1
fi

INSTALLED_VERSION=$($BIN version 2>/dev/null | head -n1 | tr -d '\r')
case "$INSTALLED_VERSION" in
  1.2.1|1.2.3|1.3.0) ;;
  *)
    echo "ERREUR: version installée non prise en charge par ce bootstrap: ${INSTALLED_VERSION:-inconnue}" >&2
    echo "Utilisez la procédure de mise à jour correspondant à votre version." >&2
    exit 1
    ;;
esac

WORK=$(mktemp -d)
cleanup() { rm -rf -- "$WORK"; }
trap cleanup EXIT

json_string() {
  local key=$1 file=$2
  sed -n "s/^[[:space:]]*\"$key\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$file" | head -n1
}
json_number() {
  local key=$1 file=$2
  sed -n "s/^[[:space:]]*\"$key\"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p" "$file" | head -n1
}
download() {
  local name=$1 destination=$2
  if [ "$TEST_MODE" = 1 ] && [ -n "$REMOTE_ROOT" ]; then
    cp "$REMOTE_ROOT/$PUBLICATION_COMMIT/dist/$name" "$destination"
  else
    curl --fail --silent --show-error --location \
      "https://raw.githubusercontent.com/$REPOSITORY/$PUBLICATION_COMMIT/dist/$name" \
      --output "$destination"
  fi
}

if [ -z "$PUBLICATION_COMMIT" ]; then
  curl --fail --silent --show-error --location \
    "https://api.github.com/repos/$REPOSITORY/commits/$CHANNEL" --output "$WORK/commit.json"
  PUBLICATION_COMMIT=$(json_string sha "$WORK/commit.json")
fi
case "$PUBLICATION_COMMIT" in
  [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
  *) echo "ERREUR: commit de publication invalide" >&2; exit 1 ;;
esac

download manifest.json "$WORK/manifest.json"
download manifest.json.sig "$WORK/manifest.json.sig"
download install.sh "$WORK/install.sh"

openssl base64 -d -A -in "$WORK/manifest.json.sig" -out "$WORK/manifest.sig" 2>/dev/null || {
  echo "ERREUR: signature du manifeste illisible" >&2
  exit 1
}
openssl pkeyutl -verify -rawin -pubin -inkey "$PUBLIC_KEY" \
  -in "$WORK/manifest.json" -sigfile "$WORK/manifest.sig" >/dev/null 2>&1 || {
  echo "ERREUR: signature Ed25519 du manifeste invalide" >&2
  exit 1
}

MANIFEST_VERSION=$(json_number manifest_version "$WORK/manifest.json")
CANDIDATE_VERSION=$(json_string version "$WORK/manifest.json")
CANDIDATE_CHANNEL=$(json_string channel "$WORK/manifest.json")
CANDIDATE_REVISION=$(json_string source_revision "$WORK/manifest.json")
EXPECTED_SHA=$(json_string installer_sha256 "$WORK/manifest.json")
[ "$MANIFEST_VERSION" = 2 ] || { echo "ERREUR: format de manifeste non pris en charge" >&2; exit 1; }
[ "$CANDIDATE_CHANNEL" = "$CHANNEL" ] || { echo "ERREUR: canal du manifeste incohérent" >&2; exit 1; }
case "$CANDIDATE_REVISION" in
  [0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
  *) echo "ERREUR: révision source invalide" >&2; exit 1 ;;
esac
[ -n "$CANDIDATE_VERSION" ] || { echo "ERREUR: version candidate absente" >&2; exit 1; }
[ "$(printf '%s\n%s\n' "$MINIMUM_INSTALLER_VERSION" "$CANDIDATE_VERSION" | sort -V | head -n1)" = "$MINIMUM_INSTALLER_VERSION" ] || {
  echo "ERREUR: installateur candidat trop ancien: $CANDIDATE_VERSION" >&2
  exit 1
}
printf '%s\n' "$EXPECTED_SHA" | grep -Eq '^[0-9a-f]{64}$' || {
  echo "ERREUR: SHA-256 de l'installateur absent ou invalide" >&2
  exit 1
}
ACTUAL_SHA=$(sha256sum "$WORK/install.sh" | awk '{print $1}')
[ "$ACTUAL_SHA" = "$EXPECTED_SHA" ] || { echo "ERREUR: SHA-256 de l'installateur incorrect" >&2; exit 1; }
bash -n "$WORK/install.sh" || { echo "ERREUR: syntaxe de l'installateur invalide" >&2; exit 1; }

echo "Installateur officiel vérifié: $CANDIDATE_VERSION ($CANDIDATE_CHANNEL), source $CANDIDATE_REVISION"
if [ -f "$INSTALLER" ] && [ "$(sha256sum "$INSTALLER" | awk '{print $1}')" = "$ACTUAL_SHA" ]; then
  echo "L'installateur vérifié est déjà installé; aucune modification nécessaire."
  exit 0
fi
if [ "$DRY_RUN" = 1 ]; then
  echo "Simulation terminée: l'installateur système serait remplacé; aucun fichier n'a été modifié."
  exit 0
fi

STAMP=$(date -u +%Y%m%dT%H%M%SZ)
BACKUP_DIR="$BACKUP_ROOT/bootstrap-network-validation-$STAMP.$$"
if [ "$TEST_MODE" = 1 ]; then
  install -d -m 0700 "$BACKUP_DIR"
  [ ! -f "$INSTALLER" ] || install -m 0755 "$INSTALLER" "$BACKUP_DIR/install.sh"
  install -m 0755 "$WORK/install.sh" "$INSTALLER.new.$$"
else
  install -d -o root -g root -m 0700 "$BACKUP_DIR"
  [ ! -f "$INSTALLER" ] || install -o root -g root -m 0755 "$INSTALLER" "$BACKUP_DIR/install.sh"
  install -o root -g root -m 0755 "$WORK/install.sh" "$INSTALLER.new.$$"
fi
mv -f "$INSTALLER.new.$$" "$INSTALLER"
INSTALLER_CHECK_LOG="$WORK/installed-version.log"
if [ "$(sha256sum "$INSTALLER" | awk '{print $1}')" != "$ACTUAL_SHA" ] || \
  ! "$INSTALLER" --version >"$INSTALLER_CHECK_LOG" 2>&1 || \
  ! grep -Fq "$CANDIDATE_VERSION" "$INSTALLER_CHECK_LOG"; then
  if [ -f "$BACKUP_DIR/install.sh" ]; then
    if [ "$TEST_MODE" = 1 ]; then
      install -m 0755 "$BACKUP_DIR/install.sh" "$INSTALLER"
    else
      install -o root -g root -m 0755 "$BACKUP_DIR/install.sh" "$INSTALLER"
    fi
  fi
  echo "ERREUR: nouvel installateur inutilisable; sauvegarde restaurée" >&2
  exit 1
fi

echo "Installateur système remplacé atomiquement. Sauvegarde: $BACKUP_DIR"
echo "La configuration, les secrets, les états, le binaire et les services sont inchangés."
echo "Étape suivante: sudo wol update --channel $CHANNEL"
