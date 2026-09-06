package app

import (
	"bufio"
	"errors"
	"log"
	"net"
	"net/http"
	"time"
)

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(payload []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(payload)
}

func (w *statusResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("response writer does not support hijacking")
	}
	return hijacker.Hijack()
}

func (w *statusResponseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusResponseWriter) Push(target string, options *http.PushOptions) error {
	if pusher, ok := w.ResponseWriter.(http.Pusher); ok {
		return pusher.Push(target, options)
	}
	return http.ErrNotSupported
}

func requestLogMiddleware(next http.Handler, observers ...func(string, int, time.Duration)) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		tracked := &statusResponseWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					for _, observe := range observers {
						if observe != nil {
							observe(r.Method, http.StatusBadGateway, time.Since(started))
						}
					}
				}
				panic(recovered)
			}
		}()
		next.ServeHTTP(tracked, r)
		status := tracked.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(started)
		log.Printf(
			"request completed request_id=%s method=%s path=%s status=%d duration_ms=%d",
			requestIDFromContext(r.Context()),
			r.Method,
			r.URL.Path,
			status,
			duration.Milliseconds(),
		)
		for _, observe := range observers {
			if observe != nil {
				observe(r.Method, status, duration)
			}
		}
	})
}
