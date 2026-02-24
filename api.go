package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	port      int
	bindIP    string
	listener  net.Listener
	mux       *http.ServeMux
	mu        sync.Mutex
	cmdLogs   []CmdLog
	onLog     func(CmdLog)
	healthFn  func() HealthResp
	connectFn func(string) error
}

type CmdLog struct {
	Time    string `json:"time"`
	Method  string `json:"method"`
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

type HealthResp struct {
	Status    string `json:"status"`
	Phase     string `json:"phase"`
	VirtIP    string `json:"virt_ip,omitempty"`
	APIPort   int    `json:"api_port,omitempty"`
	GUIPort   int    `json:"gui_port,omitempty"`
	Error     string `json:"error,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type ExecReq struct {
	Cmd        string `json:"cmd"`
	Cwd        string `json:"cwd,omitempty"`
	TimeoutSec int    `json:"timeout_sec,omitempty"`
}

type ExecResp struct {
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	Code   int    `json:"code"`
}

type ConnectReq struct {
	Config string `json:"config"`
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

type UploadReq struct {
	Path   string `json:"path"`
	Data   string `json:"data"`
	Append bool   `json:"append,omitempty"`
}

type UploadResp struct {
	OK    bool `json:"ok"`
	Bytes int  `json:"bytes"`
}

type DownloadReq struct {
	Path   string `json:"path"`
	Offset int64  `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type DownloadResp struct {
	Data      string `json:"data"`
	Size      int    `json:"size"`
	TotalSize int64  `json:"total_size"`
	Offset    int64  `json:"offset"`
	EOF       bool   `json:"eof"`
}

func NewAPIServer(bindIP string, startPort int, onLog func(CmdLog), healthFn func() HealthResp, connectFn func(string) error) *APIServer {
	s := &APIServer{
		bindIP:    bindIP,
		port:      startPort,
		onLog:     onLog,
		healthFn:  healthFn,
		connectFn: connectFn,
	}
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("/health", s.handleHealth)
	s.mux.HandleFunc("/connect", s.wrap(s.handleConnect))
	s.mux.HandleFunc("/exec", s.wrap(s.handleExec))
	s.mux.HandleFunc("/read", s.wrap(s.handleRead))
	s.mux.HandleFunc("/write", s.wrap(s.handleWrite))
	s.mux.HandleFunc("/edit", s.wrap(s.handleEdit))
	s.mux.HandleFunc("/patch", s.wrap(s.handlePatch))
	s.mux.HandleFunc("/ls", s.wrap(s.handleLs))
	s.mux.HandleFunc("/upload", s.wrap(s.handleUpload))
	s.mux.HandleFunc("/download", s.wrap(s.handleDownload))
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

func (s *APIServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if s.healthFn != nil {
		json.NewEncoder(w).Encode(s.healthFn())
		return
	}
	json.NewEncoder(w).Encode(HealthResp{
		Status:  "ok",
		Phase:   "unknown",
		APIPort: s.Port(),
	})
}

func jsonErr(w http.ResponseWriter, msg string, code int) {
	jsonErrWithCode(w, msg, "", code)
}

func jsonErrWithCode(w http.ResponseWriter, msg string, errCode string, code int) {
	w.WriteHeader(code)
	resp := map[string]string{"error": msg}
	if errCode != "" {
		resp["error_code"] = errCode
	}
	json.NewEncoder(w).Encode(resp)
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
	timeout := req.TimeoutSec
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 600 {
		timeout = 600
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
	defer cancel()

	shell, flag := getShell()
	cmd := exec.CommandContext(ctx, shell, flag, req.Cmd)
	if req.Cwd != "" {
		cmd.Dir = req.Cwd
	}

	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	code := 0
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			if stderr.Len() > 0 && !strings.HasSuffix(stderr.String(), "\n") {
				stderr.WriteString("\n")
			}
			stderr.WriteString(fmt.Sprintf("command timed out after %ds", timeout))
			code = 124
		} else if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = -1
		}
	} else {
		code = 0
	}

	s.addLog("POST", "/exec", truncate(req.Cmd, 80))
	json.NewEncoder(w).Encode(ExecResp{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
		Code:   code,
	})
}

func (s *APIServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	if s.connectFn == nil {
		jsonErr(w, "connect is not supported", 500)
		return
	}

	var req ConnectReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Config == "" {
		jsonErr(w, "config is required", 400)
		return
	}
	if err := s.connectFn(req.Config); err != nil {
		errCode := errorCodeOf(err)
		statusCode := 400
		switch {
		case errors.Is(err, ErrConfigPending), errors.Is(err, ErrAlreadyConnecting), errors.Is(err, ErrAlreadyRunning):
			statusCode = 409
		}
		jsonErrWithCode(w, err.Error(), errCode, statusCode)
		return
	}
	s.addLog("POST", "/connect", "submitted config")
	json.NewEncoder(w).Encode(map[string]bool{"ok": true})
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
		if os.IsNotExist(err) {
			// Keep transport success and report a business-level miss via payload.
			jsonErr(w, err.Error(), 200)
			return
		}
		jsonErr(w, err.Error(), 500)
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
		jsonErr(w, err.Error(), 400)
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
		jsonErr(w, err.Error(), 400)
		return
	}

	content := string(data)
	count := strings.Count(content, req.Old)
	if count == 0 {
		jsonErr(w, "old text not found", 400)
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
		if os.IsNotExist(err) {
			// Keep transport success and report a business-level miss via payload.
			jsonErr(w, err.Error(), 200)
			return
		}
		jsonErr(w, err.Error(), 500)
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

func (s *APIServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	var req UploadReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonErr(w, "invalid request body", 400)
		return
	}
	if req.Path == "" || req.Data == "" {
		jsonErr(w, "path and data are required", 400)
		return
	}

	data, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		jsonErr(w, "data must be base64", 400)
		return
	}

	dir := filepath.Dir(req.Path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}

	if req.Append {
		f, err := os.OpenFile(req.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
		defer f.Close()
		if _, err := f.Write(data); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
	} else {
		if err := os.WriteFile(req.Path, data, 0644); err != nil {
			jsonErr(w, err.Error(), 500)
			return
		}
	}

	s.addLog("POST", "/upload", truncate(req.Path, 80))
	json.NewEncoder(w).Encode(UploadResp{OK: true, Bytes: len(data)})
}

func (s *APIServer) handleDownload(w http.ResponseWriter, r *http.Request) {
	var req DownloadReq
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
		if os.IsNotExist(err) {
			jsonErr(w, err.Error(), 200)
			return
		}
		jsonErr(w, err.Error(), 500)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	total := info.Size()

	offset := req.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 1024 * 1024
	}
	if limit > 8*1024*1024 {
		limit = 8 * 1024 * 1024
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		jsonErr(w, err.Error(), 500)
		return
	}
	buf := make([]byte, limit)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		jsonErr(w, err.Error(), 500)
		return
	}

	s.addLog("POST", "/download", truncate(req.Path, 80))
	json.NewEncoder(w).Encode(DownloadResp{
		Data:      base64.StdEncoding.EncodeToString(buf[:n]),
		Size:      n,
		TotalSize: total,
		Offset:    offset,
		EOF:       offset+int64(n) >= total,
	})
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
