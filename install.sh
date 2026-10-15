#!/bin/sh
set -eu

VERSION="1.0.0-beta.1"
ACTION=install
CHECK_ONLY=0
CHANNEL=main
SERVICE_MODE=""
RUN_SETUP=0
NON_INTERACTIVE=0
ASSUME_YES=0
ALLOW_DOWNGRADE=0
REPOSITORY_OVERRIDE=${WOL_GITHUB_REPOSITORY:-}
REPOSITORY=${REPOSITORY_OVERRIDE:-HaasGa-Factory/WOL-Manager}
GITHUB_API=${WOL_GITHUB_API:-https://api.github.com}
RAW_BASE=${WOL_RAW_BASE:-https://raw.githubusercontent.com}
ROOT=${WOL_ROOT:-}
TEST_MODE=${WOL_TEST_MODE:-0}
TEST_SYSTEMD=${WOL_TEST_SYSTEMD:-0}
SCRIPT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
TMP_WORK=""
BACKUP_DIR=""
ROLLBACK_READY=0
PREVIOUS_ACTIVE=0
PREVIOUS_ENABLED=0
PUBLIC_KEY="$SCRIPT_DIR/packaging/release-public.pem"

path() { printf '%s%s' "$ROOT" "$1"; }
can_manage_systemd() { [ "$TEST_MODE" != 1 ] || [ "$TEST_SYSTEMD" = 1 ]; }
BIN=$(path /usr/local/bin/wol)
HELPER_DIR=$(path /usr/local/lib/wol-manager)
HELPER="$HELPER_DIR/install.sh"
UNIT=$(path /etc/systemd/system/wol-manager.service)
CONFIG=$(path /etc/wol-manager/wol.yaml)
RELEASE_CONFIG=$(path /etc/wol-manager/release.conf)
STATE_DIR=$(path /var/lib/wol-manager)
LOG_DIR=$(path /var/log/wol-manager)

usage() {
  cat <<EOF
WoL Manager $VERSION
Première installation : sudo ./install.sh
Mise à jour          : sudo wol update
Vérification         : wol update --check
Canal DEV            : sudo wol update --channel dev
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --update) ACTION=update ;;
    --check) CHECK_ONLY=1 ;;
    --channel) [ "$#" -ge 2 ] || { echo "--channel exige main ou dev" >&2; exit 2; }; CHANNEL=$2; shift ;;
    --service) [ "$#" -ge 2 ] || { echo "--service exige enable, install ou cli" >&2; exit 2; }; SERVICE_MODE=$2; shift ;;
    --setup) RUN_SETUP=1 ;;
    --non-interactive) NON_INTERACTIVE=1 ;;
    --yes) ASSUME_YES=1 ;;
    --allow-downgrade) ALLOW_DOWNGRADE=1 ;;
    --version) ACTION=version ;;
    --uninstall) ACTION=uninstall ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Option inconnue: $1" >&2; usage >&2; exit 2 ;;
  esac
  shift
done

case "$CHANNEL" in main|dev) ;; *) echo "Canal invalide: utilisez main ou dev" >&2; exit 2 ;; esac
case "$SERVICE_MODE" in ""|enable|install|cli) ;; *) echo "Mode service invalide" >&2; exit 2 ;; esac
if [ -z "$REPOSITORY_OVERRIDE" ] && [ -r "$RELEASE_CONFIG" ]; then
  saved_repository=$(sed -n 's/^repository=//p' "$RELEASE_CONFIG" | head -n1)
  [ -z "$saved_repository" ] || REPOSITORY=$saved_repository
fi
printf '%s\n' "$REPOSITORY" | grep -Eq '^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$' || { echo "Dépôt invalide" >&2; exit 2; }
if [ "$TEST_MODE" != 1 ]; then
  case "$GITHUB_API:$RAW_BASE" in https://*:https://*) ;; *) echo "HTTPS est obligatoire" >&2; exit 2 ;; esac
fi

timestamp=$(date -u +%Y%m%dT%H%M%SZ)
if [ "$CHECK_ONLY" -eq 1 ] || [ "$ACTION" = version ]; then
  LOG_FILE="${TMPDIR:-/tmp}/wol-manager-check-$timestamp.log"
else
  mkdir -p "$LOG_DIR"
  chmod 0750 "$LOG_DIR" 2>/dev/null || true
  LOG_FILE="$LOG_DIR/install-$timestamp.log"
fi
: >"$LOG_FILE"

log() { printf '%s %s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$*" | tee -a "$LOG_FILE"; }
warn() { log "AVERTISSEMENT: $*" >&2; }
die() { log "ERREUR: $*" >&2; exit 1; }
run() { log "+ $*"; "$@" >>"$LOG_FILE" 2>&1; }
confirm() {
  [ "$ASSUME_YES" -eq 1 ] && return 0
  [ "$NON_INTERACTIVE" -eq 0 ] || return 1
  printf '%s [y/N] ' "$1" >/dev/tty
  read answer </dev/tty || return 1
  case "$answer" in y|Y|yes|YES|oui|OUI|o|O) return 0 ;; *) return 1 ;; esac
}
cleanup() {
  status=$?
  if [ "$status" -ne 0 ] && [ "$ROLLBACK_READY" -eq 1 ]; then rollback || true; fi
  [ -z "$TMP_WORK" ] || rm -rf -- "$TMP_WORK"
  if [ "$status" -eq 0 ]; then log "Résultat final: succès"; else log "Résultat final: échec ($status)"; fi
  log "Journal: $LOG_FILE"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM

need_root() {
  [ "$TEST_MODE" = 1 ] || [ "$(id -u)" -eq 0 ] || die "Exécutez cette action avec sudo"
}

detect_system() {
  [ "${WOL_TEST_UNAME:-$(uname -s)}" = Linux ] || die "Linux Debian est requis"
  os_release=${WOL_OS_RELEASE:-$(path /etc/os-release)}
  [ -r "$os_release" ] || die "Impossible de lire $os_release"
  ID= VERSION_ID=
  . "$os_release"
  [ "${ID:-}" = debian ] || die "Debian 12 ou 13 est requis"
  case "${VERSION_ID:-}" in 12|13) ;; *) die "Debian ${VERSION_ID:-inconnue} non prise en charge" ;; esac
  raw_arch=${WOL_ARCH:-$(uname -m)}
  case "$raw_arch" in x86_64|amd64) ARCH=amd64 ;; aarch64|arm64) ARCH=arm64 ;; *) die "Architecture non prise en charge: $raw_arch" ;; esac
  command -v apt-get >/dev/null 2>&1 || die "APT est requis"
  command -v dpkg-query >/dev/null 2>&1 || die "dpkg-query est requis"
  if command -v systemctl >/dev/null 2>&1; then SYSTEMD=yes; else SYSTEMD=no; fi
  free_kb=$(df -Pk "${ROOT:-/}" 2>/dev/null | awk 'NR==2 {print $4}')
  [ -z "$free_kb" ] || [ "$free_kb" -ge 102400 ] || die "100 Mio d'espace libre sont requis"
  log "Système: Debian $VERSION_ID, linux/$ARCH, systemd $SYSTEMD"
}

add_missing() {
  case " $MISSING " in *" $1 "*) return ;; esac
  MISSING="$MISSING $1"
  REASONS="$REASONS\n  $1: $2"
}
detect_dependencies() {
  MISSING= REASONS=
  command -v ip >/dev/null 2>&1 || add_missing iproute2 "routes, interfaces et découverte réseau"
  command -v ping >/dev/null 2>&1 || add_missing iputils-ping "contrôles ICMP configurés"
  command -v getent >/dev/null 2>&1 || add_missing libc-bin "résolution et compte système"
  command -v useradd >/dev/null 2>&1 || add_missing passwd "création du compte système"
  command -v sha256sum >/dev/null 2>&1 || add_missing coreutils "contrôle SHA-256"
  command -v openssl >/dev/null 2>&1 || add_missing openssl "signature Ed25519 du manifeste"
  [ -r "$(path /etc/ssl/certs/ca-certificates.crt)" ] || add_missing ca-certificates "validation HTTPS"
  if [ "$ACTION" = update ] || [ ! -x "$SCRIPT_DIR/dist/wol-linux-${ARCH:-unknown}" ]; then
    command -v curl >/dev/null 2>&1 || add_missing curl "téléchargement HTTPS"
  fi
  if [ "$TEST_MODE" = 1 ] && [ -n "${WOL_TEST_MISSING_PACKAGE:-}" ] && [ ! -e "${WOL_TEST_DEP_MARKER:-/nonexistent}" ]; then
    add_missing "$WOL_TEST_MISSING_PACKAGE" "scénario de test"
  fi
  MISSING=$(printf '%s' "$MISSING" | sed 's/^ *//')
}
install_dependencies() {
  [ -z "$MISSING" ] && { log "Dépendances d'exécution: toutes présentes"; return; }
  log "Dépendances manquantes: $MISSING"
  printf '%b\n' "$REASONS" | tee -a "$LOG_FILE"
  [ "$CHECK_ONLY" -eq 0 ] || die "Installez les dépendances manquantes avant de revérifier"
  confirm "Installer uniquement ces paquets avec APT ?" || die "Installation des dépendances non autorisée"
  run apt-get install --no-install-recommends -y $MISSING || die "APT n'a pas pu installer les dépendances"
  detect_dependencies
  [ -z "$MISSING" ] || die "Dépendances toujours manquantes: $MISSING"
}
check_debian_updates() {
  command -v apt >/dev/null 2>&1 || { warn "Mises à jour Debian non vérifiées: apt absent"; return; }
  updates=$(apt list --upgradable 2>/dev/null | sed '1d' || true)
  count=$(printf '%s\n' "$updates" | sed '/^$/d' | wc -l | tr -d ' ')
  log "Mises à jour Debian disponibles (cache APT actuel): $count"
  [ "$count" -eq 0 ] || printf '%s\n' "$updates" | sed -n '1,30p' | tee -a "$LOG_FILE"
  log "Aucune mise à jour Debian n'a été installée"
}

installed_version() { if [ -x "$BIN" ]; then "$BIN" version 2>/dev/null || printf 'inconnue\n'; else printf 'absente\n'; fi; }
installed_revision() { if [ -x "$BIN" ]; then "$BIN" version --source-revision 2>/dev/null || printf 'unknown\n'; else printf 'absente\n'; fi; }
valid_sha() { printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{40}$'; }
valid_hash() { printf '%s\n' "$1" | grep -Eq '^[0-9a-f]{64}$'; }
manifest_value() { sed -n "s/^[[:space:]]*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$2" | head -n1; }
manifest_number() { sed -n "s/^[[:space:]]*\"$1\"[[:space:]]*:[[:space:]]*\([0-9][0-9]*\).*/\1/p" "$2" | head -n1; }
download() {
  curl -fL --proto '=https' --tlsv1.2 --connect-timeout 10 --max-time 120 -o "$2" "$1" >>"$LOG_FILE" 2>&1
}
verify_manifest() {
  manifest=$1 signature=$2
  [ -r "$PUBLIC_KEY" ] || die "Clé publique approuvée absente: $PUBLIC_KEY"
  openssl base64 -d -A -in "$signature" -out "$TMP_WORK/manifest.sig.bin" >>"$LOG_FILE" 2>&1 || die "Signature du manifeste illisible"
  openssl pkeyutl -verify -rawin -pubin -inkey "$PUBLIC_KEY" -in "$manifest" -sigfile "$TMP_WORK/manifest.sig.bin" >>"$LOG_FILE" 2>&1 || die "Authenticité du manifeste non vérifiée"
  [ "$(manifest_number manifest_version "$manifest")" = 1 ] || die "Format de manifeste non pris en charge"
  MANIFEST_VERSION=$(manifest_value version "$manifest")
  MANIFEST_CHANNEL=$(manifest_value channel "$manifest")
  SOURCE_REVISION=$(manifest_value source_revision "$manifest")
  KEY_ID=$(manifest_value key_id "$manifest")
  case "$ARCH" in
    amd64) CANDIDATE_NAME=$(manifest_value linux_amd64_file "$manifest"); CANDIDATE_HASH=$(manifest_value linux_amd64_sha256 "$manifest") ;;
    arm64) CANDIDATE_NAME=$(manifest_value linux_arm64_file "$manifest"); CANDIDATE_HASH=$(manifest_value linux_arm64_sha256 "$manifest") ;;
  esac
  [ "$MANIFEST_CHANNEL" = "$CHANNEL" ] || die "Le manifeste vise le canal $MANIFEST_CHANNEL, pas $CHANNEL"
  [ "$KEY_ID" = wol-manager-2026-01 ] || die "Clé de distribution inconnue: $KEY_ID"
  valid_sha "$SOURCE_REVISION" || die "Révision source invalide"
  valid_hash "$CANDIDATE_HASH" || die "SHA-256 du binaire invalide"
  [ "$CANDIDATE_NAME" = "wol-linux-$ARCH" ] || die "Nom de binaire inattendu: $CANDIDATE_NAME"
  printf '%s\n' "$MANIFEST_VERSION" | grep -Eq '^[0-9A-Za-z][0-9A-Za-z.+-]*$' || die "Version invalide dans le manifeste"
  log "Manifeste signé vérifié ($KEY_ID)"
}
verify_candidate() {
  actual=$(sha256sum "$CANDIDATE" | awk '{print $1}')
  [ "$actual" = "$CANDIDATE_HASH" ] || die "SHA-256 invalide pour $CANDIDATE_NAME"
  chmod 0755 "$CANDIDATE"
  CANDIDATE_VERSION=$("$CANDIDATE" version 2>/dev/null) || die "Le binaire ne s'exécute pas sur linux/$ARCH"
  CANDIDATE_REVISION=$("$CANDIDATE" version --source-revision 2>/dev/null) || die "Métadonnée source absente du binaire"
  CANDIDATE_CHANNEL=$("$CANDIDATE" version --channel 2>/dev/null) || die "Métadonnée canal absente du binaire"
  [ "$CANDIDATE_VERSION" = "$MANIFEST_VERSION" ] || die "Version du binaire différente du manifeste"
  [ "$CANDIDATE_REVISION" = "$SOURCE_REVISION" ] || die "Révision du binaire différente du manifeste"
  [ "$CANDIDATE_CHANNEL" = "$CHANNEL" ] || die "Canal du binaire différent du canal demandé"
  log "Binaire vérifié: $CANDIDATE_VERSION, source $CANDIDATE_REVISION, linux/$ARCH"
}
resolve_publication() {
  download "$GITHUB_API/repos/$REPOSITORY/commits/$CHANNEL" "$TMP_WORK/commit.json" || die "Impossible de résoudre la branche $CHANNEL sur GitHub"
  PUBLICATION_COMMIT=$(sed -n 's/^[[:space:]]*"sha"[[:space:]]*:[[:space:]]*"\([0-9a-f][0-9a-f]*\)".*/\1/p' "$TMP_WORK/commit.json" | head -n1)
  valid_sha "$PUBLICATION_COMMIT" || die "Commit de publication GitHub invalide"
  log "Canal $CHANNEL résolu vers le commit de publication $PUBLICATION_COMMIT"
}
prepare_candidate() {
  TMP_WORK=$(mktemp -d "${TMPDIR:-/tmp}/wol-manager.XXXXXX")
  if [ "$ACTION" = install ] && [ -x "$SCRIPT_DIR/dist/wol-linux-$ARCH" ]; then
    manifest="$SCRIPT_DIR/dist/manifest.json"
    signature="$SCRIPT_DIR/dist/manifest.json.sig"
    CANDIDATE="$SCRIPT_DIR/dist/wol-linux-$ARCH"
    PUBLICATION_COMMIT=local
    verify_manifest "$manifest" "$signature"
    log "Distribution locale sélectionnée"
  else
    resolve_publication
    base="$RAW_BASE/$REPOSITORY/$PUBLICATION_COMMIT/dist"
    manifest="$TMP_WORK/manifest.json"
    signature="$TMP_WORK/manifest.json.sig"
    download "$base/manifest.json" "$manifest" || die "Manifeste indisponible pour $CHANNEL"
    download "$base/manifest.json.sig" "$signature" || die "Signature indisponible pour $CHANNEL"
    verify_manifest "$manifest" "$signature"
    CANDIDATE="$TMP_WORK/$CANDIDATE_NAME"
    download "$base/$CANDIDATE_NAME" "$CANDIDATE" || die "Aucun binaire compatible linux/$ARCH n'est disponible"
  fi
  verify_candidate
}

version_lt() { [ "$1" = "$2" ] && return 1; [ "$(printf '%s\n%s\n' "$1" "$2" | sort -V | head -n1)" = "$1" ]; }
show_comparison() {
  current=$(installed_version)
  current_revision=$(installed_revision)
  log "Version installée : $current"
  log "Révision installée: $current_revision"
  log "Version disponible: $CANDIDATE_VERSION"
  log "Révision source   : $CANDIDATE_REVISION"
  log "Canal             : $CHANNEL"
  log "Architecture      : linux/$ARCH"
  [ "$CHANNEL" != dev ] || warn "CANAL DEV/BETA: cette version peut être instable; le canal par défaut reste main"
  if [ "$current" = "$CANDIDATE_VERSION" ] && [ "$current_revision" = "$CANDIDATE_REVISION" ]; then
    UPDATE_NEEDED=0
    log "WoL Manager est à jour"
  else
    UPDATE_NEEDED=1
    if [ "$current" = "$CANDIDATE_VERSION" ]; then
      log "Mise à jour disponible: même version, révision source différente (elle n'est pas présentée comme une version supérieure)"
    else
      log "Mise à jour disponible"
    fi
  fi
}

choose_service_mode() {
  [ -n "$SERVICE_MODE" ] && return
  if [ "$ACTION" = update ]; then
    if [ -e "$UNIT" ]; then SERVICE_MODE=install; else SERVICE_MODE=cli; fi
    return
  fi
  if [ "$SYSTEMD" = no ]; then SERVICE_MODE=cli; warn "systemd absent: installation CLI uniquement"; return; fi
  if [ "$NON_INTERACTIVE" -eq 1 ]; then SERVICE_MODE=install; return; fi
  printf 'Service systemd: 1 activer, 2 installer sans activer, 3 CLI seule [2]: ' >/dev/tty
  read choice </dev/tty || choice=2
  case "$choice" in 1) SERVICE_MODE=enable ;; 3) SERVICE_MODE=cli ;; *) SERVICE_MODE=install ;; esac
}
create_account_and_dirs() {
  if [ "$TEST_MODE" != 1 ]; then
    getent group wol-manager >/dev/null 2>&1 || groupadd --system wol-manager
    getent passwd wol-manager >/dev/null 2>&1 || useradd --system --gid wol-manager --home-dir /var/lib/wol-manager --shell /usr/sbin/nologin wol-manager
  fi
  install -d -m 0750 "$(path /etc/wol-manager)" "$(path /etc/wol-manager/secrets)" "$STATE_DIR" "$LOG_DIR"
  install -d -m 0755 "$(path /usr/local/bin)" "$HELPER_DIR" "$HELPER_DIR/configs" "$HELPER_DIR/packaging/systemd" "$(path /usr/share/doc/wol-manager)"
  [ "$SERVICE_MODE" = cli ] || install -d -m 0755 "$(path /etc/systemd/system)"
  if [ "$TEST_MODE" != 1 ]; then
    chown wol-manager:wol-manager "$STATE_DIR" "$LOG_DIR"
    chown root:wol-manager "$(path /etc/wol-manager)" "$(path /etc/wol-manager/secrets)"
  fi
}
backup_existing() {
  BACKUP_DIR="$STATE_DIR/backups/install-$timestamp"
  install -d -m 0750 "$BACKUP_DIR"
  [ ! -e "$BIN" ] || cp -p "$BIN" "$BACKUP_DIR/wol"
  [ ! -e "$UNIT" ] || cp -p "$UNIT" "$BACKUP_DIR/wol-manager.service"
  [ ! -e "$HELPER" ] || cp -p "$HELPER" "$BACKUP_DIR/install.sh"
  [ ! -e "$RELEASE_CONFIG" ] || cp -p "$RELEASE_CONFIG" "$BACKUP_DIR/release.conf"
  log "Sauvegarde créée: $BACKUP_DIR; configuration, secrets, données et journaux restent en place"
}
rollback() {
  ROLLBACK_READY=0
  warn "Vérification en échec: restauration de l'installation précédente"
  if [ -f "$BACKUP_DIR/wol" ]; then install -m 0755 "$BACKUP_DIR/wol" "$BIN"; else rm -f "$BIN"; fi
  if [ -f "$BACKUP_DIR/wol-manager.service" ]; then install -m 0644 "$BACKUP_DIR/wol-manager.service" "$UNIT"; else rm -f "$UNIT"; fi
  if [ -f "$BACKUP_DIR/install.sh" ]; then install -m 0755 "$BACKUP_DIR/install.sh" "$HELPER"; else rm -f "$HELPER"; fi
  if [ -f "$BACKUP_DIR/release.conf" ]; then install -m 0640 "$BACKUP_DIR/release.conf" "$RELEASE_CONFIG"; else rm -f "$RELEASE_CONFIG"; fi
  if [ ! -f "$BACKUP_DIR/install.sh" ]; then
    rm -rf -- "$HELPER_DIR"
    rm -f "$(path /usr/share/doc/wol-manager/wol.example.yaml)"
  fi
  if [ "$SYSTEMD" = yes ] && can_manage_systemd; then
    systemctl daemon-reload >>"$LOG_FILE" 2>&1 || true
    [ "$PREVIOUS_ENABLED" -eq 0 ] || systemctl enable wol-manager.service >>"$LOG_FILE" 2>&1 || true
    [ "$PREVIOUS_ACTIVE" -eq 0 ] || systemctl restart wol-manager.service >>"$LOG_FILE" 2>&1 || true
  fi
  log "Rollback terminé"
}
install_candidate() {
  current=$(installed_version)
  current_revision=$(installed_revision)
  [ "$ACTION" != install ] || [ "$current" = absente ] || die "WoL Manager est déjà installé; utilisez sudo wol update"
  if [ "$ACTION" = update ]; then
    show_comparison
    [ "$UPDATE_NEEDED" -eq 1 ] || return 0
    version_lt "$CANDIDATE_VERSION" "$current" && [ "$ALLOW_DOWNGRADE" -ne 1 ] && die "Rétrogradation $current vers $CANDIDATE_VERSION refusée"
    confirm "Installer cette mise à jour ?" || { log "Mise à jour annulée"; return 0; }
  fi
  [ ! -f "$CONFIG" ] || "$CANDIDATE" --config "$CONFIG" config check >>"$LOG_FILE" 2>&1 || die "Configuration incompatible avec $CANDIDATE_VERSION"
  if [ "$SYSTEMD" = yes ] && can_manage_systemd && [ -e "$UNIT" ]; then
    systemctl is-active --quiet wol-manager.service 2>/dev/null && PREVIOUS_ACTIVE=1 || true
    systemctl is-enabled --quiet wol-manager.service 2>/dev/null && PREVIOUS_ENABLED=1 || true
  fi
  choose_service_mode
  create_account_and_dirs
  backup_existing
  ROLLBACK_READY=1
  [ "$PREVIOUS_ACTIVE" -eq 0 ] || run systemctl stop wol-manager.service
  install -m 0755 "$CANDIDATE" "$BIN.new.$$"
  mv -f "$BIN.new.$$" "$BIN"
  if [ "$SCRIPT_DIR" != "$HELPER_DIR" ]; then
    install -m 0755 "$SCRIPT_DIR/install.sh" "$HELPER"
    install -m 0644 "$SCRIPT_DIR/packaging/release-public.pem" "$HELPER_DIR/packaging/release-public.pem"
    install -m 0644 "$SCRIPT_DIR/configs/wol.example.yaml" "$HELPER_DIR/configs/wol.example.yaml"
    install -m 0644 "$SCRIPT_DIR/packaging/systemd/wol-manager.service" "$HELPER_DIR/packaging/systemd/wol-manager.service"
  fi
  install -m 0644 "$SCRIPT_DIR/configs/wol.example.yaml" "$(path /usr/share/doc/wol-manager/wol.example.yaml)"
  [ "$SERVICE_MODE" = cli ] || install -m 0644 "$SCRIPT_DIR/packaging/systemd/wol-manager.service" "$UNIT"
  umask 027
  printf 'repository=%s\ndefault_channel=main\ninstalled_channel=%s\ninstalled_version=%s\nsource_revision=%s\npublication_commit=%s\n' \
    "$REPOSITORY" "$CHANNEL" "$CANDIDATE_VERSION" "$CANDIDATE_REVISION" "$PUBLICATION_COMMIT" >"$RELEASE_CONFIG"
  "$BIN" version >>"$LOG_FILE" 2>&1 || die "Le binaire installé ne démarre pas"
  [ ! -f "$CONFIG" ] || "$BIN" --config "$CONFIG" config check >>"$LOG_FILE" 2>&1 || die "Validation post-installation en échec"
  if [ "$SYSTEMD" = yes ] && can_manage_systemd; then
    run systemctl daemon-reload
    if [ "$ACTION" = update ]; then
      [ "$PREVIOUS_ENABLED" -eq 0 ] || run systemctl enable wol-manager.service
      [ "$PREVIOUS_ACTIVE" -eq 0 ] || run systemctl restart wol-manager.service
    else
      case "$SERVICE_MODE" in
        enable) [ ! -f "$CONFIG" ] || run systemctl enable --now wol-manager.service ;;
        install|cli) : ;;
      esac
    fi
    [ "$PREVIOUS_ACTIVE" -eq 0 ] || systemctl is-active --quiet wol-manager.service || die "Le service ne fonctionne pas après mise à jour"
  fi
  ROLLBACK_READY=0
  log "Installation atomique terminée: $CANDIDATE_VERSION ($CANDIDATE_REVISION)"
}
run_setup_if_requested() {
  if [ "$RUN_SETUP" -ne 1 ]; then
    [ "$NON_INTERACTIVE" -eq 0 ] || return 0
    [ ! -f "$CONFIG" ] || return 0
    confirm "Lancer wol setup maintenant ?" || return 0
  fi
  "$BIN" --config "$CONFIG" setup
  chmod 0640 "$CONFIG"
  [ "$TEST_MODE" = 1 ] || chown root:wol-manager "$CONFIG"
  if [ "$SERVICE_MODE" = enable ] && [ "$SYSTEMD" = yes ] && can_manage_systemd; then
    run systemctl enable --now wol-manager.service
    systemctl is-active --quiet wol-manager.service || die "Le service ne fonctionne pas après configuration"
  fi
}
uninstall() {
  need_root
  if [ "$SYSTEMD" = yes ]; then systemctl disable --now wol-manager.service >>"$LOG_FILE" 2>&1 || true; rm -f "$UNIT"; systemctl daemon-reload >>"$LOG_FILE" 2>&1 || true; fi
  rm -f "$BIN" "$RELEASE_CONFIG"
  rm -rf -- "$HELPER_DIR"
  log "Programme et service supprimés; configuration, secrets, données, journaux et sauvegardes conservés"
}

if [ "$ACTION" = version ]; then log "Installateur $VERSION; version installée: $(installed_version)"; exit 0; fi
detect_system
[ "$CHANNEL" != dev ] || warn "CANAL DEV/BETA: ce choix n'est jamais enregistré comme canal par défaut"
if [ "$ACTION" = uninstall ]; then uninstall; exit 0; fi
if [ "$ACTION" = install ] && [ -x "$BIN" ]; then die "WoL Manager est déjà installé; utilisez sudo wol update"; fi
detect_dependencies
install_dependencies
[ "$ACTION" != install ] || check_debian_updates
prepare_candidate
if [ "$CHECK_ONLY" -eq 1 ]; then show_comparison; exit 0; fi
need_root
install_candidate
[ "$ACTION" != install ] || run_setup_if_requested
