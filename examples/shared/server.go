// Package shared provides common utilities for all examples.
package shared

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

// Server wraps http.Server with graceful shutdown and common middleware.
type Server struct {
	*http.Server
	healthy atomic.Bool
	ready   atomic.Bool
	logger  *slog.Logger
}

// NewServer creates a new server with the given handler.
func NewServer(addr string, handler http.Handler, logger *slog.Logger) *Server {
	s := &Server{
		Server: &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  30 * time.Second,
			WriteTimeout: 60 * time.Second,
			IdleTimeout:  120 * time.Second,
		},
		logger: logger,
	}
	s.healthy.Store(true)
	return s
}

// SetReady marks the server as ready to accept traffic.
func (s *Server) SetReady(ready bool) {
	s.ready.Store(ready)
}

// IsHealthy returns whether the server is healthy.
func (s *Server) IsHealthy() bool {
	return s.healthy.Load()
}

// IsReady returns whether the server is ready.
func (s *Server) IsReady() bool {
	return s.ready.Load()
}

// ListenAndServeWithGracefulShutdown starts the server and handles graceful shutdown.
func (s *Server) ListenAndServeWithGracefulShutdown() error {
	done := make(chan error, 1)

	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		s.logger.Info("shutting down server")
		s.healthy.Store(false)

		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		done <- s.Shutdown(ctx)
	}()

	s.logger.Info("starting server", "addr", s.Addr)
	if err := s.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}

	return <-done
}

// HealthHandler returns a health check handler.
func (s *Server) HealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.IsHealthy() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}
}

// ReadyHandler returns a readiness check handler.
func (s *Server) ReadyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.IsReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ready"))
	}
}

// JSONResponse writes a JSON response.
func JSONResponse(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// ErrorResponse writes a JSON error response.
func ErrorResponse(w http.ResponseWriter, status int, message string) {
	JSONResponse(w, status, map[string]string{"error": message})
}

// DecodeJSON decodes a JSON request body.
func DecodeJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
