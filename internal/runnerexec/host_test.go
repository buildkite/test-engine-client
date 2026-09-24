//go:build !windows

package runnerexec

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

type testSource struct {
	batches    []Batch
	accepted   []string
	results    []Result
	unresolved []string
	reasons    []error
	done       string
	onNext     func()
}

func (s *testSource) Next() (*Batch, string, error) {
	if s.onNext != nil {
		s.onNext()
	}
	if len(s.batches) == 0 {
		return nil, s.done, nil
	}
	b := s.batches[0]
	s.batches = s.batches[1:]
	return &b, "", nil
}
func (s *testSource) Accepted(b Batch, result Result) {
	s.accepted = append(s.accepted, b.ID)
	s.results = append(s.results, result)
}
func (s *testSource) Unresolved(b Batch, err error) {
	s.unresolved = append(s.unresolved, b.ID)
	s.reasons = append(s.reasons, err)
}

func socketClient(socket string) *http.Client {
	return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socket)
	}}}
}

func request(t *testing.T, client *http.Client, method, path, session, body string, status int) []byte {
	t.Helper()
	r, err := http.NewRequest(method, "http://runner"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Bktec-Session", session)
	response, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: status %d want %d: %s", method, path, response.StatusCode, status, data)
	}
	if response.Header.Get("Content-Type") != "application/json" {
		t.Fatal("missing JSON content type")
	}
	return data
}

func handshakeBody(instance string) string {
	return fmt.Sprintf(`{"instance_id":%q,"runner":{"name":"fixture","pid":42},"capabilities":{"selector_formats":["selector","file","example"]},"future":true}`, instance)
}

func handshake(t *testing.T, client *http.Client, instance string) string {
	t.Helper()
	data := request(t, client, "POST", "/v1/sessions", "", handshakeBody(instance), 201)
	var response SessionResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	decoded, err := hex.DecodeString(response.SessionID)
	if err != nil || len(decoded) < 16 || response.Poll.MaxWaitMS != 30000 {
		t.Fatalf("invalid handshake %s", data)
	}
	return response.SessionID
}

const completed = `{"status":"completed","report_format":"rspec-json","report":{"examples":[],"summary":{"example_count":0}},"future":1}`

func TestSocketProtocol(t *testing.T) {
	source := &testSource{batches: []Batch{{ID: "first", Tests: []plan.TestCase{{Format: plan.TestCaseFormatExample, Path: "spec/a.rb:12", Identifier: "./spec/a.rb[1:2]"}}}, {ID: "second"}}}
	h, err := newHost(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	client := socketClient(h.socket)
	defer client.CloseIdleConnections()
	for path, mode := range map[string]os.FileMode{h.dir: 0700, h.socket: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("permissions %s: %v %v", path, info, err)
		}
	}
	request(t, client, "POST", "/v1/batches", "", "{}", 401)
	request(t, client, "POST", "/v1/batches", "unknown", "{}", 401)
	if got := strings.TrimSpace(string(request(t, client, "POST", "/v2/sessions", "", "{}", 426))); got != `{"supported":[1]}` {
		t.Fatal(got)
	}
	for _, body := range []string{`{`, `null`, `{} {}`, `{"instance_id":"bad","capabilities":{"selector_formats":["unknown"]}}`} {
		request(t, client, "POST", "/v1/sessions", "", body, 400)
	}
	session := handshake(t, client, "one")
	if replay := handshake(t, client, "one"); replay != session {
		t.Fatal("handshake not replayed")
	}
	request(t, client, "POST", "/v1/batches", session, `[]`, 400)
	batch := request(t, client, "POST", "/v1/batches", session, `{"extra":1}`, 200)
	if !strings.Contains(string(batch), `"identifier":"./spec/a.rb[1:2]"`) {
		t.Fatalf("lost plan selector: %s", batch)
	}
	h.mu.Lock()
	deadline := h.deadline
	h.mu.Unlock()
	if replay := request(t, client, "POST", "/v1/batches", session, `{}`, 200); string(replay) != string(batch) {
		t.Fatal("batch replay differs")
	}
	h.mu.Lock()
	changed := !h.deadline.Equal(deadline)
	h.mu.Unlock()
	if changed {
		t.Fatal("pull extended deadline")
	}
	for _, body := range []string{`{`, `{"status":"completed","report_format":"rspec-json"}`, `{"status":"completed","report_format":"rspec-json","report":null}`, `{"status":"errored","report_format":"rspec-json","error":{}}`} {
		request(t, client, "POST", "/v1/batches/first/results", session, body, 400)
	}
	request(t, client, "POST", "/v1/batches/unknown/results", session, completed, 409)
	request(t, client, "POST", "/v1/batches/first/results", session, completed, 200)
	request(t, client, "POST", "/v1/batches/first/results", session, completed, 200)
	request(t, client, "POST", "/v1/batches/first/results", session, completed+" ", 409)
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	newSession := handshake(t, client, "two")
	if newSession == session {
		t.Fatal("session not replaced")
	}
	request(t, client, "POST", "/v1/sessions", "", handshakeBody("one"), 401)
	request(t, client, "POST", "/v1/batches", session, `{}`, 401)
	request(t, client, "POST", "/v1/batches/second/results", session, completed, 401)
	request(t, client, "POST", "/v1/batches/second/results", newSession, completed, 409)
	if got := string(request(t, client, "POST", "/v1/batches", newSession, `{}`, 200)); !strings.Contains(got, `"type":"wait"`) {
		t.Fatal(got)
	}
	h.mu.Lock()
	accepted, unresolved := fmt.Sprint(source.accepted), fmt.Sprint(source.unresolved)
	source.done = "plan_completed"
	h.mu.Unlock()
	if accepted != "[first]" || unresolved != "[second]" {
		t.Fatalf("accepted %s unresolved %s", accepted, unresolved)
	}
	for range 2 {
		if got := string(request(t, client, "POST", "/v1/batches", newSession, `{}`, 200)); !strings.Contains(got, `"reason":"plan_completed"`) {
			t.Fatal(got)
		}
	}
	h.close()
	if _, err := os.Stat(h.dir); !os.IsNotExist(err) {
		t.Fatalf("socket directory remains: %v", err)
	}
}

func TestResultFormatPassedThrough(t *testing.T) {
	source := &testSource{batches: []Batch{{ID: "custom"}, {ID: "error"}}}
	h, err := newHost(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	client := socketClient(h.socket)
	defer client.CloseIdleConnections()
	session := handshake(t, client, "one")
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	for _, body := range []string{
		`{"status":"completed","report":{}}`,
		`{"status":"completed","report_format":"","report":{}}`,
		`{"status":"completed","report_format":null,"report":{}}`,
		`{"status":"completed","report_format":42,"report":{}}`,
		`{"status":"completed","report_format":"custom-json","report":[]}`,
		`{"status":"errored","report_format":"","error":{"kind":"load_error","message":"fixture failed"}}`,
	} {
		request(t, client, "POST", "/v1/batches/custom/results", session, body, 400)
	}
	const report = `{ "checks": [{"name":"example", "outcome":"ok"}], "duration_ms":17 }`
	body := `{"status":"completed","report_format":"custom-json","report":` + report + `}`
	request(t, client, "POST", "/v1/batches/custom/results", session, body, 200)
	request(t, client, "POST", "/v1/batches/custom/results", session, body, 200)
	request(t, client, "POST", "/v1/batches/custom/results", session, body+" ", 409)
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	request(t, client, "POST", "/v1/batches/error/results", session,
		`{"status":"errored","report_format":"custom-json","error":{"kind":"load_error","message":"fixture failed"}}`, 200)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(source.results) != 2 {
		t.Fatalf("received %d results, want 2", len(source.results))
	}
	completed, errored := source.results[0], source.results[1]
	if completed.ReportFormat != "custom-json" || completed.Status != "completed" || string(completed.Report) != report {
		t.Fatalf("completed result changed: %+v", completed)
	}
	if errored.ReportFormat != "custom-json" || errored.Status != "errored" || errored.Error == nil || *errored.Error != (RunnerError{Kind: "load_error", Message: "fixture failed"}) {
		t.Fatalf("errored result changed: %+v", errored)
	}
}

func TestDeleteAndErroredResult(t *testing.T) {
	source := &testSource{batches: []Batch{{ID: "load-error"}, {ID: "unfinished"}}}
	h, err := newHost(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	client := socketClient(h.socket)
	defer client.CloseIdleConnections()
	session := handshake(t, client, "one")
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	request(t, client, "POST", "/v1/batches/load-error/results", session, `{"status":"errored","report_format":"rspec-json","error":{"kind":"runner_error","message":"load failed"}}`, 200)
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	request(t, client, "DELETE", "/v1/sessions/other", session, `{"reason":"signal"}`, 401)
	request(t, client, "DELETE", "/v1/sessions/"+session, session, `{}`, 400)
	request(t, client, "DELETE", "/v1/sessions/"+session, session, `{"reason":"signal","future":1}`, 200)
	request(t, client, "POST", "/v1/batches", session, `{}`, 401)
	h.mu.Lock()
	defer h.mu.Unlock()
	if fmt.Sprint(source.accepted) != "[load-error]" || fmt.Sprint(source.unresolved) != "[unfinished]" {
		t.Fatalf("accepted %v unresolved %v", source.accepted, source.unresolved)
	}
}

func TestWatchdogDespiteSlowRequestAndReplay(t *testing.T) {
	source := &testSource{batches: []Batch{{ID: "slow", TimeoutMS: 120}}}
	h, err := newHost(source, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	client := socketClient(h.socket)
	defer client.CloseIdleConnections()
	session := handshake(t, client, "one")
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	time.Sleep(40 * time.Millisecond)
	request(t, client, "POST", "/v1/batches", session, `{}`, 200)
	conn, err := net.Dial("unix", h.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "POST /v1/batches/slow/results HTTP/1.1\r\nHost: runner\r\nX-Bktec-Session: %s\r\nContent-Length: 100\r\n\r\n{", session)
	select {
	case err := <-h.fatal:
		if !strings.Contains(err.Error(), "batch timeout") {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("slow body postponed watchdog")
	}
	request(t, client, "POST", "/v1/batches/slow/results", session, completed, 409)
	h.mu.Lock()
	defer h.mu.Unlock()
	if fmt.Sprint(source.unresolved) != "[slow]" {
		t.Fatal(source.unresolved)
	}
}

func TestTerminalReasons(t *testing.T) {
	for _, reason := range []string{"plan_completed", "pool_consumed", "pool_errored", "terminating", "error"} {
		t.Run(reason, func(t *testing.T) {
			h, err := newHost(&testSource{done: reason}, Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			client := socketClient(h.socket)
			defer client.CloseIdleConnections()
			session := handshake(t, client, "one")
			got := string(request(t, client, "POST", "/v1/batches", session, `{}`, 200))
			if !strings.Contains(got, `"reason":"`+reason+`"`) {
				t.Fatal(got)
			}
		})
	}
}
