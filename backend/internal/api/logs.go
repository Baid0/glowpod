package api

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/logs"
)

// logOptions validates the namespace, container and tail of a log request.
func logOptions(w http.ResponseWriter, r *http.Request) (ns, pod string, opts logs.Options, ok bool) {
	ns, pod = chi.URLParam(r, "namespace"), chi.URLParam(r, "pod")
	q := r.URL.Query()
	opts = logs.Options{Container: q.Get("container"), Previous: q.Get("previous") == "true"}
	if len(validation.IsDNS1123Label(ns)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
		return "", "", opts, false
	}
	if opts.Container != "" && len(validation.IsDNS1123Label(opts.Container)) > 0 {
		writeError(w, http.StatusBadRequest, "bad_container", "That isn't a valid container name.")
		return "", "", opts, false
	}
	if v := q.Get("tailLines"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 1 || n > logs.MaxTail {
			writeError(w, http.StatusBadRequest, "bad_tail", "tailLines must be between 1 and 10000.")
			return "", "", opts, false
		}
		opts.TailLines = n
	}
	return ns, pod, opts, true
}

// streamLogs follows a container's log over a WebSocket.
func (s *server) streamLogs(w http.ResponseWriter, r *http.Request) {
	ns, pod, opts, ok := logOptions(w, r)
	if !ok {
		return
	}
	logs.Serve(w, r, clientsFrom(r).Kube, ns, pod, opts)
}

// downloadLogs sends what the log holds now as a .log file.
func (s *server) downloadLogs(w http.ResponseWriter, r *http.Request) {
	ns, pod, opts, ok := logOptions(w, r)
	if !ok {
		return
	}
	if err := logs.Download(w, r, clientsFrom(r).Kube, ns, pod, opts); err != nil {
		code, msg := logs.Explain(err, opts)
		status := s.classify(err)
		if code == "not_started" || code == "no_previous" || code == "bad_request" {
			status = http.StatusBadRequest
		}
		writeError(w, status, code, msg)
	}
}
