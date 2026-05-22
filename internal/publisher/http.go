package publisher

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sync"

	"github.com/overseer/overseer/pkg/nodestate"
)

// HTTPPublisher serves the latest NodeState as JSON at GET /state.
// Obtain a listener from net.Listen and pass it to Serve.
type HTTPPublisher struct {
	mu     sync.RWMutex
	snap   *nodestate.NodeState
	server *http.Server
}

func NewHTTP(addr string) *HTTPPublisher {
	h := &HTTPPublisher{}
	mux := http.NewServeMux()
	mux.HandleFunc("/state", h.handleState)
	h.server = &http.Server{Addr: addr, Handler: mux}
	return h
}

func (h *HTTPPublisher) Publish(_ context.Context, ns nodestate.NodeState) error {
	h.mu.Lock()
	h.snap = &ns
	h.mu.Unlock()
	return nil
}

// ListenAndServe binds to the address given to NewHTTP and serves until ctx is done.
func (h *HTTPPublisher) ListenAndServe(ctx context.Context) error {
	go func() {
		<-ctx.Done()
		h.server.Shutdown(context.Background()) //nolint:errcheck
	}()
	if err := h.server.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

// Serve accepts connections on ln and serves until ctx is done. Use this in
// tests where the port must be chosen before the server starts.
func (h *HTTPPublisher) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		h.server.Shutdown(context.Background()) //nolint:errcheck
	}()
	if err := h.server.Serve(ln); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (h *HTTPPublisher) handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	h.mu.RLock()
	snap := h.snap
	h.mu.RUnlock()
	if snap == nil {
		http.Error(w, "no snapshot yet", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(snap) //nolint:errcheck
}
