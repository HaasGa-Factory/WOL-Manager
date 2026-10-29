package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type UPSStatus struct {
	ID           string  `json:"id"`
	State        string  `json:"state"`
	RawStatus    string  `json:"raw_status,omitempty"`
	Battery      float64 `json:"battery_percent,omitempty"`
	Runtime      int     `json:"runtime_seconds,omitempty"`
	BatteryKnown bool    `json:"battery_known"`
	RuntimeKnown bool    `json:"runtime_known"`
	Error        string  `json:"error,omitempty"`
}

type upsRevalidationError struct{ cause error }

func (e *upsRevalidationError) Error() string { return e.cause.Error() }
func (e *upsRevalidationError) Unwrap() error { return e.cause }

func upsRevalidationErrorf(format string, args ...any) error {
	return &upsRevalidationError{cause: fmt.Errorf(format, args...)}
}

func needsUPSRevalidation(err error) bool {
	var target *upsRevalidationError
	return errors.As(err, &target)
}

type NUTDevice struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

const (
	maxNUTLineBytes     = 64 << 10
	maxNUTListLines     = 4096
	maxNUTResponseBytes = 4 << 20
)

var errNUTResponseLimit = errors.New("limite de réponse NUT dépassée")

type nutConnection struct {
	conn net.Conn
	rw   *bufio.ReadWriter
}

func newNUTReadWriter(conn net.Conn) *bufio.ReadWriter {
	return bufio.NewReadWriter(bufio.NewReaderSize(conn, maxNUTLineBytes+1), bufio.NewWriter(conn))
}

func openNUT(ctx context.Context, u UPSConfig) (*nutConnection, error) {
	timeout := u.Timeout.Value(3 * time.Second)
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", u.Address)
	if err != nil {
		return nil, classifyNUTError(err)
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	n := &nutConnection{conn: conn, rw: newNUTReadWriter(conn)}
	if u.TLS {
		line, err := n.one("STARTTLS")
		if err != nil {
			conn.Close()
			return nil, classifyNUTError(err)
		}
		if line != "OK STARTTLS" {
			conn.Close()
			return nil, fmt.Errorf("réponse non conforme au protocole NUT STARTTLS: %s", line)
		}
		roots, err := x509.SystemCertPool()
		if err != nil {
			conn.Close()
			return nil, err
		}
		if u.CACert != "" {
			pem, readErr := os.ReadFile(u.CACert)
			if readErr != nil || !roots.AppendCertsFromPEM(pem) {
				conn.Close()
				return nil, errors.New("CA NUT invalide")
			}
		}
		serverName := u.ServerName
		if serverName == "" {
			serverName, _, _ = net.SplitHostPort(u.Address)
		}
		tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName, RootCAs: roots})
		if err = tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS NUT: %w", err)
		}
		n.conn = tlsConn
		n.rw = newNUTReadWriter(tlsConn)
	}
	if u.UsernameFile != "" {
		user, userErr := readSecret(u.UsernameFile)
		pass, passErr := readSecret(u.PasswordFile)
		if userErr != nil || passErr != nil {
			n.shutdown()
			return nil, errors.New("identifiants NUT illisibles")
		}
		if line, err := n.one("USERNAME " + user); err != nil || strings.HasPrefix(line, "ERR ") {
			n.shutdown()
			return nil, nutResponseError(line, err)
		}
		if line, err := n.one("PASSWORD " + pass); err != nil || strings.HasPrefix(line, "ERR ") {
			n.shutdown()
			return nil, nutResponseError(line, err)
		}
	}
	return n, nil
}

func (n *nutConnection) shutdown() { _ = n.conn.Close() }

func (n *nutConnection) readLine() (string, int, error) {
	line, err := n.rw.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxNUTLineBytes {
		n.shutdown()
		return "", 0, fmt.Errorf("%w: ligne supérieure à %d octets", errNUTResponseLimit, maxNUTLineBytes)
	}
	if err != nil {
		return "", 0, err
	}
	return strings.TrimSpace(string(line)), len(line), nil
}

func (n *nutConnection) one(command string) (string, error) {
	if _, err := n.rw.WriteString(command + "\n"); err != nil {
		return "", err
	}
	if err := n.rw.Flush(); err != nil {
		return "", err
	}
	line, _, err := n.readLine()
	return line, err
}

func (n *nutConnection) list(command, begin, end string) ([]string, error) {
	if _, err := n.rw.WriteString(command + "\n"); err != nil {
		return nil, err
	}
	if err := n.rw.Flush(); err != nil {
		return nil, err
	}
	var lines []string
	totalBytes := 0
	for {
		line, lineBytes, err := n.readLine()
		if err != nil {
			return nil, classifyNUTError(err)
		}
		totalBytes += lineBytes
		if len(lines) >= maxNUTListLines {
			n.shutdown()
			return nil, fmt.Errorf("%w: plus de %d lignes", errNUTResponseLimit, maxNUTListLines)
		}
		if totalBytes > maxNUTResponseBytes {
			n.shutdown()
			return nil, fmt.Errorf("%w: plus de %d octets cumulés", errNUTResponseLimit, maxNUTResponseBytes)
		}
		if len(lines) == 0 && line != begin && !strings.HasPrefix(line, "ERR ") {
			return nil, fmt.Errorf("réponse ne correspondant pas au protocole NUT: %q", line)
		}
		lines = append(lines, line)
		if line == end {
			return lines, nil
		}
		if strings.HasPrefix(line, "ERR ") {
			return nil, nutResponseError(line, nil)
		}
	}
}

func listNUTUPS(ctx context.Context, u UPSConfig) ([]NUTDevice, error) {
	n, err := openNUT(ctx, u)
	if err != nil {
		return nil, err
	}
	defer n.shutdown()
	lines, err := n.list("LIST UPS", "BEGIN LIST UPS", "END LIST UPS")
	if err != nil {
		return nil, err
	}
	var devices []NUTDevice
	for _, line := range lines {
		parts := strings.SplitN(line, " ", 3)
		if len(parts) != 3 || parts[0] != "UPS" {
			continue
		}
		description, unquoteErr := strconv.Unquote(parts[2])
		if unquoteErr != nil {
			description = strings.Trim(parts[2], "\"")
		}
		devices = append(devices, NUTDevice{Name: parts[1], Description: description})
	}
	return devices, nil
}

func nutStatus(ctx context.Context, id string, u UPSConfig) UPSStatus {
	result := UPSStatus{ID: id, State: "unknown"}
	n, err := openNUT(ctx, u)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer n.shutdown()
	lines, err := n.list("LIST VAR "+u.Name, "BEGIN LIST VAR "+u.Name, "END LIST VAR "+u.Name)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	vars := map[string]string{}
	for _, line := range lines {
		parts := strings.SplitN(line, " ", 4)
		if len(parts) == 4 && parts[0] == "VAR" && parts[1] == u.Name {
			value, unquoteErr := strconv.Unquote(parts[3])
			if unquoteErr != nil {
				value = strings.Trim(parts[3], "\"")
			}
			vars[parts[2]] = value
		}
	}
	if vars["ups.status"] == "" {
		result.Error = "lecture de ups.status impossible"
		return result
	}
	applyNUTVars(&result, vars)
	return result
}

func classifyNUTError(err error) error {
	if errors.Is(err, errNUTResponseLimit) {
		return err
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return fmt.Errorf("timeout du serveur NUT: %w", err)
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return fmt.Errorf("port NUT fermé: %w", err)
	}
	return fmt.Errorf("serveur NUT inaccessible: %w", err)
}

func nutResponseError(line string, err error) error {
	if err != nil {
		return classifyNUTError(err)
	}
	switch {
	case strings.Contains(line, "ACCESS-DENIED"):
		return errors.New("accès NUT refusé")
	case strings.Contains(line, "PASSWORD-REQUIRED"), strings.Contains(line, "USERNAME-REQUIRED"):
		return errors.New("authentification NUT nécessaire")
	case strings.Contains(line, "UNKNOWN-UPS"):
		return errors.New("UPS inaccessible ou inconnu")
	default:
		return errors.New("erreur retournée par le serveur NUT")
	}
}

func applyNUTVars(r *UPSStatus, vars map[string]string) {
	r.RawStatus = vars["ups.status"]
	switch {
	case strings.Contains(r.RawStatus, "FSD"):
		r.State = "FSD"
	case strings.Contains(r.RawStatus, "LB"):
		r.State = "LB"
	case strings.Contains(r.RawStatus, "OB"):
		r.State = "OB"
	case strings.Contains(r.RawStatus, "OL"):
		r.State = "OL"
	}
	if battery, err := strconv.ParseFloat(vars["battery.charge"], 64); err == nil && battery >= 0 && battery <= 100 {
		r.Battery = battery
		r.BatteryKnown = true
	}
	if runtime, err := strconv.Atoi(vars["battery.runtime"]); err == nil && runtime >= 0 {
		r.Runtime = runtime
		r.RuntimeKnown = true
	}
}

func readSecret(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	secret := strings.TrimSpace(string(b))
	if secret == "" {
		return "", errors.New("secret vide")
	}
	return secret, nil
}

func checkUPSPolicy(ctx context.Context, c *Config, id string) error {
	p := c.effectiveUPS(id)
	if p == nil {
		return nil
	}
	u, ok := c.UPS[p.UPS]
	if !ok {
		return fmt.Errorf("politique UPS: UPS %q inconnue", p.UPS)
	}
	s := nutStatus(ctx, p.UPS, u)
	if s.Error != "" || s.State == "unknown" {
		reason := s.Error
		if reason == "" {
			reason = "état NUT inconnu"
		}
		return upsRevalidationErrorf("politique UPS invérifiable: %s", reason)
	}
	if p.RequireOnline && s.State != "OL" {
		return upsRevalidationErrorf("politique UPS: secteur requis, état %s", s.State)
	}
	if p.MinBattery > 0 && !s.BatteryKnown {
		return upsRevalidationErrorf("politique UPS: mesure battery.charge absente ou invalide")
	}
	if p.MinBattery > 0 && s.Battery < float64(p.MinBattery) {
		return upsRevalidationErrorf("politique UPS: batterie %.0f%% < %d%%", s.Battery, p.MinBattery)
	}
	if p.MinRuntime > 0 && !s.RuntimeKnown {
		return upsRevalidationErrorf("politique UPS: mesure battery.runtime absente ou invalide")
	}
	if p.MinRuntime > 0 && s.Runtime < p.MinRuntime {
		return upsRevalidationErrorf("politique UPS: autonomie %ds < %ds", s.Runtime, p.MinRuntime)
	}
	return nil
}
