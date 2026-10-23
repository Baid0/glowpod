// Package hub is Glowpod Hub: one Glowpod for many clusters, the way Rancher
// works. People sign in to the Hub; clusters are imported with kubectl apply
// and run glowpod-agent, which connects out to the Hub. Every call to a
// cluster runs as the signed-in person (impersonated by the agent, which may
// do nothing else), and who sees which cluster comes from each cluster's
// own role bindings.
package hub

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"glowpod/internal/auth"
	"glowpod/internal/cluster"
	"glowpod/internal/subjects"
	"glowpod/internal/tunnel"
)

// LocalID is the Hub's own cluster, as in Rancher.
const LocalID = "local"

// UserPrefix marks the Hub's local users in Kubernetes: glowpod:admin.
const UserPrefix = "glowpod:"

// AdminUser is the first local user, created at first start.
const AdminUser = "admin"

const (
	importTTL    = time.Hour
	pollInterval = 30 * time.Second
	pollTimeout  = 15 * time.Second
	// bootstrapLength is random bytes in the first-start password.
	bootstrapLength = 18
)

// Roles in the Hub, like Rancher's global roles. Neither gives any rights
// inside a cluster.
const (
	RoleAdmin = "admin"
	RoleUser  = "user"
)

// Config sets up a Hub.
type Config struct {
	// Namespace is where the Hub keeps its Secrets and settings.
	Namespace string
	// Kube and Dynamic act as the Hub's own service account: its store,
	// and the role bindings of the local cluster.
	Kube    kubernetes.Interface
	Dynamic dynamic.Interface
	// Registry gets the local cluster and every imported one.
	Registry *cluster.Registry
	// PublicURL is the default server URL for agents (-public-url).
	PublicURL string
	// AgentImage runs in imported clusters.
	AgentImage string
	// HubCA, when set, is the PEM CA agents trust for the Hub (a private CA).
	HubCA string
	// Admins are usernames or groups (as Kubernetes sees them, with their
	// prefix) that are Hub administrators, besides the local admin.
	Admins []string
	Logger *slog.Logger
}

// Hub runs the Hub's side: its store, the tunnels and the routes.
type Hub struct {
	cfg      Config
	store    *store
	sessions *tunnel.Sessions
	now      func() time.Time

	mu            sync.Mutex
	records       map[string]*Record
	localSubjects subjects.Set
}

// New returns a Hub. Call Start before serving.
func New(cfg Config) *Hub {
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	h := &Hub{
		cfg:      cfg,
		store:    &store{kube: cfg.Kube, dyn: cfg.Dynamic, namespace: cfg.Namespace},
		sessions: tunnel.NewSessions(),
		now:      time.Now,
		records:  map[string]*Record{},
	}
	h.sessions.OnChange = h.onTunnel
	return h
}

// Start loads the imported clusters, prepares first start, and keeps each
// cluster's status fresh until ctx ends.
func (h *Hub) Start(ctx context.Context) error {
	if err := h.bootstrap(ctx); err != nil {
		return err
	}
	records, err := h.store.clusters(ctx)
	if err != nil {
		return err
	}
	for i := range records {
		h.add(records[i])
	}
	h.poll(ctx)
	go func() {
		t := time.NewTicker(pollInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.poll(ctx)
			}
		}
	}()
	return nil
}

// bootstrap makes the first-start password, once, until setup is done. It
// never goes to the log; the log says how to read it.
func (h *Hub) bootstrap(ctx context.Context) error {
	set, err := h.store.settings(ctx)
	if err != nil {
		return err
	}
	if set.SetupDone {
		return nil
	}
	existing, err := h.store.secret(ctx, BootstrapSecret)
	if err != nil {
		return err
	}
	if len(existing[keyPassword]) == 0 {
		pw, err := randomString(bootstrapLength)
		if err != nil {
			return err
		}
		if err := h.store.putSecret(ctx, BootstrapSecret, "bootstrap", map[string][]byte{keyPassword: []byte(pw)}); err != nil {
			return err
		}
	}
	h.cfg.Logger.Info("first start: sign in as admin with the bootstrap password, then set your own",
		"read_it_with", fmt.Sprintf("kubectl -n %s get secret %s -o jsonpath='{.data.password}' | base64 -d", h.cfg.Namespace, BootstrapSecret))
	return nil
}

// Agent clusters.

// add registers an imported cluster; its clients dial through its tunnel.
func (h *Hub) add(r Record) {
	h.mu.Lock()
	h.records[r.ID] = &r
	h.mu.Unlock()
	base := &rest.Config{
		// The agent answers plain HTTP inside the tunnel, which is TLS.
		Host:      "http://" + r.ID + ".glowpod-agent",
		Dial:      h.sessions.DialFunc(r.ID),
		UserAgent: "glowpod-hub",
	}
	h.cfg.Registry.Add(&agentCluster{Server: cluster.NewServer(base, cluster.Info{ID: r.ID, Name: r.Name}), hub: h, id: r.ID})
}

// agentCluster is an imported cluster: the clients of cluster.Server, with
// a live state from its tunnel.
type agentCluster struct {
	*cluster.Server
	hub *Hub
	id  string
}

func (a *agentCluster) Info() cluster.Info { return a.hub.info(a.id) }

func (h *Hub) info(id string) cluster.Info {
	r, ok := h.record(id)
	if !ok {
		return cluster.Info{ID: id, Name: id}
	}
	info := cluster.Info{ID: r.ID, Name: r.Name, Server: "via glowpod-agent"}
	if ok, since := h.sessions.Connected(id); ok {
		info.State, info.Since = "connected", &since
	} else if r.LastSeen != nil {
		info.State, info.Since = "disconnected", r.LastSeen
	} else {
		info.State, info.Since = "pending", &r.Created
	}
	return info
}

func (h *Hub) record(id string) (Record, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.records[id]
	if !ok {
		return Record{}, false
	}
	return *r, true
}

// onTunnel hears an agent come and go.
func (h *Hub) onTunnel(id string, connected bool) {
	h.cfg.Logger.Info("agent tunnel", "cluster", id, "connected", connected)
	ctx, cancel := context.WithTimeout(context.Background(), pollTimeout)
	go func() {
		defer cancel()
		if connected {
			h.pollCluster(ctx, id)
			return
		}
		now := h.now()
		h.mu.Lock()
		r := h.records[id]
		if r != nil {
			r.LastSeen = &now
		}
		h.mu.Unlock()
		if r != nil {
			h.saveStatus(ctx, id)
		}
	}()
}

// poll refreshes who has access to each cluster.
func (h *Hub) poll(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	if set, err := subjects.List(ctx, h.cfg.Kube); err != nil {
		h.cfg.Logger.Warn("couldn't read the local cluster's role bindings", "err", err)
	} else {
		h.mu.Lock()
		h.localSubjects = set
		h.mu.Unlock()
	}
	h.mu.Lock()
	ids := make([]string, 0, len(h.records))
	for id := range h.records {
		ids = append(ids, id)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		if ok, _ := h.sessions.Connected(id); ok {
			wg.Go(func() { h.pollCluster(ctx, id) })
		}
	}
	wg.Wait()
}

func (h *Hub) pollCluster(ctx context.Context, id string) {
	st, err := h.agentStatus(ctx, id)
	if err != nil {
		h.cfg.Logger.Warn("couldn't read an agent's status", "cluster", id, "err", err)
		return
	}
	now := h.now()
	h.mu.Lock()
	r := h.records[id]
	changed := r != nil && (r.KubernetesVersion != st.KubernetesVersion ||
		!slices.Equal(r.Subjects.Users, st.Subjects.Users) || !slices.Equal(r.Subjects.Groups, st.Subjects.Groups))
	if r != nil {
		r.KubernetesVersion, r.Subjects, r.LastSeen = st.KubernetesVersion, st.Subjects, &now
	}
	h.mu.Unlock()
	if changed {
		h.saveStatus(ctx, id)
	}
}

func (h *Hub) saveStatus(ctx context.Context, id string) {
	r, ok := h.record(id)
	if !ok {
		return
	}
	if err := h.store.saveStatus(ctx, r); err != nil {
		h.cfg.Logger.Warn("couldn't save a cluster's status", "cluster", id, "err", err)
	}
}

// Who sees what.

// Role is someone's role in the Hub.
func (h *Hub) Role(info auth.SessionInfo) string {
	if info.User == UserPrefix+AdminUser {
		return RoleAdmin
	}
	if slices.Contains(h.cfg.Admins, info.User) {
		return RoleAdmin
	}
	for _, g := range info.Groups {
		if slices.Contains(h.cfg.Admins, g) {
			return RoleAdmin
		}
	}
	return RoleUser
}

// Visible says whether to list a cluster for someone: Hub administrators
// see every cluster; everyone else sees the clusters where they or one of
// their groups is bound to anything.
func (h *Hub) Visible(info auth.SessionInfo, clusterID string) bool {
	if h.Role(info) == RoleAdmin {
		return true
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if clusterID == LocalID {
		return h.localSubjects.Includes(info.User, info.Groups)
	}
	r := h.records[clusterID]
	return r != nil && r.Subjects.Includes(info.User, info.Groups)
}

// SessionInfo is the Hub's part of GET /api/v1/session.
type SessionInfo struct {
	Role string `json:"role"`
	// SetupRequired: the admin signed in with the bootstrap password and
	// must set their own and the server URL.
	SetupRequired bool   `json:"setupRequired,omitempty"`
	ServerURL     string `json:"serverURL,omitempty"`
}

// Session describes the Hub to someone signed in.
func (h *Hub) Session(ctx context.Context, info auth.SessionInfo) any {
	out := SessionInfo{Role: h.Role(info)}
	if set, err := h.store.settings(ctx); err == nil {
		out.SetupRequired = !set.SetupDone
		if out.Role == RoleAdmin {
			out.ServerURL = cmp(set.ServerURL, h.cfg.PublicURL)
		}
	}
	return out
}

func cmp(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Local users.

var usernamePattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9._-]{0,62}[a-z0-9])?$`)

// CheckPassword signs in a local user. Before setup, admin's password is
// the bootstrap password. It returns the name Kubernetes sees.
func (h *Hub) CheckPassword(ctx context.Context, username, password string) (string, error) {
	username = strings.ToLower(strings.TrimSpace(username))
	if !usernamePattern.MatchString(username) || password == "" {
		return "", auth.ErrBadCredentials
	}
	data, err := h.store.secret(ctx, userSecretPrefix+username)
	if err != nil {
		return "", err
	}
	if data != nil {
		ok, err := checkPassword(string(data[keyPasswordHash]), password)
		if err != nil || !ok {
			return "", auth.ErrBadCredentials
		}
		return UserPrefix + username, nil
	}
	if username != AdminUser {
		return "", auth.ErrBadCredentials
	}
	set, err := h.store.settings(ctx)
	if err != nil {
		return "", err
	}
	if set.SetupDone {
		return "", auth.ErrBadCredentials
	}
	boot, err := h.store.secret(ctx, BootstrapSecret)
	if err != nil {
		return "", err
	}
	if !sameBytes(boot[keyPassword], password) {
		return "", auth.ErrBadCredentials
	}
	return UserPrefix + AdminUser, nil
}

// errSetupDone means first start is already over.
var errSetupDone = errors.New("setup is already done")

// setup sets admin's password and the server URL, and ends first start:
// the bootstrap password stops working.
func (h *Hub) setup(ctx context.Context, password, serverURL string) error {
	set, err := h.store.settings(ctx)
	if err != nil {
		return err
	}
	if set.SetupDone {
		return errSetupDone
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := h.store.putSecret(ctx, userSecretPrefix+AdminUser, "user",
		map[string][]byte{keyPasswordHash: []byte(hash), keyRole: []byte(RoleAdmin)}); err != nil {
		return err
	}
	if err := h.store.saveSettings(ctx, settings{ServerURL: serverURL, SetupDone: true}); err != nil {
		return err
	}
	return h.store.deleteSecret(ctx, BootstrapSecret)
}

func (h *Hub) serverURL(ctx context.Context) (string, error) {
	set, err := h.store.settings(ctx)
	if err != nil {
		return "", err
	}
	if u := cmp(set.ServerURL, h.cfg.PublicURL); u != "" {
		return strings.TrimSuffix(u, "/"), nil
	}
	return "", errors.New("the Hub's server URL isn't set")
}

// agentStatus asks an agent about its cluster, through its tunnel.
func (h *Hub) agentStatus(ctx context.Context, id string) (statusResponse, error) {
	tr := &http.Transport{DialContext: h.sessions.DialFunc(id), DisableKeepAlives: true}
	return getStatus(ctx, &http.Client{Transport: tr}, id)
}
