package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"runtime"
	"sync"
)

//go:embed gui.html
var guiFS embed.FS

type GUIServer struct {
	port       int
	listener   net.Listener
	mux        *http.ServeMux
	mu         sync.Mutex
	state      GUIState
	configCh   chan *Config
	logs       []CmdLog
	debugLogs  []string
	peerInfoFn func() (PeerInfoSnapshot, error)
}

type GUIState struct {
	Phase            string           `json:"phase"` // "config" | "connecting" | "running" | "error"
	Role             string           `json:"role,omitempty"`
	NetworkOwner     string           `json:"network_owner,omitempty"`
	NetworkHash      string           `json:"network_hash,omitempty"`
	TUNDevice        string           `json:"tun_device,omitempty"`
	VirtualSubnet    string           `json:"virtual_subnet,omitempty"`
	VirtIP           string           `json:"virt_ip,omitempty"`
	APIPort          int              `json:"api_port,omitempty"`
	Error            string           `json:"error,omitempty"`
	ErrorCode        string           `json:"error_code,omitempty"`
	ClipboardCommand string           `json:"clipboard_command,omitempty"`
	Commands         []InstallCommand `json:"commands,omitempty"`
}

type InstallCommand struct {
	Platform string `json:"platform"`
	Command  string `json:"command"`
}

var (
	ErrConfigPending     = errors.New("config already pending")
	ErrAlreadyConnecting = errors.New("already connecting")
	ErrAlreadyRunning    = errors.New("already running")
)

func NewGUIServer(startPort int) *GUIServer {
	g := &GUIServer{
		port:     startPort,
		configCh: make(chan *Config, 1),
		state:    GUIState{Phase: "config"},
	}
	g.mux = http.NewServeMux()
	g.mux.HandleFunc("/", g.handleIndex)
	g.mux.HandleFunc("/api/submit-config", g.handleSubmitConfig)
	g.mux.HandleFunc("/api/state", g.handleState)
	g.mux.HandleFunc("/api/logs", g.handleLogs)
	g.mux.HandleFunc("/api/debug-logs", g.handleDebugLogs)
	g.mux.HandleFunc("/api/peer-info", g.handlePeerInfo)
	g.mux.HandleFunc("/api/stop", g.handleStop)
	return g
}

func (g *GUIServer) Start() error {
	for i := 0; i < 100; i++ {
		addr := fmt.Sprintf("127.0.0.1:%d", g.port+i)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		g.listener = ln
		g.port = g.port + i
		go http.Serve(ln, g.mux)
		return nil
	}
	return fmt.Errorf("no available port for GUI")
}

func (g *GUIServer) Port() int { return g.port }

func (g *GUIServer) WaitForConfig() *Config {
	return <-g.configCh
}

func (g *GUIServer) SetState(s GUIState) {
	g.mu.Lock()
	g.state = s
	g.mu.Unlock()
}

func (g *GUIServer) SetPeerInfoProvider(fn func() (PeerInfoSnapshot, error)) {
	g.mu.Lock()
	g.peerInfoFn = fn
	g.mu.Unlock()
}

func (g *GUIServer) GetState() GUIState {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.state
}

func (g *GUIServer) AddLog(log CmdLog) {
	g.mu.Lock()
	g.logs = append(g.logs, log)
	g.mu.Unlock()
}

func (g *GUIServer) AddDebugLog(line string) {
	g.mu.Lock()
	g.debugLogs = append(g.debugLogs, line)
	g.mu.Unlock()
}

func (g *GUIServer) Stop() {
	if g.listener != nil {
		g.listener.Close()
	}
}

func (g *GUIServer) SubmitConfigEncoded(encoded string) error {
	cfg, err := DecodeConfig(encoded)
	if err != nil {
		return err
	}
	return g.SubmitConfig(cfg)
}

func (g *GUIServer) SubmitConfig(cfg *Config) error {
	state := g.GetState()
	switch state.Phase {
	case "connecting":
		return ErrAlreadyConnecting
	case "running":
		return ErrAlreadyRunning
	}
	if len(g.configCh) > 0 {
		return ErrConfigPending
	}
	if err := precheckBeforeConnect(cfg); err != nil {
		state.Phase = "error"
		state.VirtIP = ""
		state.Error = err.Error()
		state.ErrorCode = errorCodeOf(err)
		g.SetState(state)
		return err
	}
	select {
	case g.configCh <- cfg:
		state.Phase = "connecting"
		state.VirtIP = ""
		state.Error = ""
		state.ErrorCode = ""
		g.SetState(state)
		return nil
	default:
		return ErrConfigPending
	}
}

func precheckBeforeConnect(cfg *Config) error {
	if cfg == nil {
		return errors.New("config is required")
	}
	if runtime.GOOS != "windows" {
		return nil
	}

	isAdmin, err := isCurrentUserAdmin()
	if err != nil {
		return newCodedError(
			ErrorCodeWindowsAdminCheckFail,
			fmt.Sprintf("failed to check administrator privilege: %v", err),
		)
	}
	if !isAdmin {
		return newCodedError(
			ErrorCodeWindowsNotAdmin,
			"administrator privileges required on Windows; please run telehand as administrator",
		)
	}
	return nil
}

func (g *GUIServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	data, _ := guiFS.ReadFile("gui.html")
	tmpl, err := template.New("gui").Parse(string(data))
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	tmpl.Execute(w, nil)
}

func (g *GUIServer) handleSubmitConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", 405)
		return
	}
	var body struct {
		Config string `json:"config"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonResp(w, 400, map[string]string{"error": "invalid request"})
		return
	}
	if err := g.SubmitConfigEncoded(body.Config); err != nil {
		code := 400
		if errors.Is(err, ErrConfigPending) || errors.Is(err, ErrAlreadyConnecting) || errors.Is(err, ErrAlreadyRunning) {
			code = 409
		}
		resp := map[string]string{"error": err.Error()}
		if errCode := errorCodeOf(err); errCode != "" {
			resp["error_code"] = errCode
		}
		jsonResp(w, code, resp)
		return
	}
	jsonResp(w, 200, map[string]string{"ok": "true"})
}

func (g *GUIServer) handleState(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, g.GetState())
}

func (g *GUIServer) handleLogs(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	logs := make([]CmdLog, len(g.logs))
	copy(logs, g.logs)
	g.mu.Unlock()
	jsonResp(w, 200, logs)
}

func (g *GUIServer) handleDebugLogs(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	logs := make([]string, len(g.debugLogs))
	copy(logs, g.debugLogs)
	g.mu.Unlock()
	jsonResp(w, 200, logs)
}

func (g *GUIServer) handlePeerInfo(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	fn := g.peerInfoFn
	g.mu.Unlock()
	if fn == nil {
		jsonResp(w, 200, PeerInfoSnapshot{
			UpdatedAt: "",
			Peers:     []PeerInfo{},
		})
		return
	}
	snapshot, err := fn()
	if err != nil {
		jsonResp(w, 200, map[string]string{"error": err.Error()})
		return
	}
	jsonResp(w, 200, snapshot)
}

func (g *GUIServer) handleStop(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]string{"ok": "true"})
	go func() { g.configCh <- nil }()
}

func jsonResp(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}
