// Package subjects finds who has any access to a cluster: the users and
// groups named in its RoleBindings and ClusterRoleBindings. The Hub uses it
// to decide which clusters to list for someone. It decides what to show,
// never what's allowed: every call still runs as the person, and the API
// server checks it.
package subjects

import (
	"context"
	"fmt"
	"slices"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/paging"
)

// Everyone is the group Kubernetes gives every signed-in user.
const Everyone = "system:authenticated"

// Set is the users and groups bound to anything in a cluster, sorted.
type Set struct {
	Users  []string `json:"users"`
	Groups []string `json:"groups"`
}

// Includes says whether someone with this username and these groups is in s.
func (s Set) Includes(user string, groups []string) bool {
	if slices.Contains(s.Groups, Everyone) {
		return true
	}
	if _, ok := slices.BinarySearch(s.Users, user); ok {
		return true
	}
	for _, g := range groups {
		if _, ok := slices.BinarySearch(s.Groups, g); ok {
			return true
		}
	}
	return false
}

// List reads every binding in the cluster.
func List(ctx context.Context, kube kubernetes.Interface) (Set, error) {
	users, groups := map[string]bool{}, map[string]bool{}
	add := func(subjects []rbacv1.Subject) {
		for _, s := range subjects {
			switch s.Kind {
			case rbacv1.UserKind:
				users[s.Name] = true
			case rbacv1.GroupKind:
				groups[s.Name] = true
			}
		}
	}
	crbs, err := paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]rbacv1.ClusterRoleBinding, string, error) {
		l, err := kube.RbacV1().ClusterRoleBindings().List(ctx, o)
		if err != nil {
			return nil, "", err
		}
		return l.Items, l.Continue, nil
	})
	if err != nil {
		return Set{}, fmt.Errorf("list cluster role bindings: %w", err)
	}
	for i := range crbs {
		add(crbs[i].Subjects)
	}
	rbs, err := paging.All(ctx, func(ctx context.Context, o metav1.ListOptions) ([]rbacv1.RoleBinding, string, error) {
		l, err := kube.RbacV1().RoleBindings("").List(ctx, o)
		if err != nil {
			return nil, "", err
		}
		return l.Items, l.Continue, nil
	})
	if err != nil {
		return Set{}, fmt.Errorf("list role bindings: %w", err)
	}
	for i := range rbs {
		add(rbs[i].Subjects)
	}
	return Set{Users: sorted(users), Groups: sorted(groups)}, nil
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
