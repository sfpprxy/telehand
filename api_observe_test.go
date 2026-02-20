package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
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
	s := NewAPIServer("127.0.0.1", 19080, nil, nil)
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
