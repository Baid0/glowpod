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
	"strings"

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
	// Namespaces are the users and groups each namespace's RoleBindings
	// name, so people who may not list namespaces can be offered theirs.
	Namespaces map[string]Set `json:"namespaces,omitempty"`
}

// NamespacesOf are the namespaces whose RoleBindings name user or one of
// groups, sorted.
func (s Set) NamespacesOf(user string, groups []string) []string {
	var out []string
	for ns, in := range s.Namespaces {
		if _, ok := slices.BinarySearch(in.Users, user); ok {
			out = append(out, ns)
			continue
		}
		for _, g := range groups {
			if _, ok := slices.BinarySearch(in.Groups, g); ok {
				out = append(out, ns)
				break
			}
		}
	}
	slices.Sort(out)
	return out
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
	add := func(role rbacv1.RoleRef, subjects []rbacv1.Subject, clusterWide bool) {
		// Kubernetes' own roles (system:discovery, system:basic-user, …) are
		// bound to every signed-in user in every cluster; they say nothing
		// about who works there.
		if strings.HasPrefix(role.Name, "system:") {
			return
		}
		for _, s := range subjects {
			switch {
			case s.Kind == rbacv1.UserKind:
				users[s.Name] = true
			// Everyone counts only through a cluster-wide grant: tools bind
			// narrow helper roles to everyone in one namespace (sealed-secrets
			// lets kubeseal fetch its certificate), which doesn't make the
			// cluster everyone's.
			case s.Kind == rbacv1.GroupKind && (s.Name != Everyone || clusterWide):
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
		add(crbs[i].RoleRef, crbs[i].Subjects, true)
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
	perNS := map[string]*[2]map[string]bool{}
	for i := range rbs {
		add(rbs[i].RoleRef, rbs[i].Subjects, false)
		if strings.HasPrefix(rbs[i].RoleRef.Name, "system:") {
			continue
		}
		in := perNS[rbs[i].Namespace]
		if in == nil {
			in = &[2]map[string]bool{{}, {}}
			perNS[rbs[i].Namespace] = in
		}
		for _, s := range rbs[i].Subjects {
			switch {
			case s.Kind == rbacv1.UserKind:
				in[0][s.Name] = true
			// Everyone's helper roles say nothing about whose namespace it is.
			case s.Kind == rbacv1.GroupKind && s.Name != Everyone:
				in[1][s.Name] = true
			}
		}
	}
	set := Set{Users: sorted(users), Groups: sorted(groups)}
	for ns, in := range perNS {
		if len(in[0])+len(in[1]) == 0 {
			continue
		}
		if set.Namespaces == nil {
			set.Namespaces = map[string]Set{}
		}
		set.Namespaces[ns] = Set{Users: sorted(in[0]), Groups: sorted(in[1])}
	}
	return set, nil
}

func sorted(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
