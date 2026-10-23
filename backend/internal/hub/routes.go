package hub

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"glowpod/internal/agent"
	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/tunnel"
)

// MountPublic adds the routes agents use. They carry their own secret (the
// import token or the agent's credential), not a browser session.
func (h *Hub) MountPublic(r chi.Router) {
	r.Get(tunnel.ImportPrefix+"{file}", h.importManifest)
	r.Post(tunnel.RegisterPath, h.register)
	r.Get(tunnel.ConnectPath, h.connect)
	// kubectl, with a kubeconfig token instead of a cookie.
	r.HandleFunc(KubeAPIPrefix+"{cluster}", h.kubeAPI)
	r.HandleFunc(KubeAPIPrefix+"{cluster}/*", h.kubeAPI)
}

// MountAPI adds /api/v1/hub/…, for people signed in (with CSRF checked).
func (h *Hub) MountAPI(r chi.Router) {
	r.Post("/setup", h.handleSetup)
	r.Post("/kubeconfig", h.downloadKubeconfig)
	r.Get("/kubeconfig/tokens", h.listKubeTokens)
	r.Delete("/kubeconfig/tokens/{id}", h.revokeKubeToken)
	r.Get("/shell/{cluster}", h.openShell)
	r.Group(func(r chi.Router) {
		r.Use(h.requireAdmin)
		r.Post("/clusters", h.importCluster)
		r.Post("/clusters/{cluster}/import", h.reimportCluster)
		r.Delete("/clusters/{cluster}", h.removeCluster)
	})
}

func (h *Hub) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.Role(auth.SessionInfoFrom(r.Context())) != RoleAdmin {
			writeError(w, http.StatusForbidden, "forbidden", "Only Hub administrators can do this.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// First start.

type setupRequest struct {
	Password  string `json:"password"`
	ServerURL string `json:"serverURL"`
}

func (h *Hub) handleSetup(w http.ResponseWriter, r *http.Request) {
	info := auth.SessionInfoFrom(r.Context())
	if info.User != UserPrefix+AdminUser {
		writeError(w, http.StatusForbidden, "forbidden", "Sign in as admin to finish setting up the Hub.")
		return
	}
	var req setupRequest
	if !decode(w, r, &req) {
		return
	}
	if len([]rune(req.Password)) < MinPasswordLength {
		writeError(w, http.StatusBadRequest, "weak_password", fmt.Sprintf("Use at least %d characters.", MinPasswordLength))
		return
	}
	serverURL, ok := checkServerURL(req.ServerURL)
	if !ok {
		writeError(w, http.StatusBadRequest, "bad_server_url", "Use the Hub's address, such as https://glowpod.example.com, without a path.")
		return
	}
	err := h.setup(r.Context(), req.Password, serverURL)
	if errors.Is(err, errSetupDone) {
		writeError(w, http.StatusConflict, "setup_done", "The Hub is already set up.")
		return
	}
	if err != nil {
		h.cfg.Logger.Error("setup failed", "err", err)
		writeError(w, http.StatusInternalServerError, "setup_failed", "Couldn't save the settings. Check the Hub's logs.")
		return
	}
	h.cfg.Logger.Info("first start finished", "server_url", serverURL)
	w.WriteHeader(http.StatusNoContent)
}

// checkServerURL wants the address agents and people use: http(s), no path.
func checkServerURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" {
		return "", false
	}
	return u.Scheme + "://" + u.Host, true
}

// Importing clusters.

type importRequest struct {
	Name string `json:"name"`
	// Owner makes the person importing cluster-admin there.
	Owner bool `json:"owner"`
}

// ImportResponse is how to install the agent.
type ImportResponse struct {
	Cluster cluster.Info `json:"cluster"`
	// Command installs the agent; InsecureCommand does the same when the
	// Hub's certificate isn't trusted by the machine running kubectl. A
	// plain-HTTP Hub has no certificate, so no InsecureCommand.
	Command         string    `json:"command"`
	InsecureCommand string    `json:"insecureCommand,omitempty"`
	ExpiresAt       time.Time `json:"expiresAt"`
}

const maxNameLength = 63

func (h *Hub) importCluster(w http.ResponseWriter, r *http.Request) {
	var req importRequest
	if !decode(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || len(name) > maxNameLength {
		writeError(w, http.StatusBadRequest, "bad_name", "Give the cluster a name of up to 63 characters.")
		return
	}
	id := h.newID(name)
	rec := Record{ID: id, Name: name, ImportedBy: auth.SessionInfoFrom(r.Context()).User, Created: h.now()}
	if err := h.store.createCluster(r.Context(), rec); err != nil {
		h.cfg.Logger.Error("import failed", "err", err)
		writeError(w, http.StatusInternalServerError, "import_failed", "Couldn't save the cluster. Check the Hub's logs.")
		return
	}
	h.add(rec)
	h.respondImport(w, r, id, req.Owner)
}

func (h *Hub) reimportCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cluster")
	if _, ok := h.record(id); !ok {
		writeError(w, http.StatusNotFound, "unknown_cluster", "The Hub has no such cluster.")
		return
	}
	var req importRequest
	if !decode(w, r, &req) {
		return
	}
	h.respondImport(w, r, id, req.Owner)
}

// respondImport makes a new one-time token and answers with the command.
func (h *Hub) respondImport(w http.ResponseWriter, r *http.Request, id string, owner bool) {
	ctx := r.Context()
	serverURL, err := h.serverURL(ctx)
	if err != nil {
		writeError(w, http.StatusConflict, "no_server_url", "Set the Hub's server URL first.")
		return
	}
	token, hash, err := newSecret(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "import_failed", "Couldn't make an import token.")
		return
	}
	expires := h.now().Add(importTTL).UTC()
	data, err := h.store.secret(ctx, clusterSecretPrefix+id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "import_failed", "Couldn't save the import token.")
		return
	}
	if data == nil {
		data = map[string][]byte{}
	}
	data[keyTokenHash] = []byte(hash)
	data[keyTokenExpires] = []byte(expires.Format(time.RFC3339))
	delete(data, keyOwner)
	if owner {
		data[keyOwner] = []byte(auth.SessionInfoFrom(ctx).User)
	}
	if err := h.store.putSecret(ctx, clusterSecretPrefix+id, "cluster", data); err != nil {
		writeError(w, http.StatusInternalServerError, "import_failed", "Couldn't save the import token.")
		return
	}
	manifest := serverURL + tunnel.ImportPrefix + token + ".yaml"
	resp := ImportResponse{Cluster: h.info(id), Command: "kubectl apply -f " + manifest, ExpiresAt: expires}
	if strings.HasPrefix(serverURL, "https://") {
		resp.InsecureCommand = "curl --insecure -sfL " + manifest + " | kubectl apply -f -"
	}
	writeJSON(w, http.StatusOK, resp)
}

// newID is a URL-safe id for a new cluster that no other cluster has.
func (h *Hub) newID(name string) string {
	base := cluster.ContextIDs([]string{name})[name]
	h.mu.Lock()
	defer h.mu.Unlock()
	id := base
	for n := 2; id == LocalID || h.records[id] != nil; n++ {
		id = base + "-" + strconv.Itoa(n)
	}
	return id
}

// RemoveResponse says how to clean up the cluster itself.
type RemoveResponse struct {
	Cleanup string `json:"cleanup"`
}

func (h *Hub) removeCluster(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "cluster")
	if _, ok := h.record(id); !ok {
		writeError(w, http.StatusNotFound, "unknown_cluster", "The Hub has no such cluster.")
		return
	}
	// The credential goes first, so the agent can't come back.
	if err := h.store.deleteCluster(r.Context(), id); err != nil {
		h.cfg.Logger.Error("remove failed", "cluster", id, "err", err)
		writeError(w, http.StatusInternalServerError, "remove_failed", "Couldn't remove the cluster. Check the Hub's logs.")
		return
	}
	h.cfg.Registry.Remove(id)
	h.mu.Lock()
	delete(h.records, id)
	h.mu.Unlock()
	h.sessions.Close(id)
	h.forgetUpstream(id)
	h.cfg.Logger.Info("removed a cluster", "cluster", id, "by", auth.SessionInfoFrom(r.Context()).User)
	writeJSON(w, http.StatusOK, RemoveResponse{Cleanup: cleanupCommand()})
}

// The agent's routes.

// tokenFor finds the cluster a valid, unexpired import token is for.
func (h *Hub) tokenFor(ctx context.Context, token string) (string, map[string][]byte, bool) {
	id, ok := splitSecret(token)
	if !ok {
		return "", nil, false
	}
	if _, ok := h.record(id); !ok {
		return "", nil, false
	}
	data, err := h.store.secret(ctx, clusterSecretPrefix+id)
	if err != nil || data == nil || !sameHash(data[keyTokenHash], token) {
		return "", nil, false
	}
	expires, err := time.Parse(time.RFC3339, string(data[keyTokenExpires]))
	if err != nil || h.now().After(expires) {
		return "", nil, false
	}
	return id, data, true
}

func (h *Hub) importManifest(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutSuffix(chi.URLParam(r, "file"), ".yaml")
	if !ok {
		http.NotFound(w, r)
		return
	}
	id, data, ok := h.tokenFor(r.Context(), token)
	if !ok {
		http.Error(w, "This import link is wrong, used or expired. Make a new one in the Hub: Clusters, Import.", http.StatusNotFound)
		return
	}
	serverURL, err := h.serverURL(r.Context())
	if err != nil {
		http.Error(w, "The Hub's server URL isn't set.", http.StatusConflict)
		return
	}
	body, err := renderManifest(manifestData{
		ClusterID: id,
		HubURL:    serverURL,
		HubCA:     h.cfg.HubCA,
		Token:     token,
		Image:     h.cfg.AgentImage,
		Owner:     string(data[keyOwner]),
	})
	if err != nil {
		http.Error(w, "Couldn't make the manifest.", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(body)
}

func (h *Hub) register(w http.ResponseWriter, r *http.Request) {
	var req agent.RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	id, data, ok := h.tokenFor(r.Context(), req.Token)
	if !ok {
		writeError(w, http.StatusUnauthorized, "bad_token", "This import token is wrong, used or expired.")
		return
	}
	credential, hash, err := newSecret(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "register_failed", "Couldn't make a credential.")
		return
	}
	data[keyCredentialHash] = []byte(hash)
	delete(data, keyTokenHash)
	delete(data, keyTokenExpires)
	if err := h.store.putSecret(r.Context(), clusterSecretPrefix+id, "cluster", data); err != nil {
		writeError(w, http.StatusInternalServerError, "register_failed", "Couldn't save the credential.")
		return
	}
	h.cfg.Logger.Info("agent registered", "cluster", id)
	writeJSON(w, http.StatusOK, agent.RegisterResponse{Cluster: id, Credential: credential})
}

func (h *Hub) connect(w http.ResponseWriter, r *http.Request) {
	credential, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	id, okID := splitSecret(credential)
	if !ok || !okID {
		writeError(w, http.StatusUnauthorized, "bad_credential", "This agent's credential isn't valid.")
		return
	}
	if _, known := h.record(id); !known {
		writeError(w, http.StatusUnauthorized, "bad_credential", "This agent's credential isn't valid.")
		return
	}
	data, err := h.store.secret(r.Context(), clusterSecretPrefix+id)
	if err != nil || data == nil || !sameHash(data[keyCredentialHash], credential) {
		writeError(w, http.StatusUnauthorized, "bad_credential", "This agent's credential isn't valid.")
		return
	}
	if err := h.sessions.Accept(w, r, id); err != nil {
		h.cfg.Logger.Warn("agent tunnel failed", "cluster", id, "err", err)
	}
}

// statusResponse is agent.Status.
type statusResponse = agent.Status

func getStatus(ctx context.Context, client *http.Client, id string) (statusResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+id+".glowpod-agent"+tunnel.StatusPath, nil)
	if err != nil {
		return statusResponse{}, err //nolint:wrapcheck // a fixed URL
	}
	resp, err := client.Do(req)
	if err != nil {
		return statusResponse{}, fmt.Errorf("ask the agent: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // read-only
	if resp.StatusCode != http.StatusOK {
		return statusResponse{}, fmt.Errorf("ask the agent: %s", resp.Status)
	}
	var st statusResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&st); err != nil {
		return statusResponse{}, fmt.Errorf("read the agent's status: %w", err)
	}
	return st, nil
}

// Helpers.

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "bad_body", "Send JSON.")
		return false
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_body", "The request isn't valid.")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

func sameBytes(stored []byte, presented string) bool {
	return len(stored) > 0 && subtle.ConstantTimeCompare(stored, []byte(presented)) == 1
}
