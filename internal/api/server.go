// Package api exposes the scanner over HTTP and serves the web page.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"stockscanner/internal/scanner"
)

// Server handles POST /api/analyze and serves static files.
type Server struct {
	Scanner *scanner.Scanner
	Static  fs.FS
	Timeout time.Duration // per analyze request; default 2 minutes
}

// Handler returns the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/analyze", s.analyze)
	mux.HandleFunc("GET /api/symbols", s.symbols)
	if s.Static != nil {
		mux.Handle("GET /", http.FileServerFS(s.Static))
	}
	return mux
}

type analyzeRequest struct {
	Symbols json.RawMessage `json:"symbols"`
}

func (s *Server) analyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Request body must be JSON like {\"symbols\": \"RELIANCE, TCS\"}.")
		return
	}
	raw, ok := symbolsText(req.Symbols)
	if !ok {
		writeError(w, http.StatusBadRequest, "\"symbols\" must be a string or a list of strings.")
		return
	}

	timeout := s.Timeout
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	res, err := s.Scanner.Analyze(ctx, scanner.ParseEntries(raw))
	var inErr *scanner.InputError
	switch {
	case errors.As(err, &inErr):
		writeError(w, http.StatusBadRequest, inErr.Msg)
	case errors.Is(err, scanner.ErrBenchmark):
		writeError(w, http.StatusBadGateway, err.Error())
	case err != nil:
		log.Printf("analyze: %v", err)
		writeError(w, http.StatusInternalServerError, "Internal error: "+err.Error())
	default:
		writeJSON(w, http.StatusOK, res)
	}
}

type symbolEntry struct {
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
	ISIN   string `json:"isin"`
}

// symbols returns the NSE equity list for the stock picker.
func (s *Server) symbols(w http.ResponseWriter, r *http.Request) {
	if s.Scanner.Symbols == nil {
		writeError(w, http.StatusServiceUnavailable, "NSE symbol list unavailable; type symbols directly.")
		return
	}
	list, err := s.Scanner.Symbols.Get(r.Context())
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "NSE symbol list unavailable; type symbols directly.")
		return
	}
	out := make([]symbolEntry, len(list.Equities))
	for i, e := range list.Equities {
		out[i] = symbolEntry{e.Symbol, e.Name, e.ISIN}
	}
	w.Header().Set("Cache-Control", "max-age=3600")
	writeJSON(w, http.StatusOK, map[string]any{
		"symbols":    out,
		"fetched_at": list.FetchedAt.UTC(),
		"stale":      list.Stale,
	})
}

// symbolsText accepts a string or a list of strings.
func symbolsText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", true
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		return str, true
	}
	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return strings.Join(list, "\n"), true
	}
	return "", false
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writing response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
