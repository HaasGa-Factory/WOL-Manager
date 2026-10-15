package main

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("durée %q invalide: %w", n.Value, err)
	}
	*d = Duration(v)
	return nil
}
func (d Duration) Value(def time.Duration) time.Duration {
	if d == 0 {
		return def
	}
	return time.Duration(d)
}
func (d Duration) MarshalYAML() (any, error) {
	return time.Duration(d).String(), nil
}

type Config struct {
	Version     int                    `yaml:"version"`
	Global      Defaults               `yaml:"global"`
	Networks    map[string]Network     `yaml:"networks"`
	Groups      map[string]Group       `yaml:"groups"`
	Servers     map[string]Server      `yaml:"servers"`
	API         APIConfig              `yaml:"api"`
	Storage     StorageConfig          `yaml:"storage"`
	Discovery   DiscoveryConfig        `yaml:"network_discovery"`
	UPS         map[string]UPSConfig   `yaml:"ups"`
	Relays      map[string]RelayConfig `yaml:"relays"`
	Automations []Automation           `yaml:"automations"`
}

type Defaults struct {
	Port           int      `yaml:"port"`
	TransportOrder []string `yaml:"transport_order"`
	Checks         []Check  `yaml:"checks"`
	WakeTimeout    Duration `yaml:"wake_timeout"`
	WakeRetries    int      `yaml:"wake_retries"`
	Delay          Duration `yaml:"delay"`
}

type Network struct {
	CIDR                   string   `yaml:"cidr"`
	Interface              string   `yaml:"interface"`
	SourceIP               string   `yaml:"source_ip"`
	AllowLocalBroadcast    bool     `yaml:"allow_local_broadcast"`
	AllowDirectedBroadcast bool     `yaml:"allow_directed_broadcast"`
	AllowUnicast           bool     `yaml:"allow_unicast"`
	Relay                  string   `yaml:"relay"`
	TransportOrder         []string `yaml:"transport_order"`
	Checks                 []Check  `yaml:"checks"`
	WakeTimeout            Duration `yaml:"wake_timeout"`
	WakeRetries            *int     `yaml:"wake_retries"`
	Delay                  Duration `yaml:"delay"`
	Port                   int      `yaml:"port"`
}

type Group struct {
	Priority       int        `yaml:"priority"`
	Servers        []string   `yaml:"servers"`
	Checks         []Check    `yaml:"checks"`
	TransportOrder []string   `yaml:"transport_order"`
	WakeTimeout    Duration   `yaml:"wake_timeout"`
	WakeRetries    *int       `yaml:"wake_retries"`
	Dependencies   []string   `yaml:"dependencies"`
	UPS            *UPSPolicy `yaml:"ups_policy"`
	Delay          Duration   `yaml:"delay"`
}

type Server struct {
	Name         string     `yaml:"name" json:"name"`
	MAC          string     `yaml:"mac" json:"mac"`
	IP           string     `yaml:"ip" json:"ip"`
	Network      string     `yaml:"network" json:"network"`
	Groups       []string   `yaml:"groups" json:"groups"`
	Dependencies []string   `yaml:"dependencies" json:"dependencies"`
	Checks       []Check    `yaml:"checks" json:"checks"`
	Transports   []string   `yaml:"transports" json:"transports"`
	WakeTimeout  Duration   `yaml:"wake_timeout" json:"-"`
	WakeRetries  *int       `yaml:"wake_retries" json:"-"`
	Delay        Duration   `yaml:"delay" json:"-"`
	UPS          *UPSPolicy `yaml:"ups_policy" json:"-"`
	Enabled      *bool      `yaml:"enabled" json:"enabled,omitempty"`
}

type Check struct {
	Type    string   `yaml:"type" json:"type"`
	Port    int      `yaml:"port,omitempty" json:"port,omitempty"`
	Timeout Duration `yaml:"timeout,omitempty" json:"-"`
}
type UPSPolicy struct {
	UPS           string `yaml:"ups"`
	RequireOnline bool   `yaml:"require_online"`
	MinBattery    int    `yaml:"min_battery"`
	MinRuntime    int    `yaml:"min_runtime_seconds"`
}
type APIConfig struct {
	Listen          string `yaml:"listen"`
	TLSCert         string `yaml:"tls_cert"`
	TLSKey          string `yaml:"tls_key"`
	TokensFile      string `yaml:"tokens_file"`
	RatePerMinute   int    `yaml:"rate_per_minute"`
	ActiveDiscovery bool   `yaml:"allow_active_discovery"`
}
type StorageConfig struct {
	StateFile string `yaml:"state_file"`
	AuditFile string `yaml:"audit_file"`
}
type DiscoveryConfig struct {
	PassiveEnabled bool       `yaml:"passive_enabled"`
	ActiveEnabled  bool       `yaml:"active_enabled"`
	AllowedSubnets []string   `yaml:"allowed_subnets"`
	Safeguards     Safeguards `yaml:"safeguards"`
	Logging        struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"logging"`
}
type Safeguards struct {
	RequireConfirmation bool     `yaml:"require_confirmation"`
	MaxPackets          int      `yaml:"max_packets"`
	RatePPS             float64  `yaml:"rate_limit_pps"`
	Timeout             Duration `yaml:"timeout"`
}
type UPSConfig struct {
	Address             string   `yaml:"address"`
	Name                string   `yaml:"name"`
	UsernameFile        string   `yaml:"username_file"`
	PasswordFile        string   `yaml:"password_file"`
	TLS                 bool     `yaml:"tls"`
	CACert              string   `yaml:"ca_cert"`
	ServerName          string   `yaml:"server_name"`
	AllowInsecureRemote bool     `yaml:"allow_insecure_remote"`
	Timeout             Duration `yaml:"timeout"`
}
type RelayConfig struct {
	URL        string   `yaml:"url"`
	CACert     string   `yaml:"ca_cert"`
	ClientCert string   `yaml:"client_cert"`
	ClientKey  string   `yaml:"client_key"`
	Timeout    Duration `yaml:"timeout"`
}
type Automation struct {
	ID        string   `yaml:"id"`
	Enabled   bool     `yaml:"enabled"`
	Event     string   `yaml:"event"`
	UPS       string   `yaml:"ups"`
	Group     string   `yaml:"group"`
	StableFor Duration `yaml:"stable_for"`
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c Config
	d := yaml.NewDecoder(strings.NewReader(string(b)))
	d.KnownFields(true)
	if err := d.Decode(&c); err != nil {
		return nil, fmt.Errorf("configuration YAML: %w", err)
	}
	c.defaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) defaults() {
	if c.Version == 0 {
		c.Version = 1
	}
	if c.Global.Port == 0 {
		c.Global.Port = 9
	}
	if c.Global.WakeTimeout == 0 {
		c.Global.WakeTimeout = Duration(45 * time.Second)
	}
	if len(c.Global.TransportOrder) == 0 {
		c.Global.TransportOrder = []string{"local_broadcast", "unicast", "directed_broadcast", "relay"}
	}
	if c.API.Listen == "" {
		c.API.Listen = "127.0.0.1:8080"
	}
	if c.API.RatePerMinute == 0 {
		c.API.RatePerMinute = 60
	}
	if c.Storage.StateFile == "" {
		c.Storage.StateFile = "/var/lib/wol-manager/state.json"
	}
	if c.Storage.AuditFile == "" {
		c.Storage.AuditFile = "/var/log/wol-manager/audit.jsonl"
	}
	if c.Discovery.Safeguards.MaxPackets == 0 {
		c.Discovery.Safeguards.MaxPackets = 20
	}
	if c.Discovery.Safeguards.RatePPS == 0 {
		c.Discovery.Safeguards.RatePPS = 2
	}
}

func (c *Config) Validate() error {
	var problems []string
	if c.Version != 1 {
		problems = append(problems, "version doit valoir 1")
	}
	if c.Global.WakeRetries < 0 {
		problems = append(problems, "global.wake_retries ne peut pas être négatif")
	}
	allowed := map[string]bool{"local_broadcast": true, "directed_broadcast": true, "unicast": true, "relay": true}
	checkChecks := func(where string, xs []Check) {
		for _, x := range xs {
			if x.Type != "tcp" && x.Type != "icmp" && x.Type != "arp" {
				problems = append(problems, where+": vérification inconnue "+x.Type)
			}
			if x.Type == "tcp" && (x.Port < 1 || x.Port > 65535) {
				problems = append(problems, where+": port TCP invalide")
			}
			if x.Timeout < 0 {
				problems = append(problems, where+": timeout négatif")
			}
		}
	}
	checkPolicy := func(where string, p *UPSPolicy) {
		if p == nil {
			return
		}
		if _, ok := c.UPS[p.UPS]; !ok {
			problems = append(problems, where+": UPS inconnue "+p.UPS)
		}
		if p.MinBattery < 0 || p.MinBattery > 100 {
			problems = append(problems, where+": min_battery hors plage")
		}
		if p.MinRuntime < 0 {
			problems = append(problems, where+": min_runtime_seconds négatif")
		}
	}
	checkOrder := func(where string, xs []string) {
		seen := map[string]bool{}
		for _, x := range xs {
			if !allowed[x] {
				problems = append(problems, where+": transport inconnu "+x)
			}
			if seen[x] {
				problems = append(problems, where+": transport dupliqué "+x)
			}
			seen[x] = true
		}
	}
	checkOrder("global.transport_order", c.Global.TransportOrder)
	checkChecks("global.checks", c.Global.Checks)
	for id, n := range c.Networks {
		if !validID(id) {
			problems = append(problems, "identifiant réseau invalide "+id)
		}
		ipnet, err := parseIPv4CIDR(n.CIDR)
		if err != nil {
			problems = append(problems, fmt.Sprintf("network %s: %v", id, err))
			continue
		}
		_ = ipnet
		if n.SourceIP != "" {
			source := net.ParseIP(n.SourceIP)
			if source == nil || source.To4() == nil || !ipnet.Contains(source) {
				problems = append(problems, "network "+id+": source_ip invalide ou hors CIDR")
			}
		}
		if n.Relay != "" {
			if _, ok := c.Relays[n.Relay]; !ok {
				problems = append(problems, "network "+id+": relais inconnu "+n.Relay)
			}
		}
		checkOrder("network "+id, n.TransportOrder)
		checkChecks("network "+id, n.Checks)
		if n.Port < 0 || n.Port > 65535 {
			problems = append(problems, "network "+id+": port invalide")
		}
		if n.WakeRetries != nil && *n.WakeRetries < 0 {
			problems = append(problems, "network "+id+": wake_retries négatif")
		}
	}
	macs := map[string]string{}
	ips := map[string]string{}
	for id, s := range c.Servers {
		if !validID(id) {
			problems = append(problems, "identifiant serveur invalide "+id)
		}
		mac, err := net.ParseMAC(s.MAC)
		if err != nil || len(mac) != 6 {
			problems = append(problems, "server "+id+": MAC invalide")
		} else if mac[0]&1 != 0 || mac.String() == "00:00:00:00:00:00" {
			problems = append(problems, "server "+id+": MAC non unicast invalide")
		} else if old := macs[mac.String()]; old != "" {
			problems = append(problems, "MAC dupliquée: "+old+" et "+id)
		} else {
			macs[mac.String()] = id
		}
		ip := net.ParseIP(s.IP)
		if ip == nil || ip.To4() == nil {
			problems = append(problems, "server "+id+": IPv4 invalide")
		} else if old := ips[ip.String()]; old != "" {
			problems = append(problems, "IP dupliquée: "+old+" et "+id)
		} else {
			ips[ip.String()] = id
		}
		if _, ok := c.Networks[s.Network]; !ok {
			problems = append(problems, "server "+id+": réseau inconnu "+s.Network)
		} else if _, ipnet, e := net.ParseCIDR(c.Networks[s.Network].CIDR); e == nil && !ipnet.Contains(ip) {
			problems = append(problems, "server "+id+": IP hors du réseau "+s.Network)
		}
		for _, g := range s.Groups {
			if _, ok := c.Groups[g]; !ok {
				problems = append(problems, "server "+id+": groupe inconnu "+g)
			}
		}
		for _, d := range s.Dependencies {
			if _, ok := c.Servers[d]; !ok {
				problems = append(problems, "server "+id+": dépendance inconnue "+d)
			}
		}
		checkOrder("server "+id, s.Transports)
		checkChecks("server "+id, s.Checks)
		checkPolicy("server "+id, s.UPS)
		if s.WakeRetries != nil && *s.WakeRetries < 0 {
			problems = append(problems, "server "+id+": wake_retries négatif")
		}
	}
	priorities := map[int]string{}
	for id, g := range c.Groups {
		if !validID(id) {
			problems = append(problems, "identifiant groupe invalide "+id)
		}
		if old := priorities[g.Priority]; old != "" {
			problems = append(problems, fmt.Sprintf("groupes %s et %s: priorité %d dupliquée", old, id, g.Priority))
		} else {
			priorities[g.Priority] = id
		}
		for _, s := range g.Servers {
			if _, ok := c.Servers[s]; !ok {
				problems = append(problems, "group "+id+": serveur inconnu "+s)
			}
		}
		for _, d := range g.Dependencies {
			if _, ok := c.Servers[d]; !ok {
				problems = append(problems, "group "+id+": dépendance inconnue "+d)
			}
		}
		checkOrder("group "+id, g.TransportOrder)
		checkChecks("group "+id, g.Checks)
		checkPolicy("group "+id, g.UPS)
		if g.WakeRetries != nil && *g.WakeRetries < 0 {
			problems = append(problems, "group "+id+": wake_retries négatif")
		}
	}
	for id, r := range c.Relays {
		if !validID(id) {
			problems = append(problems, "identifiant relais invalide "+id)
		}
		u, parseErr := url.Parse(r.URL)
		if parseErr != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			problems = append(problems, "relay "+id+": URL HTTPS obligatoire")
		}
		if r.CACert == "" || r.ClientCert == "" || r.ClientKey == "" {
			problems = append(problems, "relay "+id+": certificats mTLS requis")
		}
	}
	for id, u := range c.UPS {
		if !validID(id) {
			problems = append(problems, "identifiant UPS invalide "+id)
		}
		host, _, err := net.SplitHostPort(u.Address)
		if err != nil || u.Name == "" {
			problems = append(problems, "ups "+id+": address host:port et name requis")
			continue
		}
		ip := net.ParseIP(host)
		local := host == "localhost" || (ip != nil && ip.IsLoopback())
		if !local && !u.TLS && !u.AllowInsecureRemote {
			problems = append(problems, "ups "+id+": STARTTLS requis hors loopback (ou allow_insecure_remote explicite pour un tunnel protégé)")
		}
	}
	if c.Discovery.ActiveEnabled {
		if c.Discovery.Safeguards.MaxPackets < 1 {
			problems = append(problems, "network_discovery: max_packets doit être positif")
		}
		if c.Discovery.Safeguards.RatePPS <= 0 {
			problems = append(problems, "network_discovery: rate_limit_pps doit être positif")
		}
		if c.Discovery.Safeguards.Timeout.Value(0) <= 0 {
			problems = append(problems, "network_discovery: timeout doit être positif")
		}
	}
	for _, subnet := range c.Discovery.AllowedSubnets {
		ip, _, err := net.ParseCIDR(subnet)
		if err != nil || ip.To4() == nil {
			problems = append(problems, "network_discovery: sous-réseau autorisé invalide "+subnet)
		} else if !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
			problems = append(problems, "network_discovery: réseau public interdit "+subnet)
		}
	}
	if !isLoopbackListen(c.API.Listen) && (c.API.TLSCert == "" || c.API.TLSKey == "") {
		problems = append(problems, "api: TLS obligatoire hors loopback")
	}
	if _, _, err := net.SplitHostPort(c.API.Listen); err != nil {
		problems = append(problems, "api: listen doit être host:port")
	}
	if c.API.RatePerMinute < 1 {
		problems = append(problems, "api: rate_per_minute doit être positif")
	}
	for _, a := range c.Automations {
		if a.Event != "nut.power_restored" {
			problems = append(problems, "automation "+a.ID+": événement non pris en charge")
		}
		if _, ok := c.Groups[a.Group]; !ok {
			problems = append(problems, "automation "+a.ID+": groupe inconnu")
		}
		if _, ok := c.UPS[a.UPS]; !ok {
			problems = append(problems, "automation "+a.ID+": UPS inconnue")
		}
	}
	if cycle := c.dependencyCycle(); len(cycle) > 0 {
		problems = append(problems, "dépendance circulaire: "+strings.Join(cycle, " -> "))
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func validID(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return false
		}
	}
	return true
}

func parseIPv4CIDR(s string) (*net.IPNet, error) {
	ip, n, err := net.ParseCIDR(s)
	if err != nil || ip.To4() == nil {
		return nil, fmt.Errorf("CIDR IPv4 invalide %q", s)
	}
	n.IP = ip.Mask(n.Mask)
	return n, nil
}
func isLoopbackListen(s string) bool {
	host, _, err := net.SplitHostPort(s)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}
func enabled(s Server) bool { return s.Enabled == nil || *s.Enabled }

func (c *Config) groupsFor(id string) []Group {
	var out []Group
	for gid, g := range c.Groups {
		listed := false
		for _, x := range g.Servers {
			if x == id {
				listed = true
			}
		}
		for _, x := range c.Servers[id].Groups {
			if x == gid {
				listed = true
			}
		}
		if listed {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Priority < out[j].Priority })
	return out
}
func (c *Config) effectiveChecks(id string) []Check {
	xs := c.Global.Checks
	if n := c.Networks[c.Servers[id].Network]; len(n.Checks) > 0 {
		xs = n.Checks
	}
	for _, g := range c.groupsFor(id) {
		if len(g.Checks) > 0 {
			xs = g.Checks
		}
	}
	if len(c.Servers[id].Checks) > 0 {
		xs = c.Servers[id].Checks
	}
	return xs
}

func (c *Config) effectiveTransportOrder(id string) []string {
	s := c.Servers[id]
	xs := c.Global.TransportOrder
	if n := c.Networks[s.Network]; len(n.TransportOrder) > 0 {
		xs = n.TransportOrder
	}
	for _, g := range c.groupsFor(id) {
		if len(g.TransportOrder) > 0 {
			xs = g.TransportOrder
		}
	}
	if len(s.Transports) > 0 {
		xs = s.Transports
	}
	return xs
}

func (c *Config) effectiveTiming(id string) (time.Duration, int, time.Duration) {
	s := c.Servers[id]
	timeout := c.Global.WakeTimeout.Value(45 * time.Second)
	retries := c.Global.WakeRetries
	delay := c.Global.Delay.Value(0)
	n := c.Networks[s.Network]
	if n.WakeTimeout != 0 {
		timeout = time.Duration(n.WakeTimeout)
	}
	if n.WakeRetries != nil {
		retries = *n.WakeRetries
	}
	if n.Delay != 0 {
		delay = time.Duration(n.Delay)
	}
	for _, g := range c.groupsFor(id) {
		if g.WakeTimeout != 0 {
			timeout = time.Duration(g.WakeTimeout)
		}
		if g.WakeRetries != nil {
			retries = *g.WakeRetries
		}
		if g.Delay != 0 {
			delay = time.Duration(g.Delay)
		}
	}
	if s.WakeTimeout != 0 {
		timeout = time.Duration(s.WakeTimeout)
	}
	if s.WakeRetries != nil {
		retries = *s.WakeRetries
	}
	if s.Delay != 0 {
		delay = time.Duration(s.Delay)
	}
	return timeout, retries, delay
}
func (c *Config) effectiveUPS(id string) *UPSPolicy {
	var p *UPSPolicy
	for _, g := range c.groupsFor(id) {
		if g.UPS != nil {
			q := *g.UPS
			p = &q
		}
	}
	if c.Servers[id].UPS != nil {
		q := *c.Servers[id].UPS
		p = &q
	}
	return p
}
func (c *Config) dependencies(id string) []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range c.groupsFor(id) {
		for _, x := range g.Dependencies {
			if !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	for _, x := range c.Servers[id].Dependencies {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}
func (c *Config) dependencyCycle() []string {
	state := map[string]int{}
	stack := []string{}
	var cycle []string
	var visit func(string) bool
	visit = func(id string) bool {
		state[id] = 1
		stack = append(stack, id)
		for _, d := range c.dependencies(id) {
			if state[d] == 1 {
				for i, x := range stack {
					if x == d {
						cycle = append(append([]string{}, stack[i:]...), d)
						return true
					}
				}
			}
			if state[d] == 0 && visit(d) {
				return true
			}
		}
		stack = stack[:len(stack)-1]
		state[id] = 2
		return false
	}
	for id := range c.Servers {
		if state[id] == 0 && visit(id) {
			break
		}
	}
	return cycle
}
