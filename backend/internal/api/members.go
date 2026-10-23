package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"glowpod/internal/auth"
	"glowpod/internal/members"
)

// Cluster members and projects: RBAC in the cluster, made as the person
// asking, so Kubernetes decides whether they may grant it.

type memberRequest struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
	Role string `json:"role"`
	// ClusterRole is the role for a custom cluster member.
	ClusterRole string `json:"clusterRole,omitempty"`
}

type projectRequest struct {
	Name       string   `json:"name,omitempty"`
	Namespaces []string `json:"namespaces"`
}

func (s *server) membersError(w http.ResponseWriter, err error) {
	if errors.Is(err, members.ErrInvalid) {
		msg := strings.TrimPrefix(err.Error(), members.ErrInvalid.Error()+": ")
		writeError(w, http.StatusBadRequest, "invalid", strings.ToUpper(msg[:1])+msg[1:]+".")
		return
	}
	s.clusterError(w, err)
}

func (s *server) auditMembers(r *http.Request, action string, args ...any) {
	id, _ := auth.IdentityFrom(r.Context())
	s.opts.Logger.Info("changed access", append([]any{"action", action, "identity", id.Key, "cluster", clusterFrom(r).ID}, args...)...)
}

func subjectFromQuery(r *http.Request) members.Subject {
	return members.Subject{Kind: r.URL.Query().Get("kind"), Name: r.URL.Query().Get("name")}
}

func (s *server) listMembers(w http.ResponseWriter, r *http.Request) {
	m, err := members.ClusterMembers(r.Context(), clientsFrom(r).Kube)
	if err != nil {
		s.membersError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

func (s *server) setMember(w http.ResponseWriter, r *http.Request) {
	var req memberRequest
	if !readJSON(w, r, &req) {
		return
	}
	subject := members.Subject{Kind: req.Kind, Name: req.Name}
	if err := members.SetClusterMember(r.Context(), clientsFrom(r).Kube, subject, req.Role, req.ClusterRole); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "set cluster member", "subject", req.Name, "role", req.Role, "cluster_role", req.ClusterRole)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) removeMember(w http.ResponseWriter, r *http.Request) {
	subject := subjectFromQuery(r)
	if err := members.RemoveClusterMember(r.Context(), clientsFrom(r).Kube, subject); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "remove cluster member", "subject", subject.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listProjects(w http.ResponseWriter, r *http.Request) {
	p, err := members.ListProjects(r.Context(), clientsFrom(r).Kube)
	if err != nil {
		s.membersError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *server) createProject(w http.ResponseWriter, r *http.Request) {
	var req projectRequest
	if !readJSON(w, r, &req) {
		return
	}
	if err := members.CreateProject(r.Context(), clientsFrom(r).Kube, req.Name, req.Namespaces); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "create project", "project", req.Name, "namespaces", req.Namespaces)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) setProjectNamespaces(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "project")
	var req projectRequest
	if !readJSON(w, r, &req) {
		return
	}
	if len(req.Namespaces) == 0 {
		writeError(w, http.StatusBadRequest, "invalid", "Keep at least one namespace, or delete the project.")
		return
	}
	if err := members.SetProjectNamespaces(r.Context(), clientsFrom(r).Kube, name, req.Namespaces); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "set project namespaces", "project", name, "namespaces", req.Namespaces)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) deleteProject(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "project")
	if err := members.DeleteProject(r.Context(), clientsFrom(r).Kube, name); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "delete project", "project", name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) setProjectMember(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "project")
	var req memberRequest
	if !readJSON(w, r, &req) {
		return
	}
	subject := members.Subject{Kind: req.Kind, Name: req.Name}
	if err := members.SetProjectMember(r.Context(), clientsFrom(r).Kube, name, subject, req.Role); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "set project member", "project", name, "subject", req.Name, "role", req.Role)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) removeProjectMember(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "project")
	subject := subjectFromQuery(r)
	if err := members.RemoveProjectMember(r.Context(), clientsFrom(r).Kube, name, subject); err != nil {
		s.membersError(w, err)
		return
	}
	s.auditMembers(r, "remove project member", "project", name, "subject", subject.Name)
	w.WriteHeader(http.StatusNoContent)
}
