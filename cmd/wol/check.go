package main

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type CheckResult struct {
	Method     string `json:"method"`
	State      string `json:"state"`
	Detail     string `json:"detail,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}
type StatusResult struct {
	Server  string        `json:"server"`
	State   string        `json:"state"`
	Results []CheckResult `json:"results"`
	Note    string        `json:"note,omitempty"`
}

func checkServer(ctx context.Context, c *Config, id string) StatusResult {
	s, ok := c.Servers[id]
	if !ok {
		return StatusResult{Server: id, State: "check_impossible", Note: "serveur inconnu"}
	}
	checks := c.effectiveChecks(id)
	if len(checks) == 0 {
		checks = []Check{{Type: "tcp", Port: 22, Timeout: Duration(2 * time.Second)}}
	}
	r := StatusResult{Server: id}
	reachable, unreachable, unknown, impossible := false, false, false, false
	for _, ch := range checks {
		start := time.Now()
		x := CheckResult{Method: ch.Type}
		timeout := ch.Timeout.Value(2 * time.Second)
		cc, cancel := context.WithTimeout(ctx, timeout)
		switch ch.Type {
		case "tcp":
			if ch.Port < 1 || ch.Port > 65535 {
				x.State = "check_impossible"
				x.Detail = "port invalide"
			} else {
				cn, e := (&net.Dialer{}).DialContext(cc, "tcp", net.JoinHostPort(s.IP, fmt.Sprint(ch.Port)))
				if e == nil {
					cn.Close()
					x.State = "reachable"
				} else {
					x.State = "unreachable"
					x.Detail = e.Error()
				}
			}
		case "icmp":
			e := ping(cc, s.IP, timeout)
			if e == nil {
				x.State = "reachable"
			} else {
				x.State = "unreachable"
				x.Detail = e.Error()
			}
		case "arp":
			mac := readNeighbors()[s.IP]
			if mac != "" {
				x.State = "unknown"
				x.Detail = "entrée voisine présente (ne prouve pas que la machine est allumée): " + mac
			} else {
				x.State = "unknown"
				x.Detail = "aucune entrée voisine connue"
			}
		default:
			x.State = "check_impossible"
			x.Detail = "méthode inconnue"
		}
		cancel()
		x.DurationMS = time.Since(start).Milliseconds()
		r.Results = append(r.Results, x)
		switch x.State {
		case "reachable":
			reachable = true
		case "unreachable":
			unreachable = true
		case "unknown":
			unknown = true
		default:
			impossible = true
		}
	}
	if c.effectiveCheckLogic(id) == "all" {
		switch {
		case unreachable:
			r.State = "unreachable"
		case impossible:
			r.State = "check_impossible"
		case unknown:
			r.State = "unknown"
		case reachable && len(r.Results) > 0:
			r.State = "reachable"
		default:
			r.State = "check_impossible"
		}
	} else {
		switch {
		case reachable:
			r.State = "reachable"
		case unknown:
			r.State = "unknown"
		case impossible:
			r.State = "check_impossible"
		case unreachable:
			r.State = "unreachable"
		default:
			r.State = "check_impossible"
		}
	}
	return r
}
func ping(ctx context.Context, ip string, timeout time.Duration) error {
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-n", "1", "-w", fmt.Sprint(timeout.Milliseconds()), ip}
	} else {
		secs := int(timeout.Seconds())
		if secs < 1 {
			secs = 1
		}
		args = []string{"-c", "1", "-W", fmt.Sprint(secs), ip}
	}
	cmd := exec.CommandContext(ctx, "ping", args...)
	if b, e := cmd.CombinedOutput(); e != nil {
		return fmt.Errorf("ping: %s", strings.TrimSpace(string(b)))
	}
	return nil
}
