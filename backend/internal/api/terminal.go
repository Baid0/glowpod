package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"k8s.io/apimachinery/pkg/util/validation"

	"glowpod/internal/auth"
	"glowpod/internal/terminal"
)

// openTerminal runs a shell in a container over a WebSocket. The cluster
// checks the user's right to pods/exec when the shell starts.
func (s *server) openTerminal(w http.ResponseWriter, r *http.Request) {
	ns, pod := chi.URLParam(r, "namespace"), chi.URLParam(r, "pod")
	container := r.URL.Query().Get("container")
	switch {
	case len(validation.IsDNS1123Label(ns)) > 0:
		writeError(w, http.StatusBadRequest, "bad_namespace", "That isn't a valid namespace name.")
		return
	case len(validation.IsDNS1123Subdomain(pod)) > 0:
		writeError(w, http.StatusBadRequest, "bad_pod", "That isn't a valid pod name.")
		return
	case len(validation.IsDNS1123Label(container)) > 0:
		writeError(w, http.StatusBadRequest, "bad_container", "Say which container to open a shell in.")
		return
	}
	id, _ := auth.IdentityFrom(r.Context())
	s.opts.Logger.Info("opened a shell", "identity", id.Key, "namespace", ns, "pod", pod, "container", container)
	terminal.Serve(w, r, s.opts.Terminal(clientsFrom(r)), terminal.Request{Namespace: ns, Pod: pod, Container: container},
		auth.CSRFTokenFrom(r.Context()))
}
