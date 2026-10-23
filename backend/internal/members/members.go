// Package members manages who has access to a cluster, the way Rancher's
// cluster members and projects do, but stored only as Kubernetes RBAC in
// the cluster: ClusterRoleBindings for cluster members, and RoleBindings in
// each of a project's namespaces for project members. A project is a label
// on namespaces. Every call runs as the person making the change, so
// Kubernetes' own escalation rules decide what they may grant.
package members

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

const (
	// ManagedBy marks bindings Glowpod made, which it may change.
	ManagedBy      = "glowpod"
	managedByLabel = "app.kubernetes.io/managed-by"
	// RoleLabel records the role a binding gives, in Glowpod's words.
	RoleLabel = "hub.glowpod.dev/member-role"
	// ProjectLabel puts a namespace, and a project binding, in a project.
	ProjectLabel = "hub.glowpod.dev/project"
	// Annotations keep the subject readable; the binding's name is a hash.
	kindAnnotation = "hub.glowpod.dev/subject-kind"
	nameAnnotation = "hub.glowpod.dev/subject-name"

	clusterPrefix = "glowpod-member-"
	projectPrefix = "glowpod-project-"
)

// Cluster roles.
const (
	RoleOwner    = "owner"
	RoleMember   = "member"
	RoleReadOnly = "read-only"
	RoleCustom   = "custom"
)

// clusterRoles maps a cluster role to its ClusterRole.
var clusterRoles = map[string]string{RoleOwner: "cluster-admin", RoleReadOnly: "view"}

// projectRoles maps a project role to the ClusterRole bound in each of the
// project's namespaces.
var projectRoles = map[string]string{RoleOwner: "admin", RoleMember: "edit", RoleReadOnly: "view"}

// ErrInvalid is a request that can't be right.
var ErrInvalid = errors.New("invalid")

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Subject is a person or group as Kubernetes sees them, prefix included
// (oidc:ann@example.com, ldap:ops, glowpod:admin).
type Subject struct {
	Kind string `json:"kind"` // User or Group
	Name string `json:"name"`
}

func (s Subject) check() error {
	if s.Kind != rbacv1.UserKind && s.Kind != rbacv1.GroupKind {
		return invalid("say whether it's a User or a Group")
	}
	name := strings.TrimSpace(s.Name)
	if name == "" || name != s.Name || len(name) > 253 || strings.ContainsAny(name, "\n\r\t") {
		return invalid("give the name as Kubernetes sees it, such as oidc:ann@example.com")
	}
	if strings.HasPrefix(name, "system:") {
		return invalid("system: names belong to Kubernetes itself")
	}
	return nil
}

func (s Subject) rbac() rbacv1.Subject {
	return rbacv1.Subject{Kind: s.Kind, APIGroup: rbacv1.GroupName, Name: s.Name}
}

// hash names a subject's binding: names can hold characters a binding's
// name can't.
func (s Subject) hash() string {
	sum := sha256.Sum256([]byte(s.Kind + "\x00" + s.Name))
	return hex.EncodeToString(sum[:])[:12]
}

func (s Subject) annotations() map[string]string {
	return map[string]string{kindAnnotation: s.Kind, nameAnnotation: s.Name}
}

// people are the users and groups a binding names, without Kubernetes'
// own (system:) and without service accounts.
func people(subjects []rbacv1.Subject) []Subject {
	var out []Subject
	for _, s := range subjects {
		if (s.Kind == rbacv1.UserKind || s.Kind == rbacv1.GroupKind) && !strings.HasPrefix(s.Name, "system:") {
			out = append(out, Subject{Kind: s.Kind, Name: s.Name})
		}
	}
	return out
}

func managed(labels map[string]string) bool { return labels[managedByLabel] == ManagedBy }

// Member is someone's access to the cluster.
type Member struct {
	Subject
	// Role is owner, read-only or custom for members Glowpod manages; empty
	// for access made some other way.
	Role        string `json:"role,omitempty"`
	ClusterRole string `json:"clusterRole"`
	Binding     string `json:"binding"`
}

// Members are a cluster's members: Glowpod's, which can be changed here,
// and others', made with kubectl or GitOps, shown as they are.
type Members struct {
	Members []Member `json:"members"`
	Others  []Member `json:"others"`
}

// ClusterMembers lists who is bound cluster-wide.
func ClusterMembers(ctx context.Context, kube kubernetes.Interface) (Members, error) {
	l, err := kube.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
	if err != nil {
		return Members{}, fmt.Errorf("list cluster role bindings: %w", err)
	}
	out := Members{Members: []Member{}, Others: []Member{}}
	for i := range l.Items {
		b := &l.Items[i]
		if b.RoleRef.Kind != "ClusterRole" {
			continue
		}
		for _, s := range people(b.Subjects) {
			m := Member{Subject: s, ClusterRole: b.RoleRef.Name, Binding: b.Name}
			if managed(b.Labels) && strings.HasPrefix(b.Name, clusterPrefix) {
				m.Role = b.Labels[RoleLabel]
				out.Members = append(out.Members, m)
			} else {
				out.Others = append(out.Others, m)
			}
		}
	}
	sortMembers(out.Members)
	sortMembers(out.Others)
	return out, nil
}

func sortMembers(m []Member) {
	slices.SortFunc(m, func(a, b Member) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Kind, b.Kind)
	})
}

// SetClusterMember gives s a cluster role: owner, read-only, or custom with
// clusterRole. It replaces any role Glowpod gave s before.
func SetClusterMember(ctx context.Context, kube kubernetes.Interface, s Subject, role, clusterRole string) error {
	if err := s.check(); err != nil {
		return err
	}
	ref, ok := clusterRoles[role]
	switch {
	case role == RoleCustom:
		if len(validation.IsDNS1123Subdomain(clusterRole)) > 0 && !validColonName(clusterRole) {
			return invalid("pick a ClusterRole")
		}
		ref = clusterRole
	case !ok:
		return invalid("the role must be owner, read-only or custom")
	}
	b := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        clusterPrefix + s.hash(),
			Labels:      map[string]string{managedByLabel: ManagedBy, RoleLabel: role},
			Annotations: s.annotations(),
		},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: ref},
		Subjects: []rbacv1.Subject{s.rbac()},
	}
	return replace(ctx, clusterBinding{kube, b})
}

// validColonName allows ClusterRole names such as system:aggregate-to-edit.
func validColonName(name string) bool {
	if name == "" || len(name) > 253 {
		return false
	}
	return strings.IndexFunc(name, func(r rune) bool {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '.' || r == ':'
		return !ok
	}) < 0
}

// binding is one binding Glowpod wants to exist, cluster-wide or in a
// namespace.
type binding interface {
	name() string
	ref() string
	create(ctx context.Context) error
	// current is the existing binding's role and whether Glowpod made it.
	current(ctx context.Context) (ref string, ours bool, err error)
	remove(ctx context.Context) error
	// refresh brings an existing binding's labels and subjects up to date.
	refresh(ctx context.Context) error
}

// replace creates b, or brings an existing one to b's role. A role ref
// can't change, so a different one is deleted and made again. A binding
// with b's name that Glowpod didn't make is left alone.
func replace(ctx context.Context, b binding) error {
	err := b.create(ctx)
	if err == nil || !apierrors.IsAlreadyExists(err) {
		return wrap(b.name(), err)
	}
	ref, ours, err := b.current(ctx)
	if err != nil {
		return wrap(b.name(), err)
	}
	if !ours {
		return invalid("%s exists and wasn't made by Glowpod; change it where it was made", b.name())
	}
	if ref == b.ref() {
		return wrap(b.name(), b.refresh(ctx))
	}
	if err := b.remove(ctx); err != nil && !apierrors.IsNotFound(err) {
		return wrap(b.name(), err)
	}
	return wrap(b.name(), b.create(ctx))
}

type clusterBinding struct {
	kube kubernetes.Interface
	b    *rbacv1.ClusterRoleBinding
}

func (c clusterBinding) name() string { return c.b.Name }
func (c clusterBinding) ref() string  { return c.b.RoleRef.Name }
func (c clusterBinding) create(ctx context.Context) error {
	_, err := c.kube.RbacV1().ClusterRoleBindings().Create(ctx, c.b, metav1.CreateOptions{})
	return err //nolint:wrapcheck // replace wraps it
}
func (c clusterBinding) current(ctx context.Context) (string, bool, error) {
	cur, err := c.kube.RbacV1().ClusterRoleBindings().Get(ctx, c.b.Name, metav1.GetOptions{})
	if err != nil {
		return "", false, err //nolint:wrapcheck // replace wraps it
	}
	return cur.RoleRef.Name, managed(cur.Labels), nil
}
func (c clusterBinding) remove(ctx context.Context) error {
	return c.kube.RbacV1().ClusterRoleBindings().Delete(ctx, c.b.Name, metav1.DeleteOptions{}) //nolint:wrapcheck // replace wraps it
}
func (c clusterBinding) refresh(ctx context.Context) error {
	cur, err := c.kube.RbacV1().ClusterRoleBindings().Get(ctx, c.b.Name, metav1.GetOptions{})
	if err != nil {
		return err //nolint:wrapcheck // replace wraps it
	}
	cur.Labels, cur.Annotations, cur.Subjects = c.b.Labels, c.b.Annotations, c.b.Subjects
	_, err = c.kube.RbacV1().ClusterRoleBindings().Update(ctx, cur, metav1.UpdateOptions{})
	return err //nolint:wrapcheck // replace wraps it
}

func wrap(name string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("binding %s: %w", name, err)
}

// RemoveClusterMember takes away the cluster role Glowpod gave s.
func RemoveClusterMember(ctx context.Context, kube kubernetes.Interface, s Subject) error {
	if err := s.check(); err != nil {
		return err
	}
	name := clusterPrefix + s.hash()
	cur, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return wrap(name, err)
	}
	if !managed(cur.Labels) {
		return invalid("%s wasn't made by Glowpod; change it where it was made", name)
	}
	return wrap(name, kube.RbacV1().ClusterRoleBindings().Delete(ctx, name, metav1.DeleteOptions{}))
}
