package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func prettyJSON(raw []byte) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}

func callRaw(t *testing.T, client *http.Client, method, url string, body any) (int, []byte) {
	t.Helper()

	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body failed: %v", err)
		}
		r = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatalf("new request failed: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s failed: %v", method, url, err)
	}
	defer resp.Body.Close()

	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out
}

func TestAPIServerObserve(t *testing.T) {
	s := NewAPIServer("127.0.0.1", 19080, nil, nil, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}
	t.Logf("API base: %s", base)

	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "a.txt")
	file2 := filepath.Join(tmpDir, "b.txt")

	// 1) GET /exec (method guard demonstration)
	status, out := callRaw(t, client, http.MethodGet, base+"/exec", nil)
	t.Logf("\n[1] GET /exec")
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", string(out))

	// 2) POST /write
	status, out = callRaw(t, client, http.MethodPost, base+"/write", WriteReq{
		Path:    file1,
		Content: "line1\nline2\nline3",
	})
	t.Logf("\n[2] POST /write")
	t.Logf("request: path=%s, content=line1\\nline2\\nline3", file1)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 3) POST /read
	status, out = callRaw(t, client, http.MethodPost, base+"/read", ReadReq{Path: file1})
	t.Logf("\n[3] POST /read")
	t.Logf("request: path=%s", file1)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 4) POST /edit
	status, out = callRaw(t, client, http.MethodPost, base+"/edit", EditReq{
		Path:      file1,
		StartLine: 2,
		EndLine:   2,
		Content:   "HELLO_FROM_EDIT",
	})
	t.Logf("\n[4] POST /edit")
	t.Logf("request: path=%s, start_line=2, end_line=2, content=HELLO_FROM_EDIT", file1)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 5) POST /read (after edit)
	status, out = callRaw(t, client, http.MethodPost, base+"/read", ReadReq{Path: file1})
	t.Logf("\n[5] POST /read (after edit)")
	t.Logf("request: path=%s", file1)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 6) POST /patch (single match)
	status, out = callRaw(t, client, http.MethodPost, base+"/patch", PatchReq{
		Path: file1,
		Old:  "line1",
		New:  "FIRST_LINE",
	})
	t.Logf("\n[6] POST /patch (single)")
	t.Logf("request: path=%s, old=line1, new=FIRST_LINE", file1)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 7) Prepare file2 + POST /patch (multi match)
	if err := os.WriteFile(file2, []byte("x\ny\nx\n"), 0644); err != nil {
		t.Fatalf("prepare file2 failed: %v", err)
	}
	status, out = callRaw(t, client, http.MethodPost, base+"/patch", PatchReq{
		Path: file2,
		Old:  "x",
		New:  "z",
	})
	t.Logf("\n[7] POST /patch (multi)")
	t.Logf("request: path=%s, old=x, new=z", file2)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 8) POST /read file2
	status, out = callRaw(t, client, http.MethodPost, base+"/read", ReadReq{Path: file2})
	t.Logf("\n[8] POST /read file2")
	t.Logf("request: path=%s", file2)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 9) POST /ls
	status, out = callRaw(t, client, http.MethodPost, base+"/ls", LsReq{Path: tmpDir})
	t.Logf("\n[9] POST /ls")
	t.Logf("request: path=%s", tmpDir)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 10) POST /exec
	execCmd := "echo hi-from-exec"
	if runtime.GOOS == "windows" {
		execCmd = "Write-Output hi-from-exec"
	}
	status, out = callRaw(t, client, http.MethodPost, base+"/exec", ExecReq{
		Cmd: execCmd,
		Cwd: tmpDir,
	})
	t.Logf("\n[10] POST /exec")
	t.Logf("request: cmd=%s, cwd=%s", execCmd, tmpDir)
	t.Logf("status: %d", status)
	t.Logf("body:\n%s", prettyJSON(out))

	// 11) Internal command logs
	logs := s.GetLogs()
	b, _ := json.MarshalIndent(logs, "", "  ")
	t.Logf("\n[11] APIServer.GetLogs()")
	t.Logf("count: %d", len(logs))
	t.Logf("logs:\n%s", string(b))
}

func TestReadAndLsNotFoundReturn200WithError(t *testing.T) {
	s := NewAPIServer("127.0.0.1", 19180, nil, nil, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}
	missing := filepath.Join(t.TempDir(), "not-exist-path")

	status, out := callRaw(t, client, http.MethodPost, base+"/read", ReadReq{Path: missing})
	if status != 200 {
		t.Fatalf("POST /read not-found status=%d body=%s", status, string(out))
	}
	var readErr map[string]string
	if err := json.Unmarshal(out, &readErr); err != nil {
		t.Fatalf("POST /read unmarshal failed: %v, body=%s", err, string(out))
	}
	if strings.TrimSpace(readErr["error"]) == "" {
		t.Fatalf("POST /read expected error payload, got: %s", string(out))
	}

	status, out = callRaw(t, client, http.MethodPost, base+"/ls", LsReq{Path: missing})
	if status != 200 {
		t.Fatalf("POST /ls not-found status=%d body=%s", status, string(out))
	}
	var lsErr map[string]string
	if err := json.Unmarshal(out, &lsErr); err != nil {
		t.Fatalf("POST /ls unmarshal failed: %v, body=%s", err, string(out))
	}
	if strings.TrimSpace(lsErr["error"]) == "" {
		t.Fatalf("POST /ls expected error payload, got: %s", string(out))
	}
}

func TestExecTimeout(t *testing.T) {
	s := NewAPIServer("127.0.0.1", 19280, nil, nil, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}

	cmd := "sleep 2"
	if runtime.GOOS == "windows" {
		cmd = "Start-Sleep -Seconds 2"
	}
	status, out := callRaw(t, client, http.MethodPost, base+"/exec", ExecReq{
		Cmd:        cmd,
		TimeoutSec: 1,
	})
	if status != 200 {
		t.Fatalf("POST /exec timeout status=%d body=%s", status, string(out))
	}
	var resp ExecResp
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal exec resp failed: %v body=%s", err, string(out))
	}
	if resp.Code != 124 {
		t.Fatalf("expected timeout code=124, got=%d body=%s", resp.Code, string(out))
	}
	if !strings.Contains(resp.Stderr, "timed out") {
		t.Fatalf("expected timeout stderr marker, got=%q", resp.Stderr)
	}
}

func TestConnectEndpoint(t *testing.T) {
	var got string
	s := NewAPIServer("127.0.0.1", 19380, nil, nil, func(cfg string) error {
		got = cfg
		return nil
	})
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}
	status, out := callRaw(t, client, http.MethodPost, base+"/connect", ConnectReq{Config: "abc"})
	if status != 200 {
		t.Fatalf("POST /connect status=%d body=%s", status, string(out))
	}
	if got != "abc" {
		t.Fatalf("connect callback not called, got=%q", got)
	}
}

func TestConnectEndpointReturnsErrorCode(t *testing.T) {
	s := NewAPIServer("127.0.0.1", 19480, nil, nil, func(cfg string) error {
		return newCodedError(ErrorCodeWindowsNotAdmin, "administrator privileges required")
	})
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}
	status, out := callRaw(t, client, http.MethodPost, base+"/connect", ConnectReq{Config: "abc"})
	if status != 400 {
		t.Fatalf("POST /connect status=%d body=%s", status, string(out))
	}

	var body map[string]string
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatalf("unmarshal failed: %v body=%s", err, string(out))
	}
	if body["error_code"] != ErrorCodeWindowsNotAdmin {
		t.Fatalf("expected error_code=%q, got=%q body=%s", ErrorCodeWindowsNotAdmin, body["error_code"], string(out))
	}
}

func TestHealthIncludesErrorCode(t *testing.T) {
	s := NewAPIServer("127.0.0.1", 19580, nil, func() HealthResp {
		return HealthResp{
			Status:    "ok",
			Phase:     "error",
			Error:     "administrator privileges required",
			ErrorCode: ErrorCodeWindowsNotAdmin,
		}
	}, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}
	status, out := callRaw(t, client, http.MethodGet, base+"/health", nil)
	if status != 200 {
		t.Fatalf("GET /health status=%d body=%s", status, string(out))
	}

	var health HealthResp
	if err := json.Unmarshal(out, &health); err != nil {
		t.Fatalf("unmarshal failed: %v body=%s", err, string(out))
	}
	if health.ErrorCode != ErrorCodeWindowsNotAdmin {
		t.Fatalf("expected health error_code=%q, got=%q", ErrorCodeWindowsNotAdmin, health.ErrorCode)
	}
}

func TestUploadAndDownload(t *testing.T) {
	s := NewAPIServer("127.0.0.1", 19680, nil, nil, nil)
	if err := s.Start(); err != nil {
		t.Fatalf("start api server failed: %v", err)
	}
	defer s.Stop()

	base := fmt.Sprintf("http://127.0.0.1:%d", s.Port())
	client := &http.Client{Timeout: 8 * time.Second}
	target := filepath.Join(t.TempDir(), "bin.dat")

	part1 := []byte{0x00, 0x01, 0x02, 0x03}
	part2 := []byte("hello")
	status, out := callRaw(t, client, http.MethodPost, base+"/upload", UploadReq{
		Path: target,
		Data: base64.StdEncoding.EncodeToString(part1),
	})
	if status != 200 {
		t.Fatalf("upload part1 status=%d body=%s", status, string(out))
	}

	status, out = callRaw(t, client, http.MethodPost, base+"/upload", UploadReq{
		Path:   target,
		Data:   base64.StdEncoding.EncodeToString(part2),
		Append: true,
	})
	if status != 200 {
		t.Fatalf("upload part2 status=%d body=%s", status, string(out))
	}

	status, out = callRaw(t, client, http.MethodPost, base+"/download", DownloadReq{Path: target, Offset: 0, Limit: 32})
	if status != 200 {
		t.Fatalf("download status=%d body=%s", status, string(out))
	}
	var dl DownloadResp
	if err := json.Unmarshal(out, &dl); err != nil {
		t.Fatalf("download unmarshal failed: %v body=%s", err, string(out))
	}
	raw, err := base64.StdEncoding.DecodeString(dl.Data)
	if err != nil {
		t.Fatalf("decode download data failed: %v", err)
	}
	want := append(part1, part2...)
	if !bytes.Equal(raw, want) {
		t.Fatalf("download bytes mismatch got=%v want=%v", raw, want)
	}
	if !dl.EOF {
		t.Fatalf("expected eof=true got=%v", dl.EOF)
	}
}
