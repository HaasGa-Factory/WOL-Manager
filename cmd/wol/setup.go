package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

func setup(path string) error {
	return setupWithIO(path, os.Stdin, os.Stdout)
}

func setupWithIO(path string, input io.Reader, output io.Writer) error {
	reader := bufio.NewReader(input)
	base := []byte(minimalConfig)
	existing := false
	current := map[string]UPSConfig{}
	if data, err := os.ReadFile(path); err == nil {
		existing = true
		base = data
		cfg, loadErr := LoadConfig(path)
		if loadErr != nil {
			return fmt.Errorf("configuration existante invalide, aucune modification: %w", loadErr)
		}
		for id, ups := range cfg.UPS {
			current[id] = ups
		}
		fmt.Fprintf(output, "Configuration existante détectée: %s (%d UPS configuré(s)).\n", path, len(current))
	} else if !os.IsNotExist(err) {
		return err
	}

	fmt.Fprintln(output, "Souhaitez-vous configurer un serveur NUT ?")
	fmt.Fprintln(output, "[1] Oui\n[2] Non\n[3] Plus tard")
	choice := prompt(reader, output, "> ")
	if choice != "1" {
		if existing {
			fmt.Fprintln(output, "Configuration existante conservée sans modification.")
			return nil
		}
		if choice != "2" && choice != "3" {
			return errors.New("choix invalide; aucune configuration écrite")
		}
		return writeSetupConfig(path, base, current, false)
	}

	working := cloneUPS(current)
	if len(working) == 0 {
		fmt.Fprintln(output, "Recherche d'un serveur NUT local sur 127.0.0.1:3493...")
		local := UPSConfig{Address: "127.0.0.1:3493", Timeout: Duration(3 * time.Second)}
		devices, err := listNUTUPS(context.Background(), local)
		if err != nil {
			fmt.Fprintf(output, "Aucun serveur NUT local utilisable: %v\n", err)
		} else if len(devices) == 0 {
			fmt.Fprintln(output, "Serveur NUT local détecté, mais aucun UPS n'est disponible.")
		} else {
			fmt.Fprintln(output, "Serveur NUT local détecté.")
			if err := selectNUTDevices(reader, output, "local", local, devices, working); err != nil {
				fmt.Fprintf(output, "Sélection locale non enregistrée: %v\n", err)
			}
		}
	}

	for {
		fmt.Fprintln(output, "\nConfiguration NUT :")
		fmt.Fprintln(output, "[1] Ajouter ou modifier un serveur\n[2] Supprimer un UPS configuré\n[3] Tester les UPS configurés\n[4] Enregistrer\n[5] Annuler sans modifier")
		switch prompt(reader, output, "> ") {
		case "1":
			if err := configureRemoteNUT(reader, output, working); err != nil {
				fmt.Fprintf(output, "Configuration reportée: %v\n", err)
			}
		case "2":
			removeConfiguredUPS(reader, output, working)
		case "3":
			testConfiguredUPS(output, working)
		case "4":
			return writeSetupConfig(path, base, working, existing)
		case "5":
			return errors.New("annulé; configuration existante conservée")
		default:
			fmt.Fprintln(output, "Choix invalide.")
		}
	}
}

func configureRemoteNUT(reader *bufio.Reader, output io.Writer, configured map[string]UPSConfig) error {
	address := prompt(reader, output, "Adresse IP ou DNS: ")
	if address == "" {
		return errors.New("adresse vide")
	}
	port := prompt(reader, output, "Port [3493]: ")
	if port == "" {
		port = "3493"
	}
	if _, err := strconv.Atoi(port); err != nil {
		return errors.New("port invalide")
	}
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = net.JoinHostPort(host, port)
	} else {
		address = net.JoinHostPort(strings.Trim(address, "[]"), port)
	}
	server := UPSConfig{Address: address, Timeout: Duration(3 * time.Second)}
	if strings.EqualFold(prompt(reader, output, "Utiliser STARTTLS ? [y/N]: "), "y") {
		server.TLS = true
		server.ServerName = prompt(reader, output, "Nom TLS du serveur [adresse]: ")
	} else {
		fmt.Fprintln(output, "ATTENTION: une connexion NUT distante sans TLS n'est pas chiffrée.")
		if prompt(reader, output, "Autoriser explicitement cette connexion ? [y/N]: ") != "y" {
			return errors.New("connexion distante non sécurisée refusée")
		}
		server.AllowInsecureRemote = true
	}

	for {
		fmt.Fprintf(output, "Connexion au serveur NUT %s...\n", server.Address)
		devices, err := listNUTUPS(context.Background(), server)
		if err == nil && len(devices) > 0 {
			serverID := prompt(reader, output, "Identifiant du serveur [nut]: ")
			if serverID == "" {
				serverID = "nut"
			}
			return selectNUTDevices(reader, output, serverID, server, devices, configured)
		}
		if err == nil {
			err = errors.New("aucun UPS disponible")
		}
		fmt.Fprintf(output, "Échec: %v\n", err)
		fmt.Fprintln(output, "[1] Réessayer\n[2] Modifier l'adresse ou le port\n[3] Configurer un autre serveur\n[4] Reporter la configuration")
		switch prompt(reader, output, "> ") {
		case "1":
			continue
		case "2", "3":
			return configureRemoteNUT(reader, output, configured)
		default:
			return errors.New("serveur NUT non enregistré")
		}
	}
}

func selectNUTDevices(reader *bufio.Reader, output io.Writer, serverID string, server UPSConfig, devices []NUTDevice, configured map[string]UPSConfig) error {
	fmt.Fprintln(output, "UPS disponibles :")
	for i, device := range devices {
		fmt.Fprintf(output, "[%d] %s", i+1, device.Name)
		if device.Description != "" {
			fmt.Fprintf(output, " — %s", device.Description)
		}
		fmt.Fprintln(output)
	}
	selection := prompt(reader, output, "Sélectionnez un ou plusieurs UPS (ex. 1,2 ou tous): ")
	indices, err := parseSelection(selection, len(devices))
	if err != nil {
		return err
	}
	added := 0
	for _, index := range indices {
		device := devices[index]
		ups := server
		ups.Name = device.Name
		status := nutStatus(context.Background(), device.Name, ups)
		if status.Error != "" {
			fmt.Fprintf(output, "%s non enregistré: lecture de ups.status impossible: %s\n", device.Name, status.Error)
			continue
		}
		baseID := slug(serverID) + "-" + slug(device.Name)
		id := baseID
		if _, exists := configured[baseID]; exists {
			if strings.EqualFold(prompt(reader, output, fmt.Sprintf("%s existe déjà. Le remplacer ? [y/N]: ", baseID)), "y") {
				id = baseID
			} else {
				id = uniqueUPSID(configured, baseID)
			}
		}
		configured[id] = ups
		added++
		fmt.Fprintf(output, "%s enregistré sous %q, état %s.\n", device.Name, id, status.RawStatus)
	}
	if added == 0 {
		return errors.New("aucun UPS validé")
	}
	return nil
}

func parseSelection(value string, count int) ([]int, error) {
	if strings.EqualFold(strings.TrimSpace(value), "tous") {
		result := make([]int, count)
		for i := range result {
			result[i] = i
		}
		return result, nil
	}
	seen := map[int]bool{}
	var result []int
	for _, part := range strings.Split(value, ",") {
		index, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || index < 1 || index > count {
			return nil, errors.New("sélection invalide")
		}
		if !seen[index-1] {
			seen[index-1] = true
			result = append(result, index-1)
		}
	}
	if len(result) == 0 {
		return nil, errors.New("aucun UPS sélectionné")
	}
	return result, nil
}

func removeConfiguredUPS(reader *bufio.Reader, output io.Writer, configured map[string]UPSConfig) {
	ids := sortedUPSIDs(configured)
	if len(ids) == 0 {
		fmt.Fprintln(output, "Aucun UPS configuré.")
		return
	}
	for i, id := range ids {
		fmt.Fprintf(output, "[%d] %s (%s / %s)\n", i+1, id, configured[id].Address, configured[id].Name)
	}
	selection, err := parseSelection(prompt(reader, output, "UPS à supprimer: "), len(ids))
	if err != nil {
		fmt.Fprintln(output, err)
		return
	}
	for _, index := range selection {
		delete(configured, ids[index])
	}
}

func testConfiguredUPS(output io.Writer, configured map[string]UPSConfig) bool {
	ok := true
	for _, id := range sortedUPSIDs(configured) {
		status := nutStatus(context.Background(), id, configured[id])
		if status.Error != "" {
			ok = false
			fmt.Fprintf(output, "%s: ERREUR — %s\n", id, status.Error)
		} else {
			fmt.Fprintf(output, "%s: %s\n", id, status.RawStatus)
		}
	}
	return ok
}

func writeSetupConfig(path string, base []byte, ups map[string]UPSConfig, backup bool) error {
	var document yaml.Node
	if err := yaml.Unmarshal(base, &document); err != nil {
		return fmt.Errorf("YAML existant invalide: %w", err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("racine YAML invalide")
	}
	root := document.Content[0]
	upsNode := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for _, id := range sortedUPSIDs(ups) {
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: id}
		value := &yaml.Node{}
		if err := value.Encode(ups[id]); err != nil {
			return err
		}
		upsNode.Content = append(upsNode.Content, key, value)
	}
	replaced := false
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "ups" {
			root.Content[i+1] = upsNode
			replaced = true
			break
		}
	}
	if !replaced {
		root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "ups"}, upsNode)
	}
	var encoded bytes.Buffer
	encoder := yaml.NewEncoder(&encoded)
	encoder.SetIndent(2)
	if err := encoder.Encode(&document); err != nil {
		return err
	}
	_ = encoder.Close()

	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".wol.yaml.*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0640); err == nil {
		_, err = temp.Write(encoded.Bytes())
	}
	closeErr := temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if _, err := LoadConfig(tempPath); err != nil {
		return fmt.Errorf("nouvelle configuration refusée: %w", err)
	}
	if backup {
		backupPath := path + ".bak-" + time.Now().UTC().Format("20060102T150405Z")
		if err := os.WriteFile(backupPath, base, 0640); err != nil {
			return fmt.Errorf("sauvegarde impossible: %w", err)
		}
	}
	if runtime.GOOS == "windows" {
		// La cible prise en charge est Debian, où rename remplace atomiquement.
		// Ce chemin permet seulement les tests et l'usage développeur Windows.
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := os.Rename(tempPath, path); err != nil {
		return err
	}
	return nil
}

func prompt(reader *bufio.Reader, output io.Writer, text string) string {
	fmt.Fprint(output, text)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(line)
}

func cloneUPS(source map[string]UPSConfig) map[string]UPSConfig {
	copy := make(map[string]UPSConfig, len(source))
	for id, ups := range source {
		copy[id] = ups
	}
	return copy
}

func sortedUPSIDs(configured map[string]UPSConfig) []string {
	ids := make([]string, 0, len(configured))
	for id := range configured {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func slug(value string) string {
	var result strings.Builder
	for _, r := range strings.ToLower(value) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			result.WriteRune(r)
		} else if result.Len() > 0 && !strings.HasSuffix(result.String(), "-") {
			result.WriteByte('-')
		}
	}
	return strings.Trim(result.String(), "-")
}

func uniqueUPSID(configured map[string]UPSConfig, base string) string {
	if base == "" {
		base = "nut-ups"
	}
	if _, exists := configured[base]; !exists {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if _, exists := configured[candidate]; !exists {
			return candidate
		}
	}
}

const minimalConfig = `version: 1
global:
  port: 9
  transport_order: [local_broadcast, unicast, directed_broadcast, relay]
  checks:
    - {type: tcp, port: 22, timeout: 2s}
api:
  listen: 127.0.0.1:8080
  tokens_file: /etc/wol-manager/secrets/tokens.json
storage:
  state_file: /var/lib/wol-manager/state.json
  audit_file: /var/log/wol-manager/audit.jsonl
network_discovery:
  passive_enabled: true
  active_enabled: false
  allowed_subnets: []
  safeguards: {require_confirmation: true, max_packets: 20, rate_limit_pps: 2, timeout: 30s}
networks: {}
groups: {}
servers: {}
ups: {}
relays: {}
automations: []
`
