package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

const (
	backupFormatVersion = 1
	backupMaxFileSize   = 32 << 20
	backupMaxTotalSize  = 64 << 20
	backupMaxEntries    = 8
	backupConfigPath    = "configuration/wol.yaml"
	backupTokensPath    = "secrets/tokens.json"
	backupStatePath     = "state/state.json"
)

type backupManifestFile struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type backupManifest struct {
	FormatVersion  int                  `json:"format_version"`
	Application    string               `json:"application"`
	CreatedAt      time.Time            `json:"created_at"`
	ApplicationVer string               `json:"application_version"`
	Channel        string               `json:"channel"`
	SourceRevision string               `json:"source_revision"`
	Level          string               `json:"level"`
	Sections       []string             `json:"sections"`
	Files          []backupManifestFile `json:"files"`
}

type backupArchive struct {
	Manifest backupManifest
	Files    map[string][]byte
}

type backupCreateOptions struct {
	Output     string
	WithTokens bool
	Full       bool
}

type backupCreateResult struct {
	Path     string         `json:"path"`
	Manifest backupManifest `json:"manifest"`
	Skipped  []string       `json:"skipped,omitempty"`
}

type backupInspection struct {
	Path          string         `json:"path"`
	Manifest      backupManifest `json:"manifest"`
	Networks      int            `json:"networks"`
	Machines      int            `json:"machines"`
	Groups        int            `json:"groups"`
	UPS           int            `json:"ups"`
	ContainsToken bool           `json:"contains_tokens"`
	ContainsState bool           `json:"contains_state"`
	Verified      bool           `json:"verified"`
}

type backupRestoreReport struct {
	Archive       string            `json:"archive"`
	Sections      []string          `json:"sections"`
	Differences   map[string]string `json:"differences"`
	DryRun        bool              `json:"dry_run"`
	SafetyBackup  string            `json:"safety_backup,omitempty"`
	Applied       bool              `json:"applied"`
	Revalidations int               `json:"jobs_requiring_revalidation,omitempty"`
}

type backupListItem struct {
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	Level     string    `json:"level"`
	Sections  []string  `json:"sections"`
	Version   string    `json:"application_version"`
}

var (
	backupNow               = func() time.Time { return time.Now().UTC() }
	backupAtomicRestoreFile = atomicRestoreFile
	backupServiceIsActive   = func() bool {
		if runtime.GOOS != "linux" {
			return false
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, "systemctl", "is-active", "--quiet", "wol-manager.service").Run() == nil
	}
)

func backupCLI(configPath string, args []string, jsonOut bool) error {
	return backupCLIWithIO(configPath, args, jsonOut, os.Stdin, os.Stdout)
}

func backupCLIWithIO(configPath string, args []string, jsonOut bool, input io.Reader, output io.Writer) error {
	if len(args) == 0 {
		return errUsage
	}
	switch args[0] {
	case "create":
		fs := flag.NewFlagSet("backup create", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		out := fs.String("output", "", "fichier ou répertoire de destination")
		withTokens := fs.Bool("with-tokens", false, "inclure les jetons")
		full := fs.Bool("full", false, "inclure jetons et état")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || (*withTokens && *full) {
			return errUsage
		}
		result, err := createBackupArchive(configPath, backupCreateOptions{Output: *out, WithTokens: *withTokens, Full: *full})
		if err != nil {
			return err
		}
		if jsonOut {
			return writeBackupJSON(output, result)
		}
		fmt.Fprintf(output, "Sauvegarde créée : %s\n", result.Path)
		fmt.Fprintf(output, "Niveau            : %s\n", result.Manifest.Level)
		fmt.Fprintf(output, "Sections          : %s\n", strings.Join(result.Manifest.Sections, ", "))
		for _, skipped := range result.Skipped {
			fmt.Fprintf(output, "AVERTISSEMENT     : %s absent; section non incluse\n", skipped)
		}
		fmt.Fprintln(output, "Permissions       : 0600")
		return nil

	case "list":
		fs := flag.NewFlagSet("backup list", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", defaultBackupDir(configPath), "répertoire")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 {
			return errUsage
		}
		items, err := listBackups(*dir)
		if err != nil {
			return err
		}
		if jsonOut {
			return writeBackupJSON(output, items)
		}
		if len(items) == 0 {
			fmt.Fprintln(output, "Aucune sauvegarde valide trouvée.")
			return nil
		}
		for _, item := range items {
			fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", item.CreatedAt.Format(time.RFC3339), item.Level, strings.Join(item.Sections, ","), item.Path)
		}
		return nil

	case "inspect", "verify":
		if len(args) != 2 {
			return errUsage
		}
		inspection, err := inspectBackup(args[1])
		if err != nil {
			return err
		}
		if jsonOut {
			return writeBackupJSON(output, inspection)
		}
		printBackupInspection(output, inspection)
		if args[0] == "verify" {
			fmt.Fprintln(output, "Vérification réussie : format, sommes et contenus reconnus sont valides.")
		}
		return nil

	case "restore":
		reordered := flagsFirst(args[1:], map[string]bool{"--sections": true})
		fs := flag.NewFlagSet("backup restore", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		sections := fs.String("sections", "config", "sections à restaurer")
		dryRun := fs.Bool("dry-run", false, "simulation")
		yes := fs.Bool("yes", false, "confirmation non interactive")
		if err := fs.Parse(reordered); err != nil || fs.NArg() != 1 {
			return errUsage
		}
		report, archive, selected, err := prepareRestore(configPath, fs.Arg(0), *sections, *dryRun)
		if err != nil {
			return err
		}
		if *dryRun {
			if jsonOut {
				return writeBackupJSON(output, report)
			}
			printRestoreReport(output, report)
			fmt.Fprintln(output, "Simulation terminée : aucun fichier n'a été modifié.")
			return nil
		}
		if !*yes {
			if jsonOut || !readerIsInteractive(input) {
				return errors.New("restauration non interactive refusée sans --yes")
			}
			printRestoreReport(output, report)
			if !confirmPrompt(bufio.NewReader(input), output, "Restaurer les sections sélectionnées ?") {
				fmt.Fprintln(output, "Restauration annulée; aucun fichier n'a été modifié.")
				return nil
			}
		}
		if containsString(selected, "state") && backupServiceIsActive() {
			return errors.New("restauration de state refusée pendant que wol-manager.service est actif; arrêtez-le avec « sudo systemctl stop wol-manager » puis recommencez")
		}
		result, err := applyRestore(configPath, archive, selected, report)
		if err != nil {
			return err
		}
		if jsonOut {
			return writeBackupJSON(output, result)
		}
		printRestoreReport(output, result)
		fmt.Fprintln(output, "Restauration terminée et configuration revalidée.")
		return nil

	case "prune":
		fs := flag.NewFlagSet("backup prune", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		dir := fs.String("dir", defaultBackupDir(configPath), "répertoire")
		keep := fs.Int("keep", 10, "nombre à conserver")
		dryRun := fs.Bool("dry-run", false, "simulation")
		yes := fs.Bool("yes", false, "confirmation non interactive")
		if err := fs.Parse(args[1:]); err != nil || fs.NArg() != 0 || *keep < 1 {
			return errUsage
		}
		remove, err := pruneCandidates(*dir, *keep)
		if err != nil {
			return err
		}
		if len(remove) == 0 {
			if jsonOut {
				return writeBackupJSON(output, map[string]any{"removed": []string{}, "dry_run": *dryRun})
			}
			fmt.Fprintln(output, "Aucune sauvegarde à supprimer.")
			return nil
		}
		if !*dryRun && !*yes {
			if jsonOut || !readerIsInteractive(input) {
				return errors.New("suppression non interactive refusée sans --yes")
			}
			for _, path := range remove {
				fmt.Fprintln(output, path)
			}
			if !confirmPrompt(bufio.NewReader(input), output, "Supprimer ces anciennes sauvegardes ?") {
				fmt.Fprintln(output, "Nettoyage annulé.")
				return nil
			}
		}
		if !*dryRun {
			for _, path := range remove {
				if err := removeBackupFile(path, *dir); err != nil {
					return err
				}
			}
		}
		if jsonOut {
			return writeBackupJSON(output, map[string]any{"removed": remove, "dry_run": *dryRun})
		}
		verb := "Supprimées"
		if *dryRun {
			verb = "À supprimer"
		}
		fmt.Fprintf(output, "%s : %d sauvegarde(s)\n", verb, len(remove))
		return nil
	default:
		return errUsage
	}
}

func createBackupArchive(configPath string, options backupCreateOptions) (backupCreateResult, error) {
	var result backupCreateResult
	configData, err := readRegularBackupSource(configPath)
	if err != nil {
		return result, fmt.Errorf("configuration à sauvegarder: %w", err)
	}
	cfg, err := loadConfigData(configData)
	if err != nil {
		return result, fmt.Errorf("configuration à sauvegarder invalide: %w", err)
	}
	files := map[string][]byte{backupConfigPath: configData}
	sections := []string{"config"}
	level := "configuration"
	var skipped []string
	if options.WithTokens || options.Full {
		level = "configuration+tokens"
		tokensPath := effectiveTokensFile(cfg, configPath)
		data, readErr := readOptionalBackupSource(tokensPath)
		if readErr != nil {
			return result, fmt.Errorf("jetons à sauvegarder: %w", readErr)
		}
		if data == nil {
			skipped = append(skipped, "tokens")
		} else {
			var parsed tokenFile
			if err := json.Unmarshal(data, &parsed); err != nil {
				return result, fmt.Errorf("fichier de jetons invalide: %w", err)
			}
			files[backupTokensPath] = data
			sections = append(sections, "tokens")
		}
	}
	if options.Full {
		level = "full"
		data, readErr := readOptionalBackupSource(cfg.Storage.StateFile)
		if readErr != nil {
			return result, fmt.Errorf("état à sauvegarder: %w", readErr)
		}
		if data == nil {
			skipped = append(skipped, "state")
		} else {
			var parsed jobFile
			if err := json.Unmarshal(data, &parsed); err != nil {
				return result, fmt.Errorf("fichier d'état invalide: %w", err)
			}
			files[backupStatePath] = data
			sections = append(sections, "state")
		}
	}
	now := backupNow()
	manifest := backupManifest{
		FormatVersion: backupFormatVersion, Application: "wol-manager", CreatedAt: now,
		ApplicationVer: version, Channel: buildChannel, SourceRevision: sourceRevision,
		Level: level, Sections: sections,
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		sum := sha256.Sum256(files[path])
		manifest.Files = append(manifest.Files, backupManifestFile{Path: path, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(files[path]))})
	}
	target, err := resolveBackupOutput(configPath, options.Output, now)
	if err != nil {
		return result, err
	}
	if err := writeBackupArchive(target, manifest, files); err != nil {
		return result, err
	}
	result = backupCreateResult{Path: target, Manifest: manifest, Skipped: skipped}
	return result, nil
}

func readRegularBackupSource(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("source refusée: fichier régulier requis, sans lien symbolique")
	}
	if info.Size() > backupMaxFileSize {
		return nil, fmt.Errorf("source trop volumineuse: %d octets", info.Size())
	}
	return os.ReadFile(path)
}

func readOptionalBackupSource(path string) ([]byte, error) {
	data, err := readRegularBackupSource(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	return data, err
}

func effectiveTokensFile(cfg *Config, configPath string) string {
	if cfg.API.TokensFile != "" {
		return cfg.API.TokensFile
	}
	return filepath.Join(filepath.Dir(configPath), "secrets", "tokens.json")
}

func defaultBackupDir(configPath string) string {
	if value := os.Getenv("WOL_BACKUP_DIR"); value != "" {
		return value
	}
	clean := filepath.Clean(configPath)
	if clean == "/etc/wol-manager/wol.yaml" || strings.HasPrefix(clean, "/etc/wol-manager/") {
		// Keep administrator backups outside the service-owned state tree. This
		// prevents the wol-manager account from altering its own recovery data.
		return "/var/backups/wol-manager"
	}
	return filepath.Join(filepath.Dir(configPath), "backups")
}

func resolveBackupOutput(configPath, requested string, now time.Time) (string, error) {
	useProtectedDirectory := requested == "" || !strings.HasSuffix(strings.ToLower(requested), ".wolbackup")
	if requested == "" {
		requested = defaultBackupDir(configPath)
	}
	target := requested
	if !strings.HasSuffix(strings.ToLower(requested), ".wolbackup") {
		target = filepath.Join(requested, "wol-manager-backup-"+now.Format("20060102T150405.000000000Z")+".wolbackup")
	}
	target, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if err := rejectBackupSymlinkComponents(filepath.Dir(target)); err != nil {
		return "", err
	}
	_, statErr := os.Stat(filepath.Dir(target))
	parentCreated := os.IsNotExist(statErr)
	if statErr != nil && !parentCreated {
		return "", statErr
	}
	if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
		return "", err
	}
	if useProtectedDirectory || parentCreated {
		if err := secureBackupDir(filepath.Dir(target)); err != nil {
			return "", err
		}
	}
	if info, err := os.Lstat(target); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("destination de sauvegarde refusée: lien symbolique")
		}
		return "", errors.New("destination de sauvegarde déjà existante")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return target, nil
}

func writeBackupArchive(target string, manifest backupManifest, files map[string][]byte) error {
	temp, err := os.CreateTemp(filepath.Dir(target), ".wol-backup-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0600); err != nil {
		temp.Close()
		return err
	}
	gz := gzip.NewWriter(temp)
	gz.Header.ModTime = manifest.CreatedAt
	tarWriter := tar.NewWriter(gz)
	manifestData, err := json.MarshalIndent(manifest, "", "  ")
	if err == nil {
		manifestData = append(manifestData, '\n')
		err = writeBackupTarFile(tarWriter, "manifest.json", manifestData, manifest.CreatedAt)
	}
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		if err == nil {
			err = writeBackupTarFile(tarWriter, path, files[path], manifest.CreatedAt)
		}
	}
	if closeErr := tarWriter.Close(); err == nil {
		err = closeErr
	}
	if closeErr := gz.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := secureBackupFile(tempPath); err != nil {
		return err
	}
	if err := os.Rename(tempPath, target); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(target))
}

func writeBackupTarFile(writer *tar.Writer, name string, data []byte, modified time.Time) error {
	header := &tar.Header{Name: name, Mode: 0600, Size: int64(len(data)), ModTime: modified, Typeflag: tar.TypeReg}
	if err := writer.WriteHeader(header); err != nil {
		return err
	}
	_, err := writer.Write(data)
	return err
}

func readBackupArchive(path string) (backupArchive, error) {
	var archive backupArchive
	info, err := os.Lstat(path)
	if err != nil {
		return archive, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return archive, errors.New("archive refusée: fichier régulier requis, sans lien symbolique")
	}
	file, err := os.Open(path)
	if err != nil {
		return archive, err
	}
	defer file.Close()
	gz, err := gzip.NewReader(io.LimitReader(file, backupMaxTotalSize+1))
	if err != nil {
		return archive, fmt.Errorf("archive gzip invalide: %w", err)
	}
	defer gz.Close()
	reader := tar.NewReader(gz)
	entries := map[string][]byte{}
	total := int64(0)
	for count := 0; ; count++ {
		header, nextErr := reader.Next()
		if errors.Is(nextErr, io.EOF) {
			break
		}
		if nextErr != nil {
			return archive, fmt.Errorf("archive tar invalide: %w", nextErr)
		}
		if count >= backupMaxEntries {
			return archive, errors.New("archive refusée: trop d'entrées")
		}
		name := filepath.ToSlash(header.Name)
		if header.Typeflag != tar.TypeReg || !allowedBackupArchivePath(name) {
			return archive, fmt.Errorf("entrée d'archive refusée: %s", name)
		}
		if _, duplicate := entries[name]; duplicate {
			return archive, fmt.Errorf("entrée d'archive dupliquée: %s", name)
		}
		if header.Size < 0 || header.Size > backupMaxFileSize || total+header.Size > backupMaxTotalSize {
			return archive, fmt.Errorf("entrée d'archive trop volumineuse: %s", name)
		}
		data, readErr := io.ReadAll(io.LimitReader(reader, header.Size+1))
		if readErr != nil || int64(len(data)) != header.Size {
			return archive, fmt.Errorf("lecture d'archive incomplète: %s", name)
		}
		entries[name] = data
		total += header.Size
	}
	manifestData, ok := entries["manifest.json"]
	if !ok {
		return archive, errors.New("manifest.json absent de l'archive")
	}
	if err := json.Unmarshal(manifestData, &archive.Manifest); err != nil {
		return archive, fmt.Errorf("manifest de sauvegarde invalide: %w", err)
	}
	delete(entries, "manifest.json")
	archive.Files = entries
	if err := validateBackupArchive(archive); err != nil {
		return backupArchive{}, err
	}
	return archive, nil
}

func allowedBackupArchivePath(path string) bool {
	return path == "manifest.json" || path == backupConfigPath || path == backupTokensPath || path == backupStatePath
}

func validateBackupArchive(archive backupArchive) error {
	m := archive.Manifest
	if m.FormatVersion != backupFormatVersion || m.Application != "wol-manager" {
		return errors.New("format de sauvegarde non pris en charge")
	}
	if m.CreatedAt.IsZero() || len(m.Files) == 0 || len(m.Sections) == 0 {
		return errors.New("manifest de sauvegarde incomplet")
	}
	expected := map[string]backupManifestFile{}
	for _, item := range m.Files {
		if !allowedBackupArchivePath(item.Path) || item.Path == "manifest.json" {
			return fmt.Errorf("chemin de manifest refusé: %s", item.Path)
		}
		if _, duplicate := expected[item.Path]; duplicate {
			return fmt.Errorf("chemin de manifest dupliqué: %s", item.Path)
		}
		expected[item.Path] = item
	}
	if len(expected) != len(archive.Files) {
		return errors.New("contenu et manifest de sauvegarde incohérents")
	}
	for path, data := range archive.Files {
		item, ok := expected[path]
		if !ok || item.Size != int64(len(data)) {
			return fmt.Errorf("taille incohérente pour %s", path)
		}
		sum := sha256.Sum256(data)
		if item.SHA256 != hex.EncodeToString(sum[:]) {
			return fmt.Errorf("SHA-256 invalide pour %s", path)
		}
	}
	configData, ok := archive.Files[backupConfigPath]
	if !ok {
		return errors.New("configuration absente de la sauvegarde")
	}
	if _, err := loadConfigData(configData); err != nil {
		return fmt.Errorf("configuration sauvegardée invalide: %w", err)
	}
	if data, ok := archive.Files[backupTokensPath]; ok {
		var parsed tokenFile
		if err := json.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("jetons sauvegardés invalides: %w", err)
		}
	}
	if data, ok := archive.Files[backupStatePath]; ok {
		var parsed jobFile
		if err := json.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("état sauvegardé invalide: %w", err)
		}
	}
	expectedSections := []string{"config"}
	expectedLevel := "configuration"
	if _, ok := archive.Files[backupTokensPath]; ok {
		expectedSections = append(expectedSections, "tokens")
		expectedLevel = "configuration+tokens"
	}
	if _, ok := archive.Files[backupStatePath]; ok {
		expectedSections = append(expectedSections, "state")
		expectedLevel = "full"
	}
	if m.Level != expectedLevel || !equalStrings(m.Sections, expectedSections) {
		return errors.New("niveau ou sections du manifest incohérents avec les fichiers")
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func inspectBackup(path string) (backupInspection, error) {
	archive, err := readBackupArchive(path)
	if err != nil {
		return backupInspection{}, err
	}
	cfg, _ := loadConfigData(archive.Files[backupConfigPath])
	return backupInspection{
		Path: path, Manifest: archive.Manifest, Networks: len(cfg.Networks), Machines: len(cfg.Servers), Groups: len(cfg.Groups), UPS: len(cfg.UPS),
		ContainsToken: archive.Files[backupTokensPath] != nil, ContainsState: archive.Files[backupStatePath] != nil, Verified: true,
	}, nil
}

func printBackupInspection(output io.Writer, inspection backupInspection) {
	fmt.Fprintf(output, "Archive              : %s\n", inspection.Path)
	fmt.Fprintf(output, "Format               : %d\n", inspection.Manifest.FormatVersion)
	fmt.Fprintf(output, "Créée le             : %s\n", inspection.Manifest.CreatedAt.Format(time.RFC3339))
	fmt.Fprintf(output, "WoL Manager          : %s (%s)\n", inspection.Manifest.ApplicationVer, inspection.Manifest.Channel)
	fmt.Fprintf(output, "Niveau               : %s\n", inspection.Manifest.Level)
	fmt.Fprintf(output, "Sections             : %s\n", strings.Join(inspection.Manifest.Sections, ", "))
	fmt.Fprintf(output, "Configuration        : %d réseau(x), %d machine(s), %d groupe(s), %d UPS\n", inspection.Networks, inspection.Machines, inspection.Groups, inspection.UPS)
	fmt.Fprintf(output, "Jetons API           : %t\n", inspection.ContainsToken)
	fmt.Fprintf(output, "État jobs/cycles     : %t\n", inspection.ContainsState)
}

func listBackups(dir string) ([]backupListItem, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []backupListItem{}, nil
	}
	if err != nil {
		return nil, err
	}
	var items []backupListItem
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".wolbackup") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue
		}
		archive, readErr := readBackupArchive(path)
		if readErr != nil {
			continue
		}
		items = append(items, backupListItem{Path: path, CreatedAt: archive.Manifest.CreatedAt, Level: archive.Manifest.Level, Sections: archive.Manifest.Sections, Version: archive.Manifest.ApplicationVer})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func prepareRestore(configPath, archivePath, sectionList string, dryRun bool) (backupRestoreReport, backupArchive, []string, error) {
	archive, err := readBackupArchive(archivePath)
	if err != nil {
		return backupRestoreReport{}, backupArchive{}, nil, err
	}
	selected, err := parseRestoreSections(sectionList, archive)
	if err != nil {
		return backupRestoreReport{}, backupArchive{}, nil, err
	}
	if !backupArchivePermissionsSafe(archivePath) {
		return backupRestoreReport{}, backupArchive{}, nil, errors.New("permissions de l'archive trop ouvertes; utilisez chmod 600 avant la restauration")
	}
	differences := map[string]string{}
	currentData, readErr := os.ReadFile(configPath)
	if readErr == nil {
		if sha256.Sum256(currentData) == sha256.Sum256(archive.Files[backupConfigPath]) {
			differences["config"] = "identique"
		} else {
			current, currentErr := loadConfigData(currentData)
			restored, restoredErr := loadConfigData(archive.Files[backupConfigPath])
			if currentErr == nil && restoredErr == nil {
				differences["config"] = fmt.Sprintf("actuelle %d réseau(x)/%d machine(s)/%d groupe(s); sauvegarde %d/%d/%d", len(current.Networks), len(current.Servers), len(current.Groups), len(restored.Networks), len(restored.Servers), len(restored.Groups))
			} else {
				differences["config"] = "contenu différent"
			}
		}
	} else if os.IsNotExist(readErr) {
		differences["config"] = "configuration actuelle absente"
	} else {
		return backupRestoreReport{}, backupArchive{}, nil, readErr
	}
	for _, section := range selected {
		if section != "config" {
			differences[section] = "sera remplacé sans afficher son contenu"
		}
	}
	report := backupRestoreReport{Archive: archivePath, Sections: selected, Differences: differences, DryRun: dryRun}
	return report, archive, selected, nil
}

func parseRestoreSections(value string, archive backupArchive) ([]string, error) {
	available := map[string]bool{"config": archive.Files[backupConfigPath] != nil, "tokens": archive.Files[backupTokensPath] != nil, "state": archive.Files[backupStatePath] != nil}
	if strings.TrimSpace(value) == "all" {
		var result []string
		for _, section := range []string{"config", "tokens", "state"} {
			if available[section] {
				result = append(result, section)
			}
		}
		return result, nil
	}
	seen := map[string]bool{}
	var result []string
	for _, raw := range strings.Split(value, ",") {
		section := strings.TrimSpace(raw)
		if section == "" || (section != "config" && section != "tokens" && section != "state") {
			return nil, fmt.Errorf("section de restauration invalide %q", section)
		}
		if !available[section] {
			return nil, fmt.Errorf("section %s absente de la sauvegarde", section)
		}
		if !seen[section] {
			seen[section] = true
			result = append(result, section)
		}
	}
	return result, nil
}

func printRestoreReport(output io.Writer, report backupRestoreReport) {
	fmt.Fprintf(output, "Archive              : %s\n", report.Archive)
	fmt.Fprintf(output, "Sections             : %s\n", strings.Join(report.Sections, ", "))
	for _, section := range report.Sections {
		fmt.Fprintf(output, "%-20s : %s\n", section, report.Differences[section])
	}
	if report.SafetyBackup != "" {
		fmt.Fprintf(output, "Sauvegarde de sécurité : %s\n", report.SafetyBackup)
	}
	if report.Revalidations > 0 {
		fmt.Fprintf(output, "Jobs suspendus       : %d\n", report.Revalidations)
	}
}

type restoreTarget struct {
	section string
	path    string
	data    []byte
	mode    os.FileMode
	secure  func(string) error
	old     []byte
	existed bool
}

func applyRestore(configPath string, archive backupArchive, selected []string, report backupRestoreReport) (backupRestoreReport, error) {
	currentCfg, err := LoadConfig(configPath)
	currentMissing := os.IsNotExist(err)
	if err != nil && !currentMissing {
		return report, fmt.Errorf("configuration actuelle requise pour la sauvegarde de sécurité: %w", err)
	}
	restoredCfg, err := loadConfigData(archive.Files[backupConfigPath])
	if err != nil {
		return report, err
	}
	if !currentMissing {
		options := backupCreateOptions{}
		if containsString(selected, "state") {
			options.Full = true
		} else if containsString(selected, "tokens") {
			options.WithTokens = true
		}
		safety, safetyErr := createBackupArchive(configPath, options)
		if safetyErr != nil {
			return report, fmt.Errorf("sauvegarde de sécurité impossible: %w", safetyErr)
		}
		report.SafetyBackup = safety.Path
	}
	targetCfg := currentCfg
	if currentMissing || containsString(selected, "config") {
		targetCfg = restoredCfg
	}
	var targets []restoreTarget
	if containsString(selected, "tokens") {
		path := effectiveTokensFile(targetCfg, configPath)
		if err := validateRestoreTarget(configPath, path, "tokens"); err != nil {
			return report, err
		}
		targets = append(targets, restoreTarget{section: "tokens", path: path, data: archive.Files[backupTokensPath], mode: 0600, secure: secureTokenFile})
	}
	if containsString(selected, "state") {
		path := targetCfg.Storage.StateFile
		if err := validateRestoreTarget(configPath, path, "state"); err != nil {
			return report, err
		}
		state, count, err := suspendRestoredJobs(archive.Files[backupStatePath])
		if err != nil {
			return report, err
		}
		report.Revalidations = count
		targets = append(targets, restoreTarget{section: "state", path: path, data: state, mode: 0640, secure: secureStateFile})
	}
	if containsString(selected, "config") {
		targets = append(targets, restoreTarget{section: "config", path: configPath, data: archive.Files[backupConfigPath], mode: 0640, secure: func(path string) error { return secureConfigFile(path, configPath) }})
	}
	for i := range targets {
		old, readErr := os.ReadFile(targets[i].path)
		if readErr == nil {
			targets[i].old, targets[i].existed = old, true
		} else if !os.IsNotExist(readErr) {
			return report, readErr
		}
	}
	for i := range targets {
		if err := backupAtomicRestoreFile(targets[i].path, targets[i].data, targets[i].mode, targets[i].secure); err != nil {
			rollbackErr := rollbackRestoreTargets(targets[:i])
			if rollbackErr != nil {
				return report, fmt.Errorf("restauration de %s échouée: %v; rollback incomplet: %w", targets[i].section, err, rollbackErr)
			}
			return report, fmt.Errorf("restauration de %s échouée; état précédent restauré: %w", targets[i].section, err)
		}
	}
	if _, err := LoadConfig(configPath); err != nil {
		rollbackErr := rollbackRestoreTargets(targets)
		if rollbackErr != nil {
			return report, fmt.Errorf("configuration restaurée invalide: %v; rollback incomplet: %w", err, rollbackErr)
		}
		return report, fmt.Errorf("configuration restaurée invalide; état précédent restauré: %w", err)
	}
	report.Applied = true
	return report, nil
}

func atomicRestoreFile(path string, data []byte, mode os.FileMode, secure func(string) error) error {
	if err := rejectBackupSymlinkComponents(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	if mode == 0640 && strings.Contains(filepath.ToSlash(path), "/var/lib/wol-manager/") {
		if err := secureStateDir(filepath.Dir(path)); err != nil {
			return err
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".wol-restore-*.tmp")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err = temp.Chmod(mode); err == nil {
		_, err = temp.Write(data)
	}
	if err == nil {
		err = temp.Sync()
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := secure(tempPath); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

func rollbackRestoreTargets(targets []restoreTarget) error {
	var problems []string
	for i := len(targets) - 1; i >= 0; i-- {
		target := targets[i]
		if target.existed {
			if err := backupAtomicRestoreFile(target.path, target.old, target.mode, target.secure); err != nil {
				problems = append(problems, target.section+": "+err.Error())
			}
		} else if err := os.Remove(target.path); err != nil && !os.IsNotExist(err) {
			problems = append(problems, target.section+": "+err.Error())
		}
	}
	if len(problems) > 0 {
		return errors.New(strings.Join(problems, "; "))
	}
	return nil
}

func suspendRestoredJobs(data []byte) ([]byte, int, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, 0, err
	}
	count := 0
	jobs, _ := root["jobs"].(map[string]any)
	for _, raw := range jobs {
		job, _ := raw.(map[string]any)
		state, _ := job["state"].(string)
		if state == "queued" || state == "running" {
			job["state"] = "revalidation_required"
			job["message"] = "restauré depuis une sauvegarde; reprise interdite avant revalidation"
			job["updated"] = backupNow().Format(time.RFC3339Nano)
			count++
		}
	}
	encoded, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	encoded = append(encoded, '\n')
	var parsed jobFile
	if err := json.Unmarshal(encoded, &parsed); err != nil {
		return nil, 0, err
	}
	return encoded, count, nil
}

func validateRestoreTarget(configPath, target, section string) error {
	if target == "" || !filepath.IsAbs(target) {
		return fmt.Errorf("cible %s refusée: chemin absolu requis", section)
	}
	cleanConfig := filepath.Clean(configPath)
	cleanTarget := filepath.Clean(target)
	if cleanConfig == "/etc/wol-manager/wol.yaml" || strings.HasPrefix(cleanConfig, "/etc/wol-manager/") {
		allowedRoot := "/etc/wol-manager/secrets"
		if section == "state" {
			allowedRoot = "/var/lib/wol-manager"
		}
		if !pathWithin(cleanTarget, allowedRoot) {
			return fmt.Errorf("cible %s refusée hors de %s", section, allowedRoot)
		}
	}
	return rejectBackupSymlinkComponents(cleanTarget)
}

func pathWithin(path, root string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(os.PathSeparator))
}

func pruneCandidates(dir string, keep int) ([]string, error) {
	items, err := listBackups(dir)
	if err != nil {
		return nil, err
	}
	if len(items) <= keep {
		return []string{}, nil
	}
	remove := make([]string, 0, len(items)-keep)
	for _, item := range items[keep:] {
		remove = append(remove, item.Path)
	}
	return remove, nil
}

func removeBackupFile(path, dir string) error {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if filepath.Dir(absPath) != absDir || !strings.HasSuffix(absPath, ".wolbackup") {
		return errors.New("suppression refusée hors du répertoire de sauvegarde")
	}
	info, err := os.Lstat(absPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("suppression refusée: fichier régulier requis")
	}
	return os.Remove(absPath)
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func writeBackupJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func readerIsInteractive(input io.Reader) bool {
	file, ok := input.(*os.File)
	return ok && isTerminalFile(file)
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}
