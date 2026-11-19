#!/usr/bin/env bash
set -Eeuo pipefail

REPO=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
SCRIPT="$REPO/scripts/migration/upgrade-network-validation.sh"
WORK=$(mktemp -d)
trap 'rm -rf -- "$WORK"' EXIT

fail() { printf 'ECHEC: %s\n' "$*" >&2; exit 1; }
assert_file() { [ -f "$1" ] || fail "fichier absent: $1"; }
assert_contains() { grep -Fq -- "$2" "$1" || fail "'$2' absent de $1"; }

PRIVATE_KEY="$WORK/release-private.pem"
PUBLIC_KEY="$WORK/release-public.pem"
openssl genpkey -algorithm Ed25519 -out "$PRIVATE_KEY" >/dev/null 2>&1
openssl pkey -in "$PRIVATE_KEY" -pubout -out "$PUBLIC_KEY" >/dev/null 2>&1

COMMIT=1111111111111111111111111111111111111111
REMOTE="$WORK/remote/$COMMIT/dist"
mkdir -p "$REMOTE"
cat >"$REMOTE/install.sh" <<'INSTALLER'
#!/usr/bin/env bash
if [ "${1:-}" = --version ]; then
  echo '2026-11-19T12:00:00Z Installateur 1.4.5; version installée: 1.3.0'
  sleep 0.05
  echo '2026-11-19T12:00:00Z Résultat final: succès'
  echo '2026-11-19T12:00:00Z Journal: /tmp/wol-manager-check.log'
  exit 0
fi
echo 'candidate installer'
INSTALLER
chmod 0755 "$REMOTE/install.sh"
sign_remote_manifest() {
  INSTALLER_SHA=$(sha256sum "$REMOTE/install.sh" | awk '{print $1}')
  cat >"$REMOTE/manifest.json" <<EOF
{
  "manifest_version": 2,
  "version": "1.4.5",
  "channel": "dev",
  "source_revision": "2222222222222222222222222222222222222222",
  "installer_sha256": "$INSTALLER_SHA"
}
EOF
  openssl pkeyutl -sign -rawin -inkey "$PRIVATE_KEY" -in "$REMOTE/manifest.json" \
    | openssl base64 -A >"$REMOTE/manifest.json.sig"
}
sign_remote_manifest

prepare_root() {
  local root=$1 version=${2:-1.3.0}
  mkdir -p "$root/usr/local/bin" "$root/usr/local/lib/wol-manager/packaging" \
    "$root/etc/wol-manager" "$root/var/lib/wol-manager" "$root/var/lib/wol-manager-installer/backups"
  cat >"$root/usr/local/bin/wol" <<EOF
#!/usr/bin/env bash
[ "\${1:-}" = version ] && { echo '$version'; exit 0; }
exit 1
EOF
  chmod 0755 "$root/usr/local/bin/wol"
  printf 'old installer\n' >"$root/usr/local/lib/wol-manager/install.sh"
  chmod 0755 "$root/usr/local/lib/wol-manager/install.sh"
  cp "$PUBLIC_KEY" "$root/usr/local/lib/wol-manager/packaging/release-public.pem"
  printf 'network: sentinel\n' >"$root/etc/wol-manager/wol.yaml"
  printf '{"state":"sentinel"}\n' >"$root/var/lib/wol-manager/state.json"
}

run_migration() {
  local root=$1
  shift
  WOL_MIGRATION_TEST_MODE=1 \
  WOL_MIGRATION_ROOT="$root" \
  WOL_MIGRATION_REMOTE_ROOT="$WORK/remote" \
  WOL_MIGRATION_PUBLICATION_COMMIT="$COMMIT" \
    "$SCRIPT" --channel dev "$@"
}

echo '[migration] remplacement authentifié depuis 1.3.0'
ROOT="$WORK/root"
prepare_root "$ROOT"
cp "$ROOT/etc/wol-manager/wol.yaml" "$WORK/config.before"
cp "$ROOT/var/lib/wol-manager/state.json" "$WORK/state.before"
cp "$ROOT/usr/local/bin/wol" "$WORK/binary.before"
run_migration "$ROOT" >"$WORK/migration.log"
assert_contains "$WORK/migration.log" 'Installateur officiel vérifié'
assert_contains "$ROOT/usr/local/lib/wol-manager/install.sh" 'candidate installer'
cmp -s "$WORK/config.before" "$ROOT/etc/wol-manager/wol.yaml" || fail 'configuration modifiée'
cmp -s "$WORK/state.before" "$ROOT/var/lib/wol-manager/state.json" || fail 'état modifié'
cmp -s "$WORK/binary.before" "$ROOT/usr/local/bin/wol" || fail 'binaire modifié'
BACKUP=$(find "$ROOT/var/lib/wol-manager-installer/backups" -type f -name install.sh -print -quit)
assert_file "$BACKUP"
assert_contains "$BACKUP" 'old installer'

echo '[migration] idempotence'
BACKUP_COUNT=$(find "$ROOT/var/lib/wol-manager-installer/backups" -type f -name install.sh | wc -l)
run_migration "$ROOT" >"$WORK/idempotent.log"
[[ $(find "$ROOT/var/lib/wol-manager-installer/backups" -type f -name install.sh | wc -l) -eq $BACKUP_COUNT ]] \
  || fail 'une sauvegarde inutile a été créée au second passage'
assert_contains "$WORK/idempotent.log" 'déjà installé'

for LEGACY_VERSION in 1.2.1 1.2.3; do
  echo "[migration] simulation sans modification depuis $LEGACY_VERSION"
  DRY="$WORK/dry-$LEGACY_VERSION"
  prepare_root "$DRY" "$LEGACY_VERSION"
  run_migration "$DRY" --dry-run >"$WORK/dry-$LEGACY_VERSION.log"
  assert_contains "$DRY/usr/local/lib/wol-manager/install.sh" 'old installer'
  assert_contains "$WORK/dry-$LEGACY_VERSION.log" 'Simulation terminée'
done

echo '[migration] version hors périmètre refusée sans modification'
UNSUPPORTED="$WORK/unsupported"
prepare_root "$UNSUPPORTED" 1.4.1
if run_migration "$UNSUPPORTED" >"$WORK/unsupported.log" 2>&1; then
  fail 'version 1.4.1 acceptée par un bootstrap inutile'
fi
assert_contains "$UNSUPPORTED/usr/local/lib/wol-manager/install.sh" 'old installer'
assert_contains "$WORK/unsupported.log" 'version installée non prise en charge'

echo '[migration] canal signé incohérent refusé'
WRONG_CHANNEL="$WORK/wrong-channel"
prepare_root "$WRONG_CHANNEL" 1.3.0
if WOL_MIGRATION_TEST_MODE=1 \
  WOL_MIGRATION_ROOT="$WRONG_CHANNEL" \
  WOL_MIGRATION_REMOTE_ROOT="$WORK/remote" \
  WOL_MIGRATION_PUBLICATION_COMMIT="$COMMIT" \
    "$SCRIPT" --channel main >"$WORK/wrong-channel.log" 2>&1; then
  fail 'manifeste dev accepté pour le canal main'
fi
assert_contains "$WRONG_CHANNEL/usr/local/lib/wol-manager/install.sh" 'old installer'
assert_contains "$WORK/wrong-channel.log" 'canal du manifeste incohérent'

echo '[migration] rollback si le nouvel installateur est inutilisable'
cp "$REMOTE/install.sh" "$WORK/good-installer.sh"
cat >"$REMOTE/install.sh" <<'BAD_INSTALLER'
#!/usr/bin/env bash
[ "${1:-}" = --version ] && { echo 'installateur inutilisable'; exit 0; }
exit 1
BAD_INSTALLER
chmod 0755 "$REMOTE/install.sh"
sign_remote_manifest
ROLLBACK="$WORK/rollback"
prepare_root "$ROLLBACK" 1.3.0
if run_migration "$ROLLBACK" >"$WORK/rollback.log" 2>&1; then
  fail 'installateur inutilisable accepté'
fi
assert_contains "$ROLLBACK/usr/local/lib/wol-manager/install.sh" 'old installer'
assert_contains "$WORK/rollback.log" 'sauvegarde restaurée'
cp "$WORK/good-installer.sh" "$REMOTE/install.sh"
chmod 0755 "$REMOTE/install.sh"
sign_remote_manifest

echo '[migration] installateur altéré refusé'
TAMPERED="$WORK/tampered"
prepare_root "$TAMPERED"
printf '# altered\n' >>"$REMOTE/install.sh"
if run_migration "$TAMPERED" >"$WORK/tampered.log" 2>&1; then
  fail 'installateur altéré accepté'
fi
assert_contains "$TAMPERED/usr/local/lib/wol-manager/install.sh" 'old installer'
assert_contains "$WORK/tampered.log" 'SHA-256'

echo '[migration] manifeste signé puis altéré refusé'
BAD_SIGNATURE="$WORK/bad-signature"
prepare_root "$BAD_SIGNATURE"
printf ' \n' >>"$REMOTE/manifest.json"
if run_migration "$BAD_SIGNATURE" >"$WORK/bad-signature.log" 2>&1; then
  fail 'manifeste altéré accepté'
fi
assert_contains "$BAD_SIGNATURE/usr/local/lib/wol-manager/install.sh" 'old installer'
assert_contains "$WORK/bad-signature.log" 'signature Ed25519'

echo 'Tous les tests du bootstrap de migration ont réussi.'
