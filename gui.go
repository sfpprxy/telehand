package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"sync"
)

//go:embed gui.html
var guiFS embed.FS

type GUIServer struct {
	port      int
	listener  net.Listener
	mux       *http.ServeMux
	mu        sync.Mutex
	state     GUIState
	configCh  chan *Config
	logs      []CmdLog
	debugLogs []string
}

type GUIState struct {
	Phase   string `json:"phase"` // "config" | "connecting" | "running"
	VirtIP  string `json:"virt_ip,omitempty"`
	APIPort int    `json:"api_port,omitempty"`
	Error   string `json:"error,omitempty"`
}

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
	cfg, err := DecodeConfig(body.Config)
	if err != nil {
		jsonResp(w, 400, map[string]string{"error": err.Error()})
		return
	}
	g.SetState(GUIState{Phase: "connecting"})
	g.configCh <- cfg
	jsonResp(w, 200, map[string]string{"ok": "true"})
}

func (g *GUIServer) handleState(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	s := g.state
	g.mu.Unlock()
	jsonResp(w, 200, s)
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

func (g *GUIServer) handleStop(w http.ResponseWriter, r *http.Request) {
	jsonResp(w, 200, map[string]string{"ok": "true"})
	go func() { g.configCh <- nil }()
}

func jsonResp(w http.ResponseWriter, code int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(data)
}
