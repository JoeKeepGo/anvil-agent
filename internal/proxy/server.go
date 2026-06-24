package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/JoeKeepGo/anvil-agent/internal/config"
	"github.com/JoeKeepGo/anvil-agent/internal/incus"
	"github.com/JoeKeepGo/anvil-agent/internal/lifecycle"
	"github.com/JoeKeepGo/anvil-agent/internal/network"
	"github.com/JoeKeepGo/anvil-agent/internal/state"
	"github.com/coder/websocket"
)

type Server struct {
	cfg            *config.Config
	incus          incusBackend
	mu             sync.RWMutex
	clients        map[*client]struct{}
	eventCh        chan incus.Event
	reporter       state.Reporter
	networkState   NetworkStateReporter
	networkApplier NetworkApplier
	lifecycle      LifecycleService
	upgrader       websocket.AcceptOptions
}

type incusBackend interface {
	Execute(context.Context, *incus.ProxyRequest) *incus.ProxyResponse
	ListenEvents(context.Context, chan<- incus.Event) error
}

// LifecycleService validates and dispatches trusted VM lifecycle requests
// for the allowlisted Incus instance operations (create/start/stop/restart/
// delete). It never performs arbitrary Incus writes, shell execution,
// snapshots, migration, console, or file operations.
type LifecycleService interface {
	Handle(ctx context.Context, method string, path string, body json.RawMessage) lifecycle.Result
	Capabilities() lifecycle.CapabilitiesResponse
}

// NetworkStateReporter builds the trusted /agent/v1/network/state report.
type NetworkStateReporter interface {
	NetworkState(context.Context) (network.NetworkState, error)
}

// NetworkApplier validates and plans Anvil-managed network apply requests.
// It never executes arbitrary shell text.
type NetworkApplier interface {
	Apply(context.Context, json.RawMessage) (network.ApplyResponse, error)
}

type client struct {
	conn *websocket.Conn
	ctx  context.Context
}

var eventWriteTimeout = 2 * time.Second

func NewServer(cfg *config.Config, incusClient *incus.Client) *Server {
	return NewServerWithReporter(cfg, incusClient, nil)
}

func NewServerWithReporter(cfg *config.Config, incusClient incusBackend, reporter state.Reporter) *Server {
	return &Server{
		cfg:      cfg,
		incus:    incusClient,
		clients:  make(map[*client]struct{}),
		eventCh:  make(chan incus.Event, 64),
		reporter: reporter,
		upgrader: websocket.AcceptOptions{
			InsecureSkipVerify: true,
		},
	}
}

// NewServerWithNetwork wires the trusted state reporter and the Anvil-managed
// network state reporter and applier alongside the Incus backend.
func NewServerWithNetwork(
	cfg *config.Config,
	incusClient incusBackend,
	reporter state.Reporter,
	networkState NetworkStateReporter,
	networkApplier NetworkApplier,
) *Server {
	return &Server{
		cfg:            cfg,
		incus:          incusClient,
		clients:        make(map[*client]struct{}),
		eventCh:        make(chan incus.Event, 64),
		reporter:       reporter,
		networkState:   networkState,
		networkApplier: networkApplier,
		upgrader: websocket.AcceptOptions{
			InsecureSkipVerify: true,
		},
	}
}

// NewServerWithLifecycle wires the trusted VM lifecycle service alongside the
// existing state reporter/network components.
func NewServerWithLifecycle(
	cfg *config.Config,
	incusClient incusBackend,
	reporter state.Reporter,
	networkState NetworkStateReporter,
	networkApplier NetworkApplier,
	lifecycleSvc LifecycleService,
) *Server {
	return &Server{
		cfg:            cfg,
		incus:          incusClient,
		clients:        make(map[*client]struct{}),
		eventCh:        make(chan incus.Event, 64),
		reporter:       reporter,
		networkState:   networkState,
		networkApplier: networkApplier,
		lifecycle:      lifecycleSvc,
		upgrader: websocket.AcceptOptions{
			InsecureSkipVerify: true,
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	go s.listenEvents(ctx)
	go s.forwardEvents(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	server := &http.Server{
		Addr:    s.cfg.ListenAddr(),
		Handler: mux,
	}

	log.Printf("Anvil agent listening on %s", s.cfg.ListenAddr())

	go func() {
		<-ctx.Done()
		server.Shutdown(context.Background())
	}()

	return server.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"ok"}`))
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.cfg.AuthToken != "" {
		token := r.Header.Get("Authorization")
		if token != "Bearer "+s.cfg.AuthToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	conn, err := websocket.Accept(w, r, &s.upgrader)
	if err != nil {
		log.Printf("websocket upgrade failed: %v", err)
		return
	}

	c := &client{conn: conn, ctx: r.Context()}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, c)
		s.mu.Unlock()
		conn.CloseNow()
	}()

	s.readLoop(c)
}

func (s *Server) readLoop(c *client) {
	for {
		_, msg, err := c.conn.Read(c.ctx)
		if err != nil {
			return
		}

		var req incus.ProxyRequest
		if err := json.Unmarshal(msg, &req); err != nil {
			s.sendProtocolError(c, "", "invalid request: "+err.Error())
			continue
		}

		if err := validateProxyRequest(req); err != nil {
			s.sendProtocolError(c, req.ID, err.Error())
			continue
		}

		go s.handleRequest(c, &req)
	}
}

func (s *Server) handleRequest(c *client, req *incus.ProxyRequest) {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	if s.isAgentRequest(req.Path) {
		s.handleAgentRequest(c, ctx, req)
		return
	}

	resp := s.incus.Execute(ctx, req)
	s.writeResponse(c, req.ID, resp)
}

func (s *Server) handleAgentRequest(c *client, ctx context.Context, req *incus.ProxyRequest) {
	switch {
	case req.Path == "/agent/v1/lifecycle/capabilities" || strings.HasPrefix(req.Path, "/agent/v1/lifecycle/"):
		s.handleLifecycle(c, ctx, req)
	case req.Path == "/agent/v1/state":
		s.handleAgentState(c, ctx, req)
	case req.Path == "/agent/v1/network/state":
		s.handleNetworkState(c, ctx, req)
	case req.Path == "/agent/v1/network/apply":
		s.handleNetworkApply(c, ctx, req)
	default:
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusNotFound, Error: "unknown agent path"})
	}
}

func (s *Server) handleLifecycle(c *client, ctx context.Context, req *incus.ProxyRequest) {
	if s.lifecycle == nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusServiceUnavailable, Error: "lifecycle service not configured"})
		return
	}
	// Capabilities is a safe GET advertisement; it requires no body and never
	// reaches the Incus backend.
	if req.Path == "/agent/v1/lifecycle/capabilities" {
		if req.Method != http.MethodGet {
			s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusBadRequest, Error: "unsupported method for lifecycle capabilities"})
			return
		}
		body, err := json.Marshal(s.lifecycle.Capabilities())
		if err != nil {
			s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "marshal lifecycle capabilities"})
			return
		}
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusOK, Body: body})
		return
	}

	result := s.lifecycle.Handle(ctx, req.Method, req.Path, req.Body)
	if result.Err != nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{
			ID:     req.ID,
			Status: result.Err.Status,
			Error:  result.Err.Error(),
		})
		return
	}
	status := result.Status
	if status == 0 {
		status = http.StatusServiceUnavailable
	}
	s.writeResponse(c, req.ID, &incus.ProxyResponse{
		ID:     req.ID,
		Status: status,
		Body:   result.Body,
	})
}

func (s *Server) handleAgentState(c *client, ctx context.Context, req *incus.ProxyRequest) {
	if req.Method != http.MethodGet {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusBadRequest, Error: "unsupported method for agent state"})
		return
	}
	if s.reporter == nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "agent state reporter not configured"})
		return
	}

	report, err := s.reporter.Report(ctx)
	if err != nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "build agent state report: " + err.Error()})
		return
	}
	body, err := json.Marshal(report)
	if err != nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "marshal agent state report: " + err.Error()})
		return
	}
	s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusOK, Body: body})
}

func (s *Server) handleNetworkState(c *client, ctx context.Context, req *incus.ProxyRequest) {
	if req.Method != http.MethodGet {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusBadRequest, Error: "unsupported method for network state"})
		return
	}
	if s.networkState == nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusServiceUnavailable, Error: "network state reporter not configured"})
		return
	}
	state, err := s.networkState.NetworkState(ctx)
	if err != nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "build network state report: " + err.Error()})
		return
	}
	body, err := json.Marshal(state)
	if err != nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "marshal network state report: " + err.Error()})
		return
	}
	s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusOK, Body: body})
}

func (s *Server) handleNetworkApply(c *client, ctx context.Context, req *incus.ProxyRequest) {
	if req.Method != http.MethodPost {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusBadRequest, Error: "unsupported method for network apply"})
		return
	}
	if s.networkApplier == nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusServiceUnavailable, Error: "network applier not configured"})
		return
	}
	result, err := s.networkApplier.Apply(ctx, req.Body)
	if err != nil {
		status := http.StatusInternalServerError
		if applyErr, ok := err.(*network.ApplyError); ok {
			status = applyErr.Status
			err = fmt.Errorf("%s", applyErr.Message)
		}
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: status, Error: err.Error()})
		return
	}
	body, err := json.Marshal(result)
	if err != nil {
		s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusInternalServerError, Error: "marshal network apply response: " + err.Error()})
		return
	}
	s.writeResponse(c, req.ID, &incus.ProxyResponse{ID: req.ID, Status: http.StatusOK, Body: body})
}

func (s *Server) writeResponse(c *client, id string, resp *incus.ProxyResponse) {
	if resp == nil {
		resp = &incus.ProxyResponse{ID: id, Status: http.StatusInternalServerError, Error: "empty response"}
	}
	data, err := json.Marshal(resp)
	if err != nil {
		s.sendError(c, id, "marshal response: "+err.Error())
		return
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	writeCtx, writeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer writeCancel()

	if err := c.conn.Write(writeCtx, websocket.MessageText, data); err != nil {
		log.Printf("write response to client: %v", err)
	}
}

func (s *Server) isAgentRequest(path string) bool {
	return path == "/agent/v1/state" || path == "/agent/v1/" || (len(path) > len("/agent/v1/") && path[:len("/agent/v1/")] == "/agent/v1/")
}

func (s *Server) sendError(c *client, id string, message string) {
	resp := &incus.ProxyResponse{ID: id, Status: 500, Error: message}
	data, _ := json.Marshal(resp)
	c.conn.Write(context.Background(), websocket.MessageText, data)
}

func (s *Server) sendProtocolError(c *client, id string, message string) {
	resp := &incus.ProxyResponse{ID: id, Status: http.StatusBadRequest, Error: message}
	data, _ := json.Marshal(resp)
	c.conn.Write(context.Background(), websocket.MessageText, data)
}

func (s *Server) listenEvents(ctx context.Context) {
	err := s.incus.ListenEvents(ctx, s.eventCh)
	if err != nil {
		log.Printf("incus event stream ended: %v (will retry in 5s)", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			go s.listenEvents(ctx)
		}
		return
	}
}

func (s *Server) forwardEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.eventCh:
			s.broadcastEvent(event)
		}
	}
}

func (s *Server) broadcastEvent(event incus.Event) {
	s.mu.RLock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.RUnlock()

	msg, err := json.Marshal(event)
	if err != nil {
		return
	}

	for _, c := range clients {
		go func(c *client) {
			writeCtx, cancel := context.WithTimeout(context.Background(), eventWriteTimeout)
			defer cancel()
			if err := c.conn.Write(writeCtx, websocket.MessageText, msg); err != nil {
				log.Printf("write event to client: %v", err)
			}
		}(c)
	}
}
