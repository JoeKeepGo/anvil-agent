package proxy

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/JoeKeepGo/anvil-agent/internal/config"
	"github.com/JoeKeepGo/anvil-agent/internal/incus"
	"github.com/coder/websocket"
)

type Server struct {
	cfg      *config.Config
	incus    *incus.Client
	mu       sync.RWMutex
	clients  map[*client]struct{}
	eventCh  chan incus.Event
	upgrader websocket.AcceptOptions
}

type client struct {
	conn *websocket.Conn
	ctx  context.Context
}

func NewServer(cfg *config.Config, incusClient *incus.Client) *Server {
	return &Server{
		cfg:     cfg,
		incus:   incusClient,
		clients: make(map[*client]struct{}),
		eventCh: make(chan incus.Event, 64),
		upgrader: websocket.AcceptOptions{
			InsecureSkipVerify: true,
		},
	}
}

func (s *Server) Start(ctx context.Context) error {
	go s.broadcastEvents(ctx)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/health", s.handleHealth)

	server := &http.Server{
		Addr:    s.cfg.ListenAddr(),
		Handler: mux,
	}

	log.Printf("Anvil agent listening on %s", s.cfg.ListenAddr())
	log.Printf("Incus socket: %s", s.cfg.IncusSocket)

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
			s.sendError(c, "", "invalid request: "+err.Error())
			continue
		}

		go s.handleRequest(c, &req)
	}
}

func (s *Server) handleRequest(c *client, req *incus.ProxyRequest) {
	ctx, cancel := context.WithTimeout(c.ctx, 30*time.Second)
	defer cancel()

	resp := s.incus.Execute(ctx, req)

	data, err := json.Marshal(resp)
	if err != nil {
		s.sendError(c, req.ID, "marshal response: "+err.Error())
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

func (s *Server) sendError(c *client, id string, message string) {
	resp := &incus.ProxyResponse{ID: id, Status: 500, Error: message}
	data, _ := json.Marshal(resp)
	c.conn.Write(context.Background(), websocket.MessageText, data)
}

func (s *Server) broadcastEvents(ctx context.Context) {
	err := s.incus.ListenEvents(ctx, s.eventCh)
	if err != nil {
		log.Printf("incus event stream ended: %v (will retry in 5s)", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
			go s.broadcastEvents(ctx)
		}
		return
	}
}

func (s *Server) broadcastEvent(event incus.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	msg, err := json.Marshal(event)
	if err != nil {
		return
	}

	for c := range s.clients {
		writeCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		c.conn.Write(writeCtx, websocket.MessageText, msg)
		cancel()
	}
}
