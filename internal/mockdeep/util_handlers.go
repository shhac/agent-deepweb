package mockdeep

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) headers(w http.ResponseWriter, r *http.Request) {
	headers := map[string]string{}
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{"headers": headers})
}

func (s *Server) echo(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	headers := map[string]string{}
	for k, v := range r.Header {
		headers[k] = strings.Join(v, ", ")
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"method":  r.Method,
		"path":    r.URL.Path,
		"query":   r.URL.Query(),
		"headers": headers,
		"body":    string(body),
	})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	codeStr := strings.TrimPrefix(r.URL.Path, "/status/")
	code, err := strconv.Atoi(codeStr)
	if err != nil || code < 100 || code > 599 {
		fail(w, http.StatusBadRequest, "bad status code: "+codeStr)
		return
	}
	if code == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "1")
	}
	writeJSON(w, code, map[string]any{"status": code})
}

func (s *Server) slow(w http.ResponseWriter, r *http.Request) {
	ms, _ := strconv.Atoi(r.URL.Query().Get("ms"))
	if ms <= 0 {
		ms = 1000
	}
	time.Sleep(time.Duration(ms) * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]any{"slept_ms": ms})
}

func (s *Server) large(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
	if n <= 0 {
		n = 256
	}
	if n > 50*1024*1024 {
		n = 50 * 1024 * 1024
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", fmt.Sprint(n))
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 4096)
	for i := range buf {
		buf[i] = 'x'
	}
	remaining := n
	for remaining > 0 {
		chunk := len(buf)
		if chunk > remaining {
			chunk = remaining
		}
		_, _ = w.Write(buf[:chunk])
		remaining -= chunk
	}
}

func (s *Server) redirect(w http.ResponseWriter, r *http.Request) {
	to := r.URL.Query().Get("to")
	if to == "" {
		to = "/whoami"
	}
	http.Redirect(w, r, to, http.StatusFound)
}
