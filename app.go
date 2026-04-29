package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

type PortRecord struct {
	Port      int    `json:"port"`
	Service   string `json:"service"`
	Protocol  string `json:"protocol"`
	Reserved  bool   `json:"reserved"`
	Allocated bool   `json:"allocated"`
	Used      bool   `json:"used"`
	Source    string `json:"source"`
	Notes     string `json:"notes"`
	UpdatedAt string `json:"updated_at"`
}

type App struct {
	cfg      Config
	cfgPath  string
	mu       sync.RWMutex
	records  map[string]PortRecord
	template *template.Template
}

func NewApp(cfg Config) *App {
	app := &App{
		cfg:     cfg,
		cfgPath: "config.json",
		records: map[string]PortRecord{},
	}
	tpl, err := loadTemplate("index.html")
	if err != nil {
		panic(err)
	}
	app.template = tpl
	app.load()
	return app
}

func (a *App) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", a.handleIndex)
	mux.HandleFunc("/api/ports", a.handlePorts)
	mux.HandleFunc("/api/ports/", a.handlePortByID)
	mux.HandleFunc("/api/scan", a.handleScan)
	mux.HandleFunc("/api/allocated", a.handleAllocated)
	mux.HandleFunc("/api/allocate", a.handleAllocate)
	mux.HandleFunc("/api/allocate/manual", a.handleManualAllocate)
	mux.HandleFunc("/api/config/range", a.handleRangeConfig)
	mux.HandleFunc("/api/ports/release/", a.handleReleasePort)
	return http.ListenAndServe(a.cfg.ServerAddr, mux)
}

func (a *App) load() {
	data, err := os.ReadFile(a.cfg.DataFile)
	if err != nil {
		if os.IsNotExist(err) {
			_ = a.save()
		}
		return
	}
	var list []PortRecord
	if err := json.Unmarshal(data, &list); err != nil {
		return
	}
	for _, r := range list {
		a.records[recordKey(r.Port, r.Protocol)] = r
	}
}

func (a *App) save() error {
	list := a.listRecords()
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.cfg.DataFile, data, 0644)
}

func (a *App) listRecords() []PortRecord {
	a.mu.RLock()
	defer a.mu.RUnlock()
	list := make([]PortRecord, 0, len(a.records))
	for _, r := range a.records {
		list = append(list, r)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Port == list[j].Port {
			return list[i].Protocol < list[j].Protocol
		}
		return list[i].Port < list[j].Port
	})
	return list
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = a.template.Execute(w, map[string]string{
		"AppName":      a.cfg.AppName,
		"TcpPortRange": a.cfg.TcpPortRange,
		"UdpPortRange": a.cfg.UdpPortRange,
	})
}

func (a *App) handlePorts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.writeJSON(w, http.StatusOK, map[string]any{"data": a.listRecords()})
	case http.MethodPost:
		var req PortRecord
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.Protocol = normalizeProtocol(req.Protocol)
		if req.Port <= 0 || req.Port > 65535 {
			a.writeError(w, http.StatusBadRequest, "invalid port")
			return
		}
		if req.Service == "" {
			a.writeError(w, http.StatusBadRequest, "service required")
			return
		}
		a.mu.Lock()
		req.UpdatedAt = nowString()
		a.records[recordKey(req.Port, req.Protocol)] = req
		a.mu.Unlock()
		_ = a.save()
		a.writeJSON(w, http.StatusCreated, req)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) handlePortByID(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/ports/"))
	if err != nil {
		a.writeError(w, http.StatusBadRequest, "invalid port id")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req PortRecord
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.Port = id
		req.Protocol = normalizeProtocol(req.Protocol)
		req.UpdatedAt = nowString()
		a.mu.Lock()
		delete(a.records, recordKey(id, "tcp"))
		delete(a.records, recordKey(id, "udp"))
		a.records[recordKey(id, req.Protocol)] = req
		a.mu.Unlock()
		_ = a.save()
		a.writeJSON(w, http.StatusOK, req)
	case http.MethodDelete:
		a.mu.Lock()
		delete(a.records, recordKey(id, "tcp"))
		delete(a.records, recordKey(id, "udp"))
		a.mu.Unlock()
		_ = a.save()
		a.writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) handleScan(w http.ResponseWriter, r *http.Request) {
	list := a.scanSystemPorts()
	a.writeJSON(w, http.StatusOK, map[string]any{"data": list})
}

func (a *App) handleAllocated(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	list := make([]PortRecord, 0)
	for _, rec := range a.records {
		if rec.Allocated && !rec.Used {
			list = append(list, rec)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Port == list[j].Port {
			return list[i].Protocol < list[j].Protocol
		}
		return list[i].Port < list[j].Port
	})
	a.writeJSON(w, http.StatusOK, map[string]any{"data": list})
}

func (a *App) handleRangeConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.writeJSON(w, http.StatusOK, map[string]string{"tcp_port_range": a.cfg.TcpPortRange, "udp_port_range": a.cfg.UdpPortRange})
	case http.MethodPut:
		var req struct {
			PortRange string `json:"port_range"`
			Protocol  string `json:"protocol"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			a.writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if min, max := parsePortRange(req.PortRange); min == 0 || max == 0 {
			a.writeError(w, http.StatusBadRequest, "invalid port range")
			return
		}
		if normalizeProtocol(req.Protocol) == "udp" {
			a.cfg.UdpPortRange = req.PortRange
		} else {
			a.cfg.TcpPortRange = req.PortRange
		}
		if err := a.saveConfig(); err != nil {
			a.writeError(w, http.StatusInternalServerError, "save config failed")
			return
		}
		a.writeJSON(w, http.StatusOK, map[string]string{"tcp_port_range": a.cfg.TcpPortRange, "udp_port_range": a.cfg.UdpPortRange})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (a *App) handleReleasePort(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/ports/release/"))
	if err != nil {
		a.writeError(w, http.StatusBadRequest, "invalid port id")
		return
	}
	protocol := normalizeProtocol(r.URL.Query().Get("protocol"))
	a.mu.Lock()
	delete(a.records, recordKey(id, "tcp"))
	delete(a.records, recordKey(id, "udp"))
	if protocol != "" {
		delete(a.records, recordKey(id, protocol))
	}
	a.mu.Unlock()
	_ = a.save()
	a.writeJSON(w, http.StatusOK, map[string]string{"message": "released"})
}

func (a *App) handleAllocate(w http.ResponseWriter, r *http.Request) {
	service := strings.TrimSpace(r.URL.Query().Get("service"))
	protocol := normalizeProtocol(r.URL.Query().Get("protocol"))
	if service == "" {
		a.writeError(w, http.StatusBadRequest, "service required")
		return
	}
	minPort, maxPort := parsePortRange(getPortRange(a.cfg, protocol))
	if minPort == 0 || maxPort == 0 {
		a.writeError(w, http.StatusBadRequest, "invalid port range")
		return
	}
	port := a.allocatePortInRange(minPort, maxPort)
	if port == 0 {
		a.writeError(w, http.StatusConflict, "no available port in range")
		return
	}
	rec := PortRecord{Port: port, Service: service, Protocol: protocol, Reserved: true, Allocated: true, Used: isPortInUse(port), Source: "allocated", UpdatedAt: nowString()}
	a.mu.Lock()
	a.records[recordKey(port, protocol)] = rec
	a.mu.Unlock()
	_ = a.save()
	a.writeJSON(w, http.StatusOK, rec)
}

func (a *App) handleManualAllocate(w http.ResponseWriter, r *http.Request) {
	service := strings.TrimSpace(r.URL.Query().Get("service"))
	protocol := normalizeProtocol(r.URL.Query().Get("protocol"))
	if service == "" {
		a.writeError(w, http.StatusBadRequest, "service required")
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("port")))
	if err != nil || port <= 0 || port > 65535 {
		a.writeError(w, http.StatusBadRequest, "invalid port")
		return
	}
	minPort, maxPort := parsePortRange(getPortRange(a.cfg, protocol))
	if minPort == 0 || maxPort == 0 || port < minPort || port > maxPort {
		a.writeError(w, http.StatusBadRequest, "port out of range")
		return
	}
	if isPortInUse(port) {
		a.writeError(w, http.StatusConflict, "port already in use")
		return
	}
	a.mu.RLock()
	_, exists := a.records[recordKey(port, protocol)]
	a.mu.RUnlock()
	if exists {
		a.writeError(w, http.StatusConflict, "port already allocated")
		return
	}
	rec := PortRecord{Port: port, Service: service, Protocol: protocol, Reserved: true, Allocated: true, Used: false, Source: "manual", UpdatedAt: nowString()}
	a.mu.Lock()
	a.records[recordKey(port, protocol)] = rec
	a.mu.Unlock()
	_ = a.save()
	a.writeJSON(w, http.StatusOK, rec)
}

func parsePortRange(value string) (int, int) {
	parts := strings.Split(value, "-")
	if len(parts) != 2 {
		return 0, 0
	}
	minPort, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	maxPort, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || minPort <= 0 || maxPort <= 0 || minPort > maxPort {
		return 0, 0
	}
	return minPort, maxPort
}

func getPortRange(cfg Config, protocol string) string {
	if normalizeProtocol(protocol) == "udp" {
		return cfg.UdpPortRange
	}
	return cfg.TcpPortRange
}

func (a *App) allocatePortInRange(minPort, maxPort int) int {
	for p := minPort; p <= maxPort; p++ {
		if _, ok := a.records[recordKey(p, "tcp")]; ok {
			continue
		}
		if _, ok := a.records[recordKey(p, "udp")]; ok {
			continue
		}
		if isPortInUse(p) {
			continue
		}
		return p
	}
	return 0
}

func (a *App) scanSystemPorts() []PortRecord {
	list := make([]PortRecord, 0)
	for _, portInfo := range scanSystemListeningPorts() {
		service := portInfo.Service
		if service == "" {
			service = portLabel(portInfo.Port)
		}
		if name, ok := defaultKnownPorts()[portInfo.Port]; ok {
			service = name
		}
		list = append(list, PortRecord{
			Port:      portInfo.Port,
			Service:   service,
			Protocol:  portInfo.Protocol,
			Reserved:  false,
			Allocated: false,
			Used:      true,
			Source:    portInfo.Source,
			UpdatedAt: nowString(),
		})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Port == list[j].Port {
			return list[i].Protocol < list[j].Protocol
		}
		return list[i].Port < list[j].Port
	})
	return list
}

type systemPortInfo struct {
	Port     int
	Protocol string
	Service  string
	Source   string
}

func scanSystemListeningPorts() []systemPortInfo {
	if runtime.GOOS == "windows" {
		return scanWithNetstatWindows()
	}
	return scanWithSsLinux()
}

func scanWithNetstatWindows() []systemPortInfo {
	out, err := exec.Command("netstat", "-ano").Output()
	if err != nil {
		return nil
	}
	lines := bytes.Split(out, []byte("\n"))
	items := make([]systemPortInfo, 0)
	for _, line := range lines {
		text := strings.TrimSpace(string(line))
		if !strings.HasPrefix(text, "TCP") && !strings.HasPrefix(text, "UDP") {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) < 2 {
			continue
		}
		protocol := strings.ToLower(fields[0])
		local := fields[1]
		port := parsePortFromEndpoint(local)
		if port == 0 {
			continue
		}
		items = append(items, systemPortInfo{Port: port, Protocol: protocol, Source: "netstat"})
	}
	return dedupeSystemPorts(items)
}

func scanWithSsLinux() []systemPortInfo {
	out, err := exec.Command("sh", "-c", "ss -lntuH 2>/dev/null || netstat -lntu 2>/dev/null").Output()
	if err != nil {
		return nil
	}
	lines := bytes.Split(out, []byte("\n"))
	items := make([]systemPortInfo, 0)
	for _, line := range lines {
		text := strings.TrimSpace(string(line))
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) < 5 {
			continue
		}
		local := fields[4]
		if runtime.GOOS == "darwin" && len(fields) >= 4 {
			local = fields[len(fields)-2]
		}
		port := parsePortFromEndpoint(local)
		if port == 0 {
			continue
		}
		protocol := "tcp"
		if strings.Contains(strings.ToLower(text), "udp") {
			protocol = "udp"
		}
		items = append(items, systemPortInfo{Port: port, Protocol: protocol, Source: "ss"})
	}
	return dedupeSystemPorts(items)
}

func dedupeSystemPorts(items []systemPortInfo) []systemPortInfo {
	seen := map[int]systemPortInfo{}
	for _, item := range items {
		if _, ok := seen[item.Port]; !ok {
			seen[item.Port] = item
		}
	}
	list := make([]systemPortInfo, 0, len(seen))
	for _, item := range seen {
		list = append(list, item)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Port < list[j].Port })
	return list
}

func parsePortFromEndpoint(endpoint string) int {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return 0
	}
	if idx := strings.LastIndex(endpoint, ":"); idx >= 0 {
		port, err := strconv.Atoi(strings.Trim(endpoint[idx+1:], "[]"))
		if err == nil {
			return port
		}
	}
	return 0
}

func (a *App) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (a *App) writeError(w http.ResponseWriter, status int, msg string) {
	a.writeJSON(w, status, map[string]string{"error": msg})
}

func loadTemplate(path string) (*template.Template, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return template.New("index").Parse(string(data))
}

func (a *App) saveConfig() error {
	fmt.Printf("config: %+v\n", a.cfg)
	data, err := json.MarshalIndent(a.cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(a.cfgPath, data, 0644)
}

func isPortInUse(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200000000)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func defaultKnownPorts() map[int]string {
	return map[int]string{
		20:    "ftp-data",
		21:    "ftp",
		22:    "ssh",
		23:    "telnet",
		25:    "smtp",
		53:    "dns",
		67:    "dhcp-server",
		68:    "dhcp-client",
		69:    "tftp",
		79:    "finger",
		80:    "http",
		110:   "pop3",
		123:   "ntp",
		143:   "imap",
		161:   "snmp",
		162:   "snmptrap",
		443:   "https",
		3306:  "mysql",
		5432:  "postgresql",
		6379:  "redis",
		11211: "memcached",
		27017: "mongodb",
		9200:  "elasticsearch",
		9300:  "elasticsearch-cluster",
		9042:  "cassandra",
		9092:  "kafka",
		8123:  "clickhouse",
		5672:  "rabbitmq",
		15672: "rabbitmq-mgmt",
		61613: "activemq",
		61614: "activemq-ssl",
		8500:  "consul",
		8600:  "consul-dns",
		3000:  "dev-http",
		8000:  "http-alt",
		8001:  "http-alt2",
		8080:  "http-proxy",
		8081:  "http-proxy2",
		8443:  "https-alt",
		8888:  "sun-answerbook",
		9000:  "websocket",
		9080:  "webcache",
		9443:  "webcache-ssl",
		10050: "zabbix-agent",
		2375:  "docker",
		2376:  "docker-ssl",
		2377:  "docker-swarm",
		4243:  "docker-api",
		6443:  "kubernetes-api",
		139:   "netbios-ssn",
		445:   "microsoft-ds",
		548:   "afp",
		873:   "rsync",
		1080:  "socks",
		1158:  "oracle-https",
		1521:  "oracle",
		2049:  "nfs",
		3690:  "svn",
		9418:  "git",
		9090:  "prometheus",
		9093:  "alertmanager",
		9115:  "blackbox-exporter",
		9121:  "redis-exporter",
		9122:  "postgres-exporter",
		9987:  "teamspeak",
		500:   "isakmp",
		1194:  "openvpn",
		1701:  "l2tp",
		1723:  "pptp",
		4500:  "ipsec-nat",
		5000:  "upnp",
		8388:  "shadowsocks",
		2080:  "autodesk-nlm",
		465:   "smtps",
		587:   "submission",
		993:   "imaps",
		995:   "pop3s",
	}
}
