package runnerexec

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/buildkite/test-engine-client/v3/internal/plan"
)

type host struct {
	mu                sync.Mutex
	source            Source
	opts              Options
	dir, socket       string
	server            *http.Server
	ready             chan struct{}
	fatal             chan error
	done              chan struct{}
	instance, session string
	retired           map[string]bool
	formats           map[plan.TestCaseFormat]bool
	outstanding       *Batch
	deadline          time.Time
	watchdog          *time.Timer
	results           map[string][32]byte
	issued            map[string]bool
	terminal          string
	stopping          bool
}

func newHost(source Source, opts Options) (*host, error) {
	if source == nil {
		return nil, errors.New("persistent runner requires a source")
	}
	if opts.StartupTimeout < 0 || opts.BatchTimeout < 0 || opts.ShutdownTimeout < 0 {
		return nil, errors.New("negative runner timeout")
	}
	if opts.StartupTimeout == 0 {
		opts.StartupTimeout = 5 * time.Minute
	}
	if opts.BatchTimeout == 0 {
		opts.BatchTimeout = 10 * time.Minute
	}
	if opts.ShutdownTimeout == 0 {
		opts.ShutdownTimeout = 90 * time.Second
	}
	h := &host{source: source, opts: opts, ready: make(chan struct{}), fatal: make(chan error, 1), done: make(chan struct{}), retired: map[string]bool{}, results: map[string][32]byte{}, issued: map[string]bool{}}
	dir, err := os.MkdirTemp("", "bktec-")
	if err != nil {
		return nil, err
	}
	h.dir, h.socket = dir, filepath.Join(dir, "runner.sock")
	listener, err := net.Listen("unix", h.socket)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	if err = os.Chmod(h.socket, 0600); err != nil {
		listener.Close()
		os.RemoveAll(dir)
		return nil, err
	}
	h.server = &http.Server{Handler: h, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: time.Minute, MaxHeaderBytes: 8192}
	go func() {
		if err := h.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.fail(err)
		}
	}()
	return h, nil
}

func (h *host) close() {
	h.mu.Lock()
	h.stopping = true
	h.unresolved(errors.New("runner host closed"))
	h.mu.Unlock()
	h.server.Close()
	os.RemoveAll(h.dir)
}

func (h *host) unresolved(err error) {
	if h.watchdog != nil {
		h.watchdog.Stop()
	}
	if h.outstanding != nil {
		batch := *h.outstanding
		h.outstanding = nil
		h.source.Unresolved(batch, err)
	}
}

func (h *host) fail(err error) {
	h.stopping = true
	h.unresolved(err)
	select {
	case h.fatal <- err:
	default:
	}
}

func reply(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func reject(w http.ResponseWriter, status int, message string) {
	reply(w, status, map[string]string{"error": message})
}

func object(body []byte, target any) bool {
	return len(bytes.TrimSpace(body)) > 0 && bytes.TrimSpace(body)[0] == '{' && json.Unmarshal(body, target) == nil
}

// Keep network writes outside the state lock so slow/disconnected clients cannot
// postpone the watchdog or serialize unrelated authenticated requests.
type responseBuffer struct {
	bytes.Buffer
	header http.Header
	status int
}

func (b *responseBuffer) Header() http.Header    { return b.header }
func (b *responseBuffer) WriteHeader(status int) { b.status = status }

func (h *host) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<20))
	if err != nil {
		reject(w, 400, "invalid body")
		return
	}
	response := &responseBuffer{header: make(http.Header), status: 200}
	h.mu.Lock()
	h.handle(response, r, body)
	h.mu.Unlock()
	for key, values := range response.header {
		w.Header()[key] = values
	}
	w.WriteHeader(response.status)
	_, _ = w.Write(response.Bytes())
}

func (h *host) handle(w http.ResponseWriter, r *http.Request, body []byte) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) > 0 && strings.HasPrefix(parts[0], "v") && parts[0] != "v1" {
		reply(w, 426, map[string][]int{"supported": {1}})
		return
	}
	if len(parts) < 2 || parts[0] != "v1" {
		reject(w, 404, "unknown endpoint")
		return
	}
	handshake := r.Method == "POST" && r.URL.Path == "/v1/sessions"
	if !handshake && (h.session == "" || r.Header.Get("X-Bktec-Session") != h.session) {
		reject(w, 401, "unknown session")
		return
	}
	if h.outstanding != nil && !time.Now().Before(h.deadline) {
		h.fail(errors.New("batch timeout"))
	}
	if handshake {
		h.handshake(w, body)
		return
	}
	switch {
	case r.Method == "POST" && r.URL.Path == "/v1/batches":
		var request map[string]json.RawMessage
		if !object(body, &request) {
			reject(w, 400, "expected JSON object")
			return
		}
		h.pull(w)
	case r.Method == "POST" && len(parts) == 4 && parts[1] == "batches" && parts[3] == "results":
		h.result(w, parts[2], body)
	case r.Method == "DELETE" && len(parts) == 3 && parts[1] == "sessions":
		if parts[2] != h.session {
			reject(w, 401, "unknown session")
			return
		}
		var request struct {
			Reason string `json:"reason"`
		}
		if !object(body, &request) || request.Reason == "" {
			reject(w, 400, "reason required")
			return
		}
		h.retired[h.instance] = true
		h.session = ""
		h.fail(fmt.Errorf("runner departed: %s", request.Reason))
		reply(w, 200, struct{}{})
	default:
		reject(w, 404, "unknown endpoint")
	}
}

func (h *host) handshake(w http.ResponseWriter, body []byte) {
	var request SessionRequest
	if !object(body, &request) || request.InstanceID == "" || len(request.Capabilities.SelectorFormats) == 0 {
		reject(w, 400, "instance_id and selector_formats required")
		return
	}
	formats := map[plan.TestCaseFormat]bool{}
	for _, format := range request.Capabilities.SelectorFormats {
		switch format {
		case plan.TestCaseFormatSelector, plan.TestCaseFormatFile, plan.TestCaseFormatExample:
			formats[format] = true
		default:
			reject(w, 400, "unsupported selector format")
			return
		}
	}
	if h.retired[request.InstanceID] {
		reject(w, 401, "retired instance")
		return
	}
	if h.stopping {
		reject(w, 409, "host terminating")
		return
	}
	if request.InstanceID != h.instance {
		id := make([]byte, 32)
		if _, err := rand.Read(id); err != nil {
			h.fail(err)
			reject(w, 500, "session generation failed")
			return
		}
		if h.instance != "" {
			h.retired[h.instance] = true
			h.unresolved(errors.New("session replaced"))
		}
		h.instance, h.session, h.formats = request.InstanceID, hex.EncodeToString(id), formats
		h.results = map[string][32]byte{}
		select {
		case <-h.ready:
		default:
			close(h.ready)
		}
	}
	response := SessionResponse{SessionID: h.session}
	response.Poll.MaxWaitMS = 30000
	reply(w, 201, response)
}

func (h *host) pull(w http.ResponseWriter) {
	if h.stopping {
		reply(w, 200, PullResponse{Type: "done", Reason: "terminating"})
		return
	}
	if h.terminal != "" {
		reply(w, 200, PullResponse{Type: "done", Reason: h.terminal})
		return
	}
	if h.outstanding == nil {
		batch, reason, err := h.source.Next()
		if err == nil && reason != "" && (!validReason(reason) || batch != nil) {
			err = errors.New("invalid source terminal response")
		}
		if err != nil {
			h.fail(err)
			reply(w, 200, PullResponse{Type: "done", Reason: "error"})
			return
		}
		if reason != "" {
			h.terminal = reason
			close(h.done)
			reply(w, 200, PullResponse{Type: "done", Reason: reason})
			return
		}
		if batch == nil {
			reply(w, 200, PullResponse{Type: "wait", RetryAfterMS: 1000})
			return
		}
		copy := *batch
		copy.Tests = append([]plan.TestCase{}, batch.Tests...)
		if copy.TimeoutMS == 0 {
			copy.TimeoutMS = h.opts.BatchTimeout.Milliseconds()
		}
		if copy.ID == "" || url.PathEscape(copy.ID) != copy.ID || h.issued[copy.ID] || copy.TimeoutMS <= 0 || copy.TimeoutMS > int64((1<<63-1)/time.Millisecond) {
			h.fail(errors.New("invalid source batch"))
			reject(w, 500, "invalid source batch")
			return
		}
		for _, test := range copy.Tests {
			if !h.formats[test.Format] {
				h.fail(errors.New("batch selector not supported by runner"))
				reject(w, 400, "unsupported selector format")
				return
			}
		}
		h.outstanding = &copy
		h.issued[copy.ID] = true
		h.deadline = time.Now().Add(time.Duration(copy.TimeoutMS) * time.Millisecond)
		id, session := copy.ID, h.session
		h.watchdog = time.AfterFunc(time.Until(h.deadline), func() {
			h.mu.Lock()
			defer h.mu.Unlock()
			if h.session == session && h.outstanding != nil && h.outstanding.ID == id {
				h.fail(errors.New("batch timeout"))
			}
		})
	}
	reply(w, 200, PullResponse{Type: "batch", Batch: h.outstanding})
}

func (h *host) result(w http.ResponseWriter, id string, body []byte) {
	digest := sha256.Sum256(body)
	if previous, ok := h.results[id]; ok {
		if previous == digest {
			reply(w, 200, struct{}{})
		} else {
			reject(w, 409, "result differs")
		}
		return
	}
	if h.stopping || h.outstanding == nil || h.outstanding.ID != id {
		reject(w, 409, "batch not outstanding")
		return
	}
	var result Result
	var report map[string]json.RawMessage
	if !object(body, &result) || result.ReportFormat == "" ||
		!(result.Status == "completed" && result.Error == nil && object(result.Report, &report) || result.Status == "errored" && len(result.Report) == 0 && result.Error != nil && result.Error.Kind != "" && result.Error.Message != "") {
		reject(w, 400, "invalid result envelope")
		return
	}
	// Parsing a large native report also counts against the dispatch deadline.
	if !time.Now().Before(h.deadline) {
		h.fail(errors.New("batch timeout"))
		reject(w, 409, "batch timed out")
		return
	}
	batch := *h.outstanding
	h.watchdog.Stop()
	h.outstanding = nil
	h.results[id] = digest
	h.source.Accepted(batch, result)
	reply(w, 200, struct{}{})
}
