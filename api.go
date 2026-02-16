package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type APIServer struct {
	port     int
	bindIP   string
	listener net.Listener
	mux      *http.ServeMux
	mu       sync.Mutex
	cmdLogs  []CmdLog
	onLog    func(CmdLog)
}

type CmdLog struct {
	Time    string `json:"time"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

type ExecReq struct {
	Cmd string `json:"cmd"`
	Cwd string `json:"cwd,omitempty"`
}

type ExecResp struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

type ReadReq struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ReadResp struct {
	Content    string `json:"content"`
	TotalLines int    `json:"total_lines"`
}

type WriteReq struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type EditReq struct {
	Path      string `json:"path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Content   string `json:"content"`
}

type PatchReq struct {
	Path       string `json:"path"`
	Old        string `json:"old"`
	New        string `json:"new"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type PatchResp struct {
	Replaced int    `json:"replaced"`
	Warning  string `json:"warning,omitempty"`
	Matches  []int  `json:"matches,omitempty"`
}

type LsReq struct {
	Path string `json:"path"`
}

type LsEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
}

type LsResp struct {
	Entries []LsEntry `json:"entries"`
}

func NewAPIServer(bindIP string, startPort int, onLog func(CmdLog)) *APIServer {
	s := &APIServer{bindIP: bindIP, port: startPort, onLog: onLog}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/exec", s.wrap(s.handleExec))
	s.mux.HandleFunc("/read", s.wrap(s.handleRead))
	s.mux.HandleFunc("/write", s.wrap(s.handleWrite))
	s.mux.HandleFunc("/edit", s.wrap(s.handleEdit))
	s.mux.HandleFunc("/patch", s.wrap(s.handlePatch))
	s.mux.HandleFunc("/ls", s.wrap(s.handleLs))
	return s
}

func (s *APIServer) Start() error {
	for i := 0; i < 100; i++ {
		addr := fmt.Sprintf("%s:%d", s.bindIP, s.port+i)
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		s.listener = ln
		s.port = s.port + i
		go http.Serve(ln, s.mux)
		return nil
	}
	return fmt.Errorf("no available port found starting from %d", s.port)
}

func (s *APIServer) Port() int { return s.port }

func (s *APIServer) Stop() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *APIServer) GetLogs() []CmdLog {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := make([]CmdLog, len(s.cmdLogs))
	copy(cp, s.cmdLogs)
	return cp
}

func (s *APIServer) addLog(method, path, summary string) {
	log := CmdLog{
		Time:    time.Now().Format("15:04:05"),
		Method:  method,
		Path:    path,
		Summary: summary,
	}
	s.mu.Lock()
	s.cmdLogs = append(s.cmdLogs, log)
	s.mu.Unlock()
	if s.onLog != nil {
		s.onLog(log)
	}
}

func (s *APIServer) wrap(handler func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		handler(w, r)
	}
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func (s *APIServer) handleExec(w http.ResponseWriter, r *http.Request) {
	var req ExecReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Cmd == "" {
		jsonErr(w, "cmd is required", 400)
		return
	}

	shell, flag := getShell()
	cmd := exec.Command(shell, flag, req.Cmd)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	}

	s.addLog("POST", "/exec", truncate(req.Cmd, 80))
	json.NewEncoder(w).Encode(ExecResp{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   code,
	})
}

func (s *APIServer) handleRead(w http.ResponseWriter, r *http.Request) {
	var req ReadReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Path == "" {
		jsonErr(w, "path is required", 400)
		return
	}

	f, err := os.Open(req.Path)
	if err != nil {
		jsonErr(w, err.Error(), 404)
		return
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	totalLines := len(lines)

	offset := req.Offset
	limit := req.Limit
	if offset < 0 {
		offset = 0
	}
	if offset > totalLines {
		offset = totalLines
	}
	if limit <= 0 {
		limit = totalLines
	}
	end := offset + limit
	if end > totalLines {
		end = totalLines
	}

	content := strings.Join(lines[offset:end], "\n")
	s.addLog("POST", "/read", truncate(req.Path, 80))
	json.NewEncoder(w).Encode(ReadResp{Content: content, TotalLines: totalLines})
}

func (s *APIServer) handleWrite(w http.ResponseWriter, r *http.Request) {
	var req WriteReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Path == "" {
		jsonErr(w, "path is required", 400)
		return
	}

	dir := filepath.Dir(req.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	if err := os.WriteFile(req.Path, []byte(req.Content), 0644); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	s.addLog("POST", "/write", truncate(req.Path, 80))
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *APIServer) handleEdit(w http.ResponseWriter, r *http.Request) {
	var req EditReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Path == "" {
		jsonErr(w, "path is required", 400)
		return
	}

	data, err := os.ReadFile(req.Path)
	if err != nil {
		jsonErr(w, err.Error(), 404)
		return
	}

	lines := strings.Split(string(data), "\n")
	if req.StartLine < 1 || req.StartLine > len(lines)+1 {
		jsonErr(w, fmt.Sprintf("start_line %d out of range (1-%d)", req.StartLine, len(lines)+1), 400)
		return
	}
	if req.EndLine < req.StartLine-1 || req.EndLine > len(lines) {
		jsonErr(w, fmt.Sprintf("end_line %d out of range (%d-%d)", req.EndLine, req.StartLine-1, len(lines)), 400)
		return
	}

	var newContent []string
	if req.Content != "" {
		newContent = strings.Split(req.Content, "\n")
	}

	result := make([]string, 0, len(lines))
	result = append(result, lines[:req.StartLine-1]...)
	result = append(result, newContent...)
	result = append(result, lines[req.EndLine:]...)

	if err := os.WriteFile(req.Path, []byte(strings.Join(result, "\n")), 0644); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	s.addLog("POST", "/edit", fmt.Sprintf("%s L%d-%d", truncate(req.Path, 40), req.StartLine, req.EndLine))
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func (s *APIServer) handlePatch(w http.ResponseWriter, r *http.Request) {
	var req PatchReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Path == "" || req.Old == "" {
		jsonErr(w, "path and old are required", 400)
		return
	}

	data, err := os.ReadFile(req.Path)
	if err != nil {
		jsonErr(w, err.Error(), 404)
		return
	}

	content := string(data)
	count := strings.Count(content, req.Old)
	if count == 0 {
		jsonErr(w, "old text not found", 404)
		return
	}

	matchLines := findMatchLines(content, req.Old)

	var newContent string
	replaced := 0
	if req.ReplaceAll {
		newContent = strings.ReplaceAll(content, req.Old, req.New)
		replaced = count
	} else {
		newContent = strings.Replace(content, req.Old, req.New, 1)
		replaced = 1
	}

	if err := os.WriteFile(req.Path, []byte(newContent), 0644); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	resp := PatchResp{Replaced: replaced}
	if count > 1 && !req.ReplaceAll {
		resp.Warning = fmt.Sprintf("multiple matches found (%d total), only replaced first occurrence", count)
		resp.Matches = matchLines
	}

	s.addLog("POST", "/patch", truncate(req.Path, 80))
	json.NewEncoder(w).Encode(resp)
}

func (s *APIServer) handleLs(w http.ResponseWriter, r *http.Request) {
	var req LsReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Path == "" {
		jsonErr(w, "path is required", 400)
		return
	}

	entries, err := os.ReadDir(req.Path)
	if err != nil {
		jsonErr(w, err.Error(), 404)
		return
	}

	var result []LsEntry
	for _, e := range entries {
		info, _ := e.Info()
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		result = append(result, LsEntry{
			Name:  e.Name(),
			IsDir: e.IsDir(),
			Size:  size,
		})
	}

	s.addLog("POST", "/ls", truncate(req.Path, 80))
	json.NewEncoder(w).Encode(LsResp{Entries: result})
}

func findMatchLines(content, pattern string) []int {
	var lines []int
	idx := 0
	for {
		pos := strings.Index(content[idx:], pattern)
		if pos == -1 {
			break
		}
		absPos := idx + pos
		line := strings.Count(content[:absPos], "\n") + 1
		lines = append(lines, line)
		idx = absPos + len(pattern)
	}
	return lines
}

func getShell() (string, string) {
	if runtime.GOOS == "windows" {
		return "powershell.exe", "-Command"
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	return sh, "-c"
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}


