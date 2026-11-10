package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

type backupFixture struct {
	dir        string
	configPath string
	tokensPath string
	statePath  string
}

func newBackupFixture(t *testing.T) backupFixture {
	t.Helper()
	dir := t.TempDir()
	fixture := backupFixture{
		dir:        dir,
		configPath: filepath.Join(dir, "wol.yaml"),
		tokensPath: filepath.Join(dir, "secrets", "tokens.json"),
		statePath:  filepath.Join(dir, "state", "state.json"),
	}
	cfg := testConfig(t)
	cfg.API.TokensFile = fixture.tokensPath
	cfg.Storage.StateFile = fixture.statePath
	cfg.Storage.AuditFile = filepath.Join(dir, "audit.jsonl")
	writeBackupTestConfig(t, fixture.configPath, cfg)
	writeBackupTestJSON(t, fixture.tokensPath, tokenFile{Tokens: []TokenRecord{{ID: "admin", Hash: "secret-hash-must-not-leak", Permissions: []string{"read", "admin"}}}}, 0600)
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	writeBackupTestJSON(t, fixture.statePath, jobFile{Jobs: map[string]*Job{
		"queued": {ID: "queued", Kind: "wake", Target: "one", State: "queued", Created: now, Updated: now},
		"done":   {ID: "done", Kind: "wake", Target: "two", State: "completed", Created: now, Updated: now},
	}}, 0640)
	return fixture
}

func writeBackupTestConfig(t *testing.T, path string, cfg *Config) {
	t.Helper()
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0640); err != nil {
		t.Fatal(err)
	}
}

func writeBackupTestJSON(t *testing.T, path string, value any, mode os.FileMode) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), mode); err != nil {
		t.Fatal(err)
	}
}

func TestBackupCreateDefaultsToConfigurationOnly(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "default.wolbackup")})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readBackupArchive(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Manifest.Level != "configuration" || strings.Join(result.Manifest.Sections, ",") != "config" {
		t.Fatalf("niveau par défaut inattendu: %#v", result.Manifest)
	}
	if len(archive.Files) != 1 || archive.Files[backupConfigPath] == nil {
		t.Fatalf("la sauvegarde par défaut doit contenir uniquement la configuration: %#v", archive.Files)
	}
	if info, err := os.Stat(result.Path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("permissions de l'archive: info=%v err=%v", info, err)
	}
	if got := defaultBackupDir("/etc/wol-manager/wol.yaml"); got != "/var/backups/wol-manager" {
		t.Fatalf("répertoire système non isolé du service: %s", got)
	}
}

func TestBackupCreateWithExplicitFileDoesNotChangeParentPermissions(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("les modes Unix ne sont pas représentatifs sous Windows")
	}
	fixture := newBackupFixture(t)
	parent := filepath.Join(fixture.dir, "shared-parent")
	if err := os.Mkdir(parent, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(parent, "explicit.wolbackup")}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(parent)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0755 {
		t.Fatalf("le répertoire parent a été modifié: mode=%04o", info.Mode().Perm())
	}
}

func TestBackupCreateFullIncludesProtectedDataWithoutLeakingItOnInspect(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "full.wolbackup"), Full: true})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readBackupArchive(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Files[backupTokensPath] == nil || archive.Files[backupStatePath] == nil {
		t.Fatalf("sections protégées absentes: %v", result.Manifest.Sections)
	}
	var output bytes.Buffer
	if err := backupCLIWithIO(fixture.configPath, []string{"inspect", result.Path}, false, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "secret-hash-must-not-leak") || strings.Contains(output.String(), "queued") {
		t.Fatalf("inspect a révélé un contenu protégé:\n%s", output.String())
	}
}

func TestBackupCreateWithTokensDoesNotIncludeState(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "tokens.wolbackup"), WithTokens: true})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readBackupArchive(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	if archive.Files[backupTokensPath] == nil || archive.Files[backupStatePath] != nil {
		t.Fatalf("niveau configuration+tokens incorrect: %v", result.Manifest.Sections)
	}
}

func TestBackupVerifyRejectsChecksumTamperingAndUnsafeEntries(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "valid.wolbackup")})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readBackupArchive(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	archive.Files[backupConfigPath] = append(archive.Files[backupConfigPath], []byte("\n# altered")...)
	tampered := filepath.Join(fixture.dir, "tampered.wolbackup")
	if err := writeBackupArchive(tampered, archive.Manifest, archive.Files); err != nil {
		t.Fatal(err)
	}
	if _, err := readBackupArchive(tampered); err == nil || !strings.Contains(err.Error(), "incohérente") && !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("altération non refusée: %v", err)
	}

	unsafe := filepath.Join(fixture.dir, "unsafe.wolbackup")
	writeUnsafeBackupArchive(t, unsafe)
	if _, err := readBackupArchive(unsafe); err == nil || !strings.Contains(err.Error(), "entrée d'archive refusée") {
		t.Fatalf("chemin traversant non refusé: %v", err)
	}
}

func TestBackupVerifyRejectsManifestSectionsThatDoNotMatchFiles(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "sections-valid.wolbackup"), WithTokens: true})
	if err != nil {
		t.Fatal(err)
	}
	archive, err := readBackupArchive(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	archive.Manifest.Sections = []string{"config"}
	inconsistent := filepath.Join(fixture.dir, "sections-inconsistent.wolbackup")
	if err := writeBackupArchive(inconsistent, archive.Manifest, archive.Files); err != nil {
		t.Fatal(err)
	}
	if _, err := readBackupArchive(inconsistent); err == nil || !strings.Contains(err.Error(), "sections") {
		t.Fatalf("sections incohérentes non refusées: %v", err)
	}
}

func writeUnsafeBackupArchive(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	tw := tar.NewWriter(gz)
	data := []byte("unsafe")
	if err := tw.WriteHeader(&tar.Header{Name: "../wol.yaml", Mode: 0600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBackupRestoreDryRunDoesNotModifyFiles(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "restore.wolbackup"), Full: true})
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte("# current changed\n"), original...)
	if err := os.WriteFile(fixture.configPath, changed, 0640); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := backupCLIWithIO(fixture.configPath, []string{"restore", "--sections", "all", "--dry-run", result.Path}, false, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(fixture.configPath)
	if !bytes.Equal(after, changed) || !strings.Contains(output.String(), "aucun fichier n'a été modifié") {
		t.Fatalf("le dry-run a modifié la configuration ou n'a pas été explicite")
	}
}

func TestBackupRestoreConfigurationCanRecoverAMissingConfiguration(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "recovery.wolbackup")})
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.configPath); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := backupCLIWithIO(fixture.configPath, []string{"restore", "--yes", result.Path}, false, strings.NewReader(""), &output); err != nil {
		t.Fatalf("restauration de secours refusée: %v", err)
	}
	actual, err := os.ReadFile(fixture.configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(actual, expected) {
		t.Fatal("la configuration restaurée diffère de la sauvegarde")
	}
	if strings.Contains(output.String(), "Sauvegarde de sécurité") {
		t.Fatalf("une sauvegarde de sécurité inexistante a été annoncée:\n%s", output.String())
	}
}

func TestBackupRestoreFullRevalidatesActiveJobsAndCreatesSafetyBackup(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "restore-full.wolbackup"), Full: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixture.tokensPath, []byte("{\"tokens\":[]}"), 0600); err != nil {
		t.Fatal(err)
	}
	writeBackupTestJSON(t, fixture.statePath, jobFile{Jobs: map[string]*Job{}}, 0640)

	oldServiceCheck := backupServiceIsActive
	backupServiceIsActive = func() bool { return false }
	t.Cleanup(func() { backupServiceIsActive = oldServiceCheck })
	var output bytes.Buffer
	if err := backupCLIWithIO(fixture.configPath, []string{"restore", "--sections", "all", "--yes", result.Path}, false, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	var restored jobFile
	data, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Jobs["queued"].State != "revalidation_required" || restored.Jobs["done"].State != "completed" {
		t.Fatalf("états restaurés incorrects: %#v", restored.Jobs)
	}
	if !strings.Contains(output.String(), "Sauvegarde de sécurité") || !strings.Contains(output.String(), "Jobs suspendus") {
		t.Fatalf("rapport incomplet:\n%s", output.String())
	}
	items, err := listBackups(defaultBackupDir(fixture.configPath))
	if err != nil || len(items) == 0 {
		t.Fatalf("sauvegarde de sécurité absente: items=%v err=%v", items, err)
	}
}

func TestBackupRestoreStateRefusesWhileServiceIsActive(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "state-active.wolbackup"), Full: true})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(fixture.statePath)
	oldServiceCheck := backupServiceIsActive
	backupServiceIsActive = func() bool { return true }
	t.Cleanup(func() { backupServiceIsActive = oldServiceCheck })
	err = backupCLIWithIO(fixture.configPath, []string{"restore", "--sections", "state", "--yes", result.Path}, false, strings.NewReader(""), &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "service est actif") {
		t.Fatalf("service actif non bloqué: %v", err)
	}
	after, _ := os.ReadFile(fixture.statePath)
	if !bytes.Equal(before, after) {
		t.Fatal("l'état a changé malgré le refus")
	}
}

func TestBackupRestoreRejectsArchiveWithOverlyBroadPermissions(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("les modes Unix ne sont pas représentatifs sous Windows")
	}
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "broad.wolbackup")})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(result.Path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := prepareRestore(fixture.configPath, result.Path, "config", true); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("archive trop ouverte non refusée: %v", err)
	}
}

func TestBackupRestoreRollsBackEarlierSectionsWhenALaterWriteFails(t *testing.T) {
	fixture := newBackupFixture(t)
	result, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: filepath.Join(fixture.dir, "rollback.wolbackup"), WithTokens: true})
	if err != nil {
		t.Fatal(err)
	}
	currentTokens := []byte("{\"tokens\":[]}\n")
	if err := os.WriteFile(fixture.tokensPath, currentTokens, 0600); err != nil {
		t.Fatal(err)
	}
	archive, err := readBackupArchive(result.Path)
	if err != nil {
		t.Fatal(err)
	}
	report := backupRestoreReport{Archive: result.Path, Sections: []string{"tokens", "config"}, Differences: map[string]string{}}
	originalRestore := backupAtomicRestoreFile
	failedConfig := false
	backupAtomicRestoreFile = func(path string, data []byte, mode os.FileMode, secure func(string) error) error {
		if path == fixture.configPath && !failedConfig {
			failedConfig = true
			return os.ErrPermission
		}
		return originalRestore(path, data, mode, secure)
	}
	t.Cleanup(func() { backupAtomicRestoreFile = originalRestore })
	if _, err := applyRestore(fixture.configPath, archive, report.Sections, report); err == nil || !strings.Contains(err.Error(), "état précédent restauré") {
		t.Fatalf("échec tardif ou rollback non signalé: %v", err)
	}
	restoredTokens, err := os.ReadFile(fixture.tokensPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restoredTokens, currentTokens) {
		t.Fatalf("les jetons n'ont pas été rollbackés: %s", restoredTokens)
	}
}

func TestBackupPruneKeepsNewestValidArchivesAndNeverDeletesTheLast(t *testing.T) {
	fixture := newBackupFixture(t)
	dir := filepath.Join(fixture.dir, "prune")
	originalNow := backupNow
	t.Cleanup(func() { backupNow = originalNow })
	for i := 0; i < 3; i++ {
		backupNow = func() time.Time { return time.Date(2026, 11, 9, 12, 0, i, 0, time.UTC) }
		if _, err := createBackupArchive(fixture.configPath, backupCreateOptions{Output: dir}); err != nil {
			t.Fatal(err)
		}
	}
	remove, err := pruneCandidates(dir, 1)
	if err != nil || len(remove) != 2 {
		t.Fatalf("candidats inattendus: %v err=%v", remove, err)
	}
	for _, path := range remove {
		if err := removeBackupFile(path, dir); err != nil {
			t.Fatal(err)
		}
	}
	items, err := listBackups(dir)
	if err != nil || len(items) != 1 || items[0].CreatedAt.Second() != 2 {
		t.Fatalf("mauvaise archive conservée: %#v err=%v", items, err)
	}
	if remove, err := pruneCandidates(dir, 1); err != nil || len(remove) != 0 {
		t.Fatalf("la dernière archive serait supprimée: %v err=%v", remove, err)
	}
}
