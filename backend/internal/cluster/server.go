package cluster

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	authnv1 "k8s.io/api/authentication/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	// clientIdle is how long an identity's clients (and discovery cache)
	// are kept without use.
	clientIdle = 15 * time.Minute
	maxClients = 1000
)

// ErrNoIdentity means a request reached the cluster without a signed-in user.
var ErrNoIdentity = errors.New("no signed-in identity")

// Server serves server mode: every identity gets its own clients. Glowpod's
// own service account is only used to impersonate users; tokens are passed
// through as they are. Nothing is shared between identities.
type Server struct {
	base *rest.Config
	info Info
	now  func() time.Time

	mu      sync.Mutex
	clients map[string]*cachedClients
}

type cachedClients struct {
	clients  *Clients
	lastUsed time.Time
}

// NewServer uses base (normally rest.InClusterConfig) as Glowpod's own
// identity, which needs only the right to impersonate.
func NewServer(base *rest.Config, info Info) *Server {
	return &Server{base: base, info: info, now: time.Now, clients: map[string]*cachedClients{}}
}

// Info implements Provider.
func (s *Server) Info() Info { return s.info }

// ClientsFor implements Provider.
func (s *Server) ClientsFor(_ context.Context, id Identity) (*Clients, error) {
	if id.Key == "" || (id.Token == "" && id.User == "") {
		return nil, ErrNoIdentity
	}
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.clients[id.Key]; ok {
		c.lastUsed = now
		return c.clients, nil
	}
	s.evict(now)
	c, err := newClients(ConfigFor(s.base, id))
	if err != nil {
		return nil, err
	}
	s.clients[id.Key] = &cachedClients{clients: c, lastUsed: now}
	return c, nil
}

// Forget drops an identity's clients, e.g. on sign-out.
func (s *Server) Forget(id Identity) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, id.Key)
}

func (s *Server) evict(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for k, c := range s.clients {
		if now.Sub(c.lastUsed) > clientIdle {
			delete(s.clients, k)
			continue
		}
		if oldestKey == "" || c.lastUsed.Before(oldest) {
			oldestKey, oldest = k, c.lastUsed
		}
	}
	if len(s.clients) >= maxClients && oldestKey != "" {
		delete(s.clients, oldestKey)
	}
}

// ConfigFor returns the REST config that acts as id: the user's own token
// with none of Glowpod's credentials, or Glowpod's credentials
// impersonating the user and their groups.
func ConfigFor(base *rest.Config, id Identity) *rest.Config {
	var cfg *rest.Config
	if id.Token != "" {
		cfg = rest.AnonymousClientConfig(base)
		cfg.BearerToken = id.Token
	} else {
		cfg = rest.CopyConfig(base)
		cfg.Impersonate = rest.ImpersonationConfig{UserName: id.User, Groups: id.Groups}
	}
	cfg.UserAgent = "glowpod"
	cfg.QPS = 50
	cfg.Burst = 100
	return cfg
}

func newClients(cfg *rest.Config) (*Clients, error) {
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}
	dyn, err := dynamic.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}
	return &Clients{
		Kube:      kube,
		Dynamic:   dyn,
		Discovery: memory.NewMemCacheClient(kube.Discovery()),
		Config:    cfg,
	}, nil
}

// InClusterInfo describes the cluster Glowpod runs in.
func InClusterInfo(cfg *rest.Config, name string) Info {
	if name == "" {
		name = "in-cluster"
	}
	return Info{ID: DefaultID, Name: name, Server: serverHost(cfg.Host)}
}

// WhoAmI asks the cluster who id is (SelfSubjectReview), as id.
func (s *Server) WhoAmI(ctx context.Context, id Identity) (string, []string, error) {
	c, err := s.ClientsFor(ctx, id)
	if err != nil {
		return "", nil, err
	}
	review, err := c.Kube.AuthenticationV1().SelfSubjectReviews().Create(ctx, &authnv1.SelfSubjectReview{}, metav1.CreateOptions{})
	if err != nil {
		return "", nil, fmt.Errorf("ask who this is: %w", err)
	}
	return review.Status.UserInfo.Username, review.Status.UserInfo.Groups, nil
}
