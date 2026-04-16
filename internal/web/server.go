package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"api-tester/internal/model"
	"api-tester/internal/storage"
)

type TriggerFunc func(ctx context.Context, mode string) (*model.RunReport, error)

type Server struct {
	store   *storage.Store
	trigger TriggerFunc
	mux     *http.ServeMux
}

func New(store *storage.Store, trigger TriggerFunc) *Server {
	s := &Server{store: store, trigger: trigger, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("/", s.handleIndex)
	s.mux.HandleFunc("/api/runs", s.handleRuns)
	s.mux.HandleFunc("/api/calls", s.handleCalls)
	s.mux.HandleFunc("/api/endpoints", s.handleEndpoints)
	s.mux.HandleFunc("/api/run", s.handleRun)
	s.mux.HandleFunc("/api/healthz", s.handleHealth)
	s.mux.Handle("/metrics", promhttp.Handler())
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	runs, err := s.store.ListRuns(r.Context(), limit)
	respondJSON(w, runs, err)
}

func (s *Server) handleCalls(w http.ResponseWriter, r *http.Request) {
	runID, _ := strconv.ParseInt(r.URL.Query().Get("run_id"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	q := r.URL.Query().Get("q")
	recs, err := s.store.ListCallRecords(r.Context(), runID, q, limit)
	respondJSON(w, recs, err)
}

func (s *Server) handleEndpoints(w http.ResponseWriter, r *http.Request) {
	eps, err := s.store.ListEndpoints(r.Context(), false)
	respondJSON(w, eps, err)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	if s.trigger == nil {
		respondJSON(w, nil, http.ErrNotSupported)
		return
	}
	if r.Method != http.MethodPost && r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	mode := strings.TrimSpace(r.URL.Query().Get("mode"))
	if mode == "" {
		mode = "smoke"
	}
	report, err := s.trigger(r.Context(), mode)
	respondJSON(w, report, err)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	respondJSON(w, map[string]any{"ok": true}, nil)
}

func respondJSON(w http.ResponseWriter, v any, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func RunHTTP(ctx context.Context, addr string, handler http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: handler}
	go func() {
		<-ctx.Done()
		_ = srv.Shutdown(context.Background())
	}()
	err := srv.ListenAndServe()
	if err != nil && !strings.Contains(err.Error(), "Server closed") {
		return err
	}
	return nil
}

var _ = model.Run{}
