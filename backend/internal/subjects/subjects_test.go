package subjects

import (
	"testing"

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func crb(name, role string, subjects ...rbacv1.Subject) *rbacv1.ClusterRoleBinding {
	return &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: role},
		Subjects:   subjects,
	}
}

func group(n string) rbacv1.Subject { return rbacv1.Subject{Kind: rbacv1.GroupKind, Name: n} }
func user(n string) rbacv1.Subject  { return rbacv1.Subject{Kind: rbacv1.UserKind, Name: n} }

func TestWhoSeesACluster(t *testing.T) {
	defaults := []*rbacv1.ClusterRoleBinding{
		// Every cluster has these: they don't make it anyone's.
		crb("system:discovery", "system:discovery", group(Everyone)),
		crb("system:basic-user", "system:basic-user", group(Everyone)),
		crb("system:public-info-viewer", "system:public-info-viewer", group(Everyone), group("system:unauthenticated")),
	}
	tests := []struct {
		name   string
		extra  []*rbacv1.ClusterRoleBinding
		user   string
		groups []string
		sees   bool
	}{
		{"a fresh cluster is no one's", nil, "oidc:bob", []string{"oidc:ops"}, false},
		{"a binding to your group", []*rbacv1.ClusterRoleBinding{crb("ops", "edit", group("oidc:ops"))}, "oidc:bob", []string{"oidc:ops"}, true},
		{"a binding to you", []*rbacv1.ClusterRoleBinding{crb("bob", "view", user("oidc:bob"))}, "oidc:bob", nil, true},
		{"someone else's", []*rbacv1.ClusterRoleBinding{crb("ann", "view", user("oidc:ann"))}, "oidc:bob", nil, false},
		{"a real grant to everyone", []*rbacv1.ClusterRoleBinding{crb("all-view", "view", group(Everyone))}, "oidc:bob", nil, true},
	}
	// A tool's narrow role for everyone in one namespace doesn't count.
	narrow := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "sealed-secrets-proxier", Namespace: "kube-system"},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "sealed-secrets-proxier"},
		Subjects:   []rbacv1.Subject{group(Everyone), group("oidc:ops")},
	}
	kube := fake.NewClientset(narrow)
	set, err := List(t.Context(), kube)
	if err != nil {
		t.Fatal(err)
	}
	if set.Includes("oidc:bob", nil) || !set.Includes("oidc:bob", []string{"oidc:ops"}) {
		t.Fatalf("a namespace grant to everyone isn't the cluster's; one to a group is (set %+v)", set)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			kube := fake.NewClientset()
			for _, b := range append(append([]*rbacv1.ClusterRoleBinding{}, defaults...), tt.extra...) {
				if _, err := kube.RbacV1().ClusterRoleBindings().Create(t.Context(), b, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			set, err := List(t.Context(), kube)
			if err != nil {
				t.Fatal(err)
			}
			if got := set.Includes(tt.user, tt.groups); got != tt.sees {
				t.Fatalf("sees = %v, want %v (set %+v)", got, tt.sees, set)
			}
		})
	}
}
