// A deterministic SDK adoption fixture. All writable state belongs to the
// application inside a disposable container; Obey is contacted only via SDK.
package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Obedience-Corp/obey-go-sdk/pkg/obey"
)

type job struct {
	LastError *failure         `json:"last_error,omitempty"`
	ID        string           `json:"id"`
	Key       string           `json:"key,omitempty"`
	Session   obey.SessionRef  `json:"session"`
	Prompt    string           `json:"prompt"`
	Options   obey.SendOptions `json:"options"`
	Ref       *obey.TurnRef    `json:"ref,omitempty"`
	Receipt   *obey.Acceptance `json:"receipt,omitempty"`
	Phase     string           `json:"phase"`
	LastCode  string           `json:"last_code,omitempty"`
}

// Raw SDK diagnostics can contain malformed/non-UTF8 stdout, so HTTP/journal
// transport uses explicit base64 byte fields rather than assuming valid JSON.
type diagnostic struct {
	Code, Message, Severity string
	DaemonCode              *int32
	Details                 []byte `json:",omitempty"`
	DetailsEncoding         string `json:",omitempty"`
}

func diagnostics(values []obey.Diagnostic) []diagnostic {
	if values == nil {
		return nil
	}
	out := make([]diagnostic, len(values))
	for i, value := range values {
		out[i] = diagnostic{Code: value.Code, Message: value.Message, Severity: value.Severity, DaemonCode: value.DaemonCode, Details: append([]byte(nil), value.Details...)}
		if len(value.Details) > 0 {
			out[i].DetailsEncoding = "base64"
		}
	}
	return out
}

type resultOutput struct {
	*obey.Result
	Diagnostics []diagnostic
}

func resultForHTTP(value *obey.Result) *resultOutput {
	if value == nil {
		return nil
	}
	return &resultOutput{Result: value, Diagnostics: diagnostics(value.Diagnostics)}
}

type failure struct {
	RawEncoding, DetailsEncoding                                       string `json:",omitempty"`
	ExitCode                                                           *int
	Details                                                            []byte
	Diagnostics                                                        []diagnostic
	Code, Message                                                      string
	Session                                                            obey.SessionRef
	TurnID, Key, CallID                                                string
	AcceptanceIndeterminate, DecisionIndeterminate, Deadline, Canceled bool
	DaemonCode                                                         *int32
	GRPCCode                                                           string
	Raw                                                                []byte
}
type probe struct {
	DiscoveryCallWallMS int64
	StatusCallWallMS    int64
	ProviderLatencyMS   *int64
	Source              string
}
type reply struct {
	Probe *probe `json:"probe,omitempty"`

	Job       *job               `json:"job,omitempty"`
	Session   *obey.SessionRef   `json:"session,omitempty"`
	Result    *resultOutput      `json:"result,omitempty"`
	State     *obey.TurnState    `json:"state,omitempty"`
	Cancel    *obey.CancelResult `json:"cancel,omitempty"`
	Approvals []obey.Approval    `json:"approvals,omitempty"`
	Error     *failure           `json:"error,omitempty"`
}

func describe(err error) *failure {
	if err == nil {
		return nil
	}
	out := &failure{Code: "service_error", Message: err.Error(), Deadline: errors.Is(err, context.DeadlineExceeded), Canceled: errors.Is(err, context.Canceled)}
	var typed *obey.Error
	if errors.As(err, &typed) {
		out.Code = typed.Code
		out.Session = typed.Session
		out.TurnID = typed.TurnID
		out.Key = typed.IdempotencyKey
		out.CallID = typed.CallID
		out.AcceptanceIndeterminate = typed.AcceptanceIndeterminate
		out.DecisionIndeterminate = typed.DecisionIndeterminate
		out.ExitCode = typed.ExitCode
		out.Details = append([]byte(nil), typed.Details...)
		if len(typed.Details) > 0 {
			out.DetailsEncoding = "base64"
		}
		out.Diagnostics = diagnostics(typed.Diagnostics)
		out.DaemonCode = typed.DaemonCode
		out.GRPCCode = typed.GRPCCode
		out.Raw = append([]byte(nil), typed.RawRecord...)
		if len(typed.RawRecord) > 0 {
			out.RawEncoding = "base64"
		}
	}
	return out
}
func input(message string) error {
	return &obey.Error{Code: "invalid_input", Cause: errors.New(message)}
}
func output(w http.ResponseWriter, status int, value reply) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func bad(w http.ResponseWriter, status int, err error) {
	output(w, status, reply{Error: describe(err)})
}
func decode(w http.ResponseWriter, r *http.Request, value any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return input("invalid request JSON: " + err.Error())
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return input("exactly one JSON request required")
	}
	return nil
}

type store struct {
	dir string
	mu  sync.Mutex
}

func (s *store) path(id string) string {
	digest := sha256.Sum256([]byte(id))
	return filepath.Join(s.dir, hex.EncodeToString(digest[:])+".json")
}
func (s *store) load(id string) (*job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		return nil, err
	}
	var out job
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	if out.ID != id {
		return nil, input("stored job identity differs")
	}
	return &out, nil
}

// save owns its write independently of the HTTP request's cancellation. A known
// accepted identity must still be journaled after the caller disconnects.
func (s *store) save(value *job) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.CreateTemp(s.dir, ".job-")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return err
	}
	if err := json.NewEncoder(file).Encode(value); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(name, s.path(value.ID)); err != nil {
		return err
	}
	dir, err := os.Open(s.dir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

type application struct {
	client      *obey.Client
	camp        string
	store       store
	submissions chan struct{}
}

func (a *application) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "{\"ready\":true}\n")
	})
	mux.HandleFunc("GET /diagnostics", a.diagnostics)
	mux.HandleFunc("POST /sessions", a.createSession)
	mux.HandleFunc("POST /jobs", a.createJob)
	mux.HandleFunc("GET /jobs/{job}", a.jobView)
	mux.HandleFunc("GET /jobs/{job}/state", a.readJob)
	mux.HandleFunc("GET /jobs/{job}/wait", a.readJob)
	mux.HandleFunc("POST /jobs/{job}/cancel", a.readJob)
	mux.HandleFunc("POST /jobs/{job}/retry", a.retryJob)
	mux.HandleFunc("GET /sessions/{session}/approvals", a.sessionAction)
	mux.HandleFunc("POST /sessions/{session}/approvals/{call}", a.sessionAction)
	mux.HandleFunc("POST /sessions/{session}/stop", a.sessionAction)
	mux.HandleFunc("GET /sessions/{session}/events", a.sessionAction)
	return mux
}

// Explicit read-only probe measures public SDK control/setup calls in a running
// fixture, independently of inference. It does not claim cold-OS startup latency.
func (a *application) diagnostics(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	_, err := a.client.RefreshCapabilities(r.Context())
	metrics := &probe{DiscoveryCallWallMS: time.Since(start).Milliseconds(), Source: "fresh SDK discovery and unary Ping wall time; no inference"}
	if err == nil {
		start = time.Now()
		_, err = a.client.Status(r.Context())
		metrics.StatusCallWallMS = time.Since(start).Milliseconds()
	}
	status := 200
	if err != nil {
		status = 502
	}
	output(w, status, reply{Probe: metrics, Error: describe(err)})
}
func (a *application) createSession(w http.ResponseWriter, r *http.Request) {
	var options obey.SessionOptions
	if err := decode(w, r, &options); err != nil {
		bad(w, 400, err)
		return
	}
	session, err := a.client.CreateSession(r.Context(), &options)
	out := reply{Error: describe(err)}
	if session != nil {
		ref := session.Ref()
		out.Session = &ref
	}
	status := 200
	if err != nil {
		status = 502
	}
	output(w, status, out)
}
func (a *application) enter(ctx context.Context) error {
	select {
	case a.submissions <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (a *application) createJob(w http.ResponseWriter, r *http.Request) {
	var request job
	if err := decode(w, r, &request); err != nil {
		bad(w, 400, err)
		return
	}
	if strings.TrimSpace(request.ID) == "" || request.Session.CampID == "" || request.Session.SessionID == "" || strings.TrimSpace(request.Prompt) == "" {
		bad(w, 400, input("job ID, exact session scope and prompt required"))
		return
	}
	if err := a.enter(r.Context()); err != nil {
		bad(w, 408, err)
		return
	}
	defer func() { <-a.submissions }()
	if existing, err := a.store.load(request.ID); err == nil {
		output(w, 200, reply{Job: existing})
		return
	} else if !errors.Is(err, os.ErrNotExist) {
		bad(w, 500, err)
		return
	}
	// Ignore any caller-supplied derived state: only the request's original input
	// and app-owned key are recorded before the first SDK I/O.
	request.Ref = nil
	request.Receipt = nil
	request.Phase = "prepared"
	request.LastCode = ""
	request.LastError = nil
	if request.Key == "" {
		var key [16]byte
		if _, err := rand.Read(key[:]); err != nil {
			bad(w, 500, err)
			return
		}
		request.Key = hex.EncodeToString(key[:])
	}
	request.Options.IdempotencyKey = request.Key
	if err := a.store.save(&request); err != nil {
		bad(w, 500, err)
		return
	}
	a.submit(w, r, &request)
}
func (a *application) submit(w http.ResponseWriter, r *http.Request, record *job) {
	session, err := a.client.OpenSession(r.Context(), record.Session)
	if err != nil {
		record.Phase = "not_submitted"
		record.LastError = describe(err)
		record.LastCode = record.LastError.Code
		if saveErr := a.store.save(record); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
		output(w, 502, reply{Job: record, Error: describe(err)})
		return
	}
	options := record.Options
	options.IdempotencyKey = record.Key
	turn, err := session.Send(r.Context(), record.Prompt, &options)
	if turn != nil {
		ref := turn.Ref()
		record.Ref = &ref
		record.Receipt = turn.Acceptance()
	}
	record.Phase = "accepted"
	record.LastCode = ""
	record.LastError = nil
	if err != nil {
		record.LastError = describe(err)
		record.LastCode = record.LastError.Code
		record.Phase = "uncertain"
		var typed *obey.Error
		if errors.As(err, &typed) && !typed.AcceptanceIndeterminate && turn == nil {
			record.Phase = "not_submitted"
		}
	}
	if saveErr := a.store.save(record); saveErr != nil {
		err = errors.Join(err, saveErr)
	}
	status := 200
	if err != nil {
		status = 502
	}
	output(w, status, reply{Job: record, Error: describe(err)})
}
func (a *application) retryJob(w http.ResponseWriter, r *http.Request) {
	if err := a.enter(r.Context()); err != nil {
		bad(w, 408, err)
		return
	}
	defer func() { <-a.submissions }()
	record, err := a.store.load(r.PathValue("job"))
	if err != nil {
		bad(w, 404, err)
		return
	}
	if record.Key == "" || record.Session.CampID == "" || record.Session.SessionID == "" {
		bad(w, 409, input("original stored key and exact session identity required for explicit retry"))
		return
	}
	if record.Ref != nil && (record.Ref.SessionRef != record.Session || record.Ref.TurnID == "" || (record.Ref.IdempotencyKey != "" && record.Ref.IdempotencyKey != record.Key)) {
		bad(w, 409, input("stored accepted identity/key contradicts original submission"))
		return
	}
	// Caller explicitly requested retry of unchanged stored input with original key.
	// Read-only state/wait never enter this branch and never require key metadata.
	a.submit(w, r, record)
}
func (a *application) jobView(w http.ResponseWriter, r *http.Request) {
	record, err := a.store.load(r.PathValue("job"))
	if err != nil {
		bad(w, 404, err)
		return
	}
	output(w, 200, reply{Job: record})
}
func (a *application) readJob(w http.ResponseWriter, r *http.Request) {
	record, err := a.store.load(r.PathValue("job"))
	if err != nil {
		bad(w, 404, err)
		return
	}
	if record.Ref == nil {
		if record.Phase == "not_submitted" {
			known := record.LastError
			if known == nil {
				code := record.LastCode
				if code == "" {
					code = "protocol_error"
				}
				known = &failure{Code: code, Message: "stored request was not submitted; original diagnostic details unavailable", Session: record.Session}
			}
			output(w, 502, reply{Job: record, Error: known})
			return
		}
		output(w, 409, reply{Job: record, Error: describe(&obey.Error{Code: "indeterminate_acceptance", IdempotencyKey: record.Key, AcceptanceIndeterminate: true, Cause: errors.New("no accepted turn identity stored; explicit reconciliation required")})})
		return
	}
	if record.Ref.SessionRef != record.Session {
		bad(w, 409, &obey.Error{Code: "scope_mismatch", Session: record.Ref.SessionRef, TurnID: record.Ref.TurnID, Cause: errors.New("stored turn ref contradicts original session scope")})
		return
	}
	turn, err := a.client.OpenTurn(r.Context(), *record.Ref)
	out := reply{Job: record}
	if err == nil {
		switch {
		case r.Method == http.MethodPost:
			out.Cancel, err = turn.Cancel(r.Context())
		case strings.HasSuffix(r.URL.Path, "/wait"):
			var result *obey.Result
			result, err = turn.Wait(r.Context())
			out.Result = resultForHTTP(result)
		default:
			out.State, err = turn.State(r.Context())
		}
	}
	out.Error = describe(err)
	status := 200
	if err != nil {
		status = 502
	}
	output(w, status, out)
}
func (a *application) sessionAction(w http.ResponseWriter, r *http.Request) {
	camp := r.URL.Query().Get("camp")
	if camp == "" {
		camp = a.camp
	}
	session, err := a.client.OpenSession(r.Context(), obey.SessionRef{CampID: camp, SessionID: r.PathValue("session")})
	if err != nil {
		bad(w, 502, err)
		return
	}
	out := reply{}
	switch {
	case strings.HasSuffix(r.URL.Path, "/events"):
		stream, err := session.Attach(r.Context(), obey.AttachOptions{NoBackfill: true})
		if err != nil {
			bad(w, 502, err)
			return
		}
		defer stream.Close()
		w.Header().Set("Content-Type", "application/x-ndjson")
		encoder := json.NewEncoder(w)
		controller := http.NewResponseController(w)
		for stream.Next() {
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if err := encoder.Encode(stream.Event()); err != nil {
				return
			}
			if err := controller.Flush(); err != nil {
				return
			}
		}
		if r.Context().Err() == nil && stream.Err() != nil {
			_ = encoder.Encode(reply{Error: describe(stream.Err())})
		}
		return
	case strings.HasSuffix(r.URL.Path, "/stop"):
		err = session.Stop(r.Context())
	case r.Method == http.MethodPost:
		var decision struct {
			Approve *bool  `json:"approve"`
			Reason  string `json:"reason"`
		}
		if err := decode(w, r, &decision); err != nil {
			bad(w, 400, err)
			return
		}
		if decision.Approve == nil {
			bad(w, 400, input("explicit approve boolean required"))
			return
		}
		err = session.RespondApproval(r.Context(), obey.ApprovalDecision{CallID: r.PathValue("call"), Approve: *decision.Approve, Reason: decision.Reason})
	default:
		out.Approvals, err = session.PendingApprovals(r.Context())
	}
	out.Error = describe(err)
	status := 200
	if err != nil {
		status = 502
	}
	output(w, status, out)
}
func request(ctx context.Context, url, method, body string, timeout time.Duration, records int) error {
	caller, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(caller, method, url, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(req)
	out := struct {
		Status int             `json:"status"`
		Body   json.RawMessage `json:"body,omitempty"`
		Error  *failure        `json:"error,omitempty"`
	}{Error: describe(err)}
	if err == nil {
		defer response.Body.Close()
		out.Status = response.StatusCode
		if records > 0 {
			decoder := json.NewDecoder(io.LimitReader(response.Body, (64<<20)+1))
			var observed []json.RawMessage
			for range records {
				var record json.RawMessage
				if err := decoder.Decode(&record); err != nil {
					out.Error = describe(err)
					break
				}
				observed = append(observed, record)
			}
			out.Body, err = json.Marshal(observed)
			if err != nil {
				return err
			}
			// Closing this response detaches observation; no control endpoint runs.
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, (64<<20)+1))
		if readErr != nil {
			out.Error = describe(readErr)
		} else if len(data) > 64<<20 {
			out.Error = describe(input("response exceeded fixture client bound"))
		} else if !json.Valid(data) {
			out.Error = describe(input("response is not one JSON document"))
		} else {
			out.Body = data
		}
	}
	return json.NewEncoder(os.Stdout).Encode(out)
}
func run() error {
	binary := flag.String("binary", "/usr/local/bin/obey", "installed CLI")
	socket := flag.String("socket", "/tmp/daemon.sock", "exact fixture socket")
	camp := flag.String("camp", "", "explicit camp ID")
	dir := flag.String("store", "/data/sdk-service-jobs", "application-owned persistent store")
	address := flag.String("listen", "127.0.0.1:8089", "local fixture HTTP listener")
	requestFile := flag.String("request-file", "", "container-private synthetic HTTP body file")
	requestURL := flag.String("request-url", "", "HTTP fixture client mode")
	method := flag.String("request-method", "GET", "HTTP method")
	body := flag.String("request-body", "", "synthetic JSON body")
	timeout := flag.Duration("request-timeout", 10*time.Second, "HTTP caller lifetime, separate from turn controls")
	records := flag.Int("request-records", 0, "read this many NDJSON records, then explicitly detach")
	flag.Parse()
	if *records < 0 {
		return input("negative record count")
	}
	if os.Getenv("OBEY_SDK_TEST_CONTAINER") != "1" {
		return errors.New("service journey requires isolated fixture")
	}
	if *requestURL != "" {
		if *requestFile != "" {
			data, err := os.ReadFile(*requestFile)
			if err != nil {
				return err
			}
			*body = string(data)
		}
		return request(context.Background(), *requestURL, *method, *body, *timeout, *records)
	}
	if *camp == "" {
		return input("explicit --camp required")
	}
	if err := os.MkdirAll(*dir, 0700); err != nil {
		return err
	}
	client, err := obey.NewClient(*binary, obey.WithSocket(*socket), obey.WithCamp(*camp), obey.WithSessionDefaults(obey.SessionOptions{Provider: "crash-provider", Model: "crash-model"}))
	if err != nil {
		return err
	}
	defer client.Close()
	app := &application{client: client, camp: *camp, store: store{dir: *dir}, submissions: make(chan struct{}, 1)}
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: app.routes(), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan error, 1)
	go func() { done <- server.Serve(listener) }()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	select {
	case err := <-done:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
		_ = server.Close()
		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	return nil
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
