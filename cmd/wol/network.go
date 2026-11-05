package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"
)

type TransportDecision struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Reason      string `json:"reason"`
	Destination string `json:"destination,omitempty"`
	Interface   string `json:"interface,omitempty"`
	Source      string `json:"source,omitempty"`
	Relay       string `json:"relay,omitempty"`
}

type localBroadcastTarget struct {
	Interface   string
	Source      string
	Destination string
}

var localBroadcastResolver = resolveLocalBroadcast

func transportPlan(c *Config, id string) ([]TransportDecision, error) {
	s, ok := c.Servers[id]
	if !ok {
		return nil, fmt.Errorf("serveur inconnu %q", id)
	}
	n := c.Networks[s.Network]
	order := c.effectiveTransportOrder(id)
	ipnet, err := parseIPv4CIDR(n.CIDR)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(s.IP).To4()
	if !ipnet.Contains(ip) {
		return nil, fmt.Errorf("serveur %s: IP hors du réseau %s", id, n.CIDR)
	}
	port := n.Port
	if port == 0 {
		port = c.Global.Port
	}
	var out []TransportDecision
	selected := false
	for _, name := range order {
		d := TransportDecision{Name: name, State: "unavailable"}
		switch name {
		case "local_broadcast":
			if !n.AllowLocalBroadcast {
				d.State = "disabled"
				d.Reason = "non autorisé par le réseau"
			} else {
				target, targetErr := localBroadcastResolver(n, port)
				if targetErr != nil {
					d.Reason = targetErr.Error()
				} else {
					d.Destination = target.Destination
					d.Interface = target.Interface
					d.Source = target.Source
					d.Reason = "interface " + target.Interface + ", source " + target.Source
					d.State = "candidate"
				}
			}
		case "directed_broadcast":
			if !n.AllowDirectedBroadcast {
				d.State = "not_authorized"
				d.Reason = "autorisation explicite absente"
			} else {
				d.Destination = fmt.Sprintf("%s:%d", broadcastIP(ipnet), port)
				d.Reason = "broadcast dirigé explicitement autorisé"
				d.State = "candidate"
			}
		case "relay":
			if n.Relay == "" {
				d.Reason = "aucun relais configuré"
			} else if _, ok := c.Relays[n.Relay]; !ok {
				d.Reason = "relais inconnu"
			} else {
				d.Relay = n.Relay
				d.Reason = "relais autorisé pour ce réseau"
				d.State = "candidate"
			}
		}
		if d.State == "candidate" && !selected {
			d.State = "selected"
			selected = true
		}
		out = append(out, d)
	}
	if !selected {
		return out, fmt.Errorf("aucun transport autorisé et disponible pour %s", id)
	}
	return out, nil
}

func broadcastIP(n *net.IPNet) net.IP {
	ip := n.IP.To4()
	out := make(net.IP, 4)
	for i := 0; i < 4; i++ {
		out[i] = ip[i] | ^n.Mask[i]
	}
	return out
}

func resolveLocalBroadcast(n Network, port int) (localBroadcastTarget, error) {
	if n.Interface == "" {
		return localBroadcastTarget{}, errors.New("interface explicite requise pour le broadcast local")
	}
	configured, err := parseIPv4CIDR(n.CIDR)
	if err != nil {
		return localBroadcastTarget{}, err
	}
	ones, bits := configured.Mask.Size()
	if bits != 32 || ones > 30 {
		return localBroadcastTarget{}, fmt.Errorf("CIDR %s sans adresse de broadcast utilisable", n.CIDR)
	}
	iface, err := net.InterfaceByName(n.Interface)
	if err != nil {
		return localBroadcastTarget{}, fmt.Errorf("interface %s indisponible", n.Interface)
	}
	if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
		return localBroadcastTarget{}, fmt.Errorf("interface %s non disponible pour le broadcast", n.Interface)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return localBroadcastTarget{}, fmt.Errorf("adresses de l'interface %s: %w", n.Interface, err)
	}
	for _, addr := range addrs {
		ip, actual, parseErr := net.ParseCIDR(addr.String())
		if parseErr != nil || ip.To4() == nil || !configured.Contains(ip) {
			continue
		}
		if n.SourceIP != "" && !ip.Equal(net.ParseIP(n.SourceIP)) {
			continue
		}
		if actual.String() != configured.String() {
			return localBroadcastTarget{}, fmt.Errorf("CIDR %s différent du réseau réel %s de %s", configured, actual, n.Interface)
		}
		return localBroadcastTarget{
			Interface:   n.Interface,
			Source:      ip.String(),
			Destination: net.JoinHostPort(broadcastIP(actual).String(), fmt.Sprint(port)),
		}, nil
	}
	return localBroadcastTarget{}, fmt.Errorf("interface %s sans adresse IPv4 autorisée dans %s", n.Interface, n.CIDR)
}

func sourceForNetwork(n Network) string {
	if n.SourceIP != "" {
		return n.SourceIP
	}
	if n.Interface == "" {
		return ""
	}
	ipnet, err := parseIPv4CIDR(n.CIDR)
	if err != nil {
		return ""
	}
	iface, err := net.InterfaceByName(n.Interface)
	if err != nil {
		return ""
	}
	addrs, _ := iface.Addrs()
	for _, a := range addrs {
		ip, _, e := net.ParseCIDR(a.String())
		if e == nil && ip.To4() != nil && ipnet.Contains(ip) {
			return ip.String()
		}
	}
	return ""
}

type DiscoveryReport struct {
	Time       time.Time       `json:"time"`
	Active     bool            `json:"active"`
	Interfaces any             `json:"interfaces"`
	Routes     json.RawMessage `json:"routes,omitempty"`
	Neighbors  json.RawMessage `json:"neighbors,omitempty"`
	Tests      []ActiveTest    `json:"tests,omitempty"`
	Warnings   []string        `json:"warnings,omitempty"`
}
type ActiveTest struct {
	Network      string `json:"network"`
	Kind         string `json:"kind"`
	Target       string `json:"target"`
	Connectivity string `json:"connectivity"`
	Packet       string `json:"packet"`
	Reception    string `json:"reception"`
	Wake         string `json:"wake"`
	Detail       string `json:"detail,omitempty"`
}

func passiveDiscovery(ctx context.Context) DiscoveryReport {
	r := DiscoveryReport{Time: time.Now().UTC()}
	type ifaceInfo struct {
		Name      string   `json:"name"`
		Flags     string   `json:"flags"`
		Hardware  string   `json:"hardware,omitempty"`
		Addresses []string `json:"addresses"`
	}
	var items []ifaceInfo
	ifaces, err := net.Interfaces()
	if err != nil {
		r.Warnings = append(r.Warnings, err.Error())
	}
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		x := ifaceInfo{Name: i.Name, Flags: i.Flags.String(), Hardware: i.HardwareAddr.String()}
		for _, a := range addrs {
			x.Addresses = append(x.Addresses, a.String())
		}
		items = append(items, x)
	}
	r.Interfaces = items
	if runtime.GOOS == "linux" {
		if b, e := fixedCommand(ctx, "ip", "-j", "route", "show"); e == nil && json.Valid(b) {
			r.Routes = b
		} else if e != nil {
			r.Warnings = append(r.Warnings, "routes: "+e.Error())
		}
		if b, e := fixedCommand(ctx, "ip", "-j", "neigh", "show"); e == nil && json.Valid(b) {
			r.Neighbors = b
		} else if e != nil {
			r.Warnings = append(r.Warnings, "voisins: "+e.Error())
		}
	} else {
		r.Warnings = append(r.Warnings, "les routes et voisins détaillés nécessitent Linux/iproute2")
	}
	return r
}
func fixedCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = []string{"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "LANG=C"}
	return cmd.Output()
}

func activeDiscovery(ctx context.Context, c *Config, dry bool) DiscoveryReport {
	r := passiveDiscovery(ctx)
	r.Active = true
	budget := c.Discovery.Safeguards.MaxPackets
	delay := time.Duration(float64(time.Second) / c.Discovery.Safeguards.RatePPS)
	allowed := func(n *net.IPNet) bool {
		for _, x := range c.Discovery.AllowedSubnets {
			_, a, e := net.ParseCIDR(x)
			if e == nil && a.String() == n.String() {
				return true
			}
		}
		return false
	}
	keys := make([]string, 0, len(c.Networks))
	for k := range c.Networks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if budget <= 0 {
			r.Warnings = append(r.Warnings, "budget maximal de paquets atteint")
			break
		}
		n := c.Networks[id]
		ipnet, e := parseIPv4CIDR(n.CIDR)
		if e != nil || !allowed(ipnet) {
			r.Tests = append(r.Tests, ActiveTest{Network: id, Kind: "guard", Target: n.CIDR, Connectivity: "not_tested", Packet: "blocked", Reception: "unknown", Wake: "unknown", Detail: "sous-réseau non autorisé"})
			continue
		}
		t := ActiveTest{Network: id, Kind: "route", Target: n.CIDR, Reception: "not_instrumented", Wake: "not_tested"}
		if dry {
			t.Connectivity = "not_tested"
			t.Packet = "dry_run"
		} else {
			target := firstHost(ipnet)
			conn := net.Dialer{Timeout: time.Second}
			x, err := conn.DialContext(ctx, "udp", net.JoinHostPort(target.String(), "9"))
			if err == nil {
				x.Close()
				t.Connectivity = "route_available"
				t.Packet = "udp_socket_opened"
			} else {
				t.Connectivity = "failed"
				t.Packet = "not_sent"
				t.Detail = err.Error()
			}
			select {
			case <-ctx.Done():
				t.Detail = "interrompu: " + ctx.Err().Error()
				r.Tests = append(r.Tests, t)
				return r
			case <-time.After(delay):
			}
		}
		r.Tests = append(r.Tests, t)
	}
	serverIDs := make([]string, 0, len(c.Servers))
	for id := range c.Servers {
		serverIDs = append(serverIDs, id)
	}
	sort.Strings(serverIDs)
	for _, id := range serverIDs {
		if budget <= 0 {
			r.Warnings = append(r.Warnings, "budget maximal de paquets atteint")
			break
		}
		s := c.Servers[id]
		netCfg := c.Networks[s.Network]
		ipnet, err := parseIPv4CIDR(netCfg.CIDR)
		if err != nil || !allowed(ipnet) {
			continue
		}
		plan, err := transportPlan(c, id)
		if err != nil {
			r.Tests = append(r.Tests, ActiveTest{Network: s.Network, Kind: "transport", Target: id, Connectivity: "unknown", Packet: "blocked", Reception: "unknown", Wake: "unknown", Detail: err.Error()})
			continue
		}
		mac, _ := net.ParseMAC(s.MAC)
		for _, d := range plan {
			if d.State != "selected" && d.State != "candidate" {
				continue
			}
			t := ActiveTest{Network: s.Network, Kind: d.Name, Target: id, Connectivity: "route_available", Reception: "not_instrumented", Wake: "not_tested"}
			if dry {
				t.Packet = "dry_run"
			} else if d.Name == "relay" {
				t.Packet = "not_sent"
				budget--
				if err = relayHealth(ctx, c.Relays[d.Relay]); err != nil {
					t.Connectivity = "failed"
					t.Detail = err.Error()
				} else {
					t.Connectivity = "relay_confirmed"
					t.Reception = "relay_health_confirmed"
				}
			} else {
				source := sourceForNetwork(netCfg)
				if d.Name == "local_broadcast" {
					source = d.Source
				}
				err = sendMagic(d.Destination, source, true, magicPacket(mac))
				budget--
				if err != nil {
					t.Packet = "failed"
					t.Detail = err.Error()
				} else {
					t.Packet = "socket_accepted"
					status := checkServer(ctx, c, id)
					if status.State == "reachable" {
						t.Wake = "observed_reachable"
					} else {
						t.Wake = "not_observed"
					}
				}
			}
			if !dry {
				select {
				case <-ctx.Done():
					t.Detail = "interrompu: " + ctx.Err().Error()
					r.Tests = append(r.Tests, t)
					return r
				case <-time.After(delay):
				}
			}
			r.Tests = append(r.Tests, t)
			if budget <= 0 {
				break
			}
		}
	}
	return r
}
func firstHost(n *net.IPNet) net.IP { ip := append(net.IP(nil), n.IP.To4()...); ip[3]++; return ip }

func readNeighbors() map[string]string {
	out := map[string]string{}
	f, e := os.Open("/proc/net/arp")
	if e != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Scan()
	for s.Scan() {
		p := strings.Fields(s.Text())
		if len(p) >= 4 {
			out[p[0]] = p[3]
		}
	}
	return out
}
func magicPacket(mac net.HardwareAddr) []byte {
	b := bytes.Repeat([]byte{0xff}, 6)
	for i := 0; i < 16; i++ {
		b = append(b, mac...)
	}
	return b
}
