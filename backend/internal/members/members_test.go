package members

import (
	"errors"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

var (
	ann = Subject{Kind: rbacv1.UserKind, Name: "oidc:ann@example.com"}
	ops = Subject{Kind: rbacv1.GroupKind, Name: "ldap:ops"}
)

func TestSubjectCheck(t *testing.T) {
	tests := []struct {
		name string
		s    Subject
		ok   bool
	}{
		{"a user", ann, true},
		{"a group", ops, true},
		{"a service account", Subject{Kind: "ServiceAccount", Name: "x"}, false},
		{"Kubernetes' own", Subject{Kind: rbacv1.GroupKind, Name: "system:masters"}, false},
		{"empty", Subject{Kind: rbacv1.UserKind}, false},
		{"spaces around", Subject{Kind: rbacv1.UserKind, Name: " ann "}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.check(); (err == nil) != tt.ok {
				t.Fatalf("check = %v, want ok %v", err, tt.ok)
			}
		})
	}
}

func TestClusterMembers(t *testing.T) {
	kube := fake.NewClientset(&rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "ops-admins"},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "cluster-admin"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.GroupKind, Name: "ldap:ops"},
			{Kind: rbacv1.GroupKind, Name: "system:masters"},
			{Kind: rbacv1.ServiceAccountKind, Name: "argocd", Namespace: "argocd"},
		},
	})
	ctx := t.Context()

	if err := SetClusterMember(ctx, kube, ann, RoleReadOnly, ""); err != nil {
		t.Fatal(err)
	}
	m, err := ClusterMembers(ctx, kube)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Members) != 1 || m.Members[0].Subject != ann || m.Members[0].Role != RoleReadOnly || m.Members[0].ClusterRole != "view" {
		t.Fatalf("members = %+v", m.Members)
	}
	if len(m.Others) != 1 || m.Others[0].Subject != ops || m.Others[0].ClusterRole != "cluster-admin" {
		t.Fatalf("others (no system: names or service accounts) = %+v", m.Others)
	}

	// A new role replaces the binding; a role ref can't be changed in place.
	if err := SetClusterMember(ctx, kube, ann, RoleOwner, ""); err != nil {
		t.Fatal(err)
	}
	if err := SetClusterMember(ctx, kube, ann, RoleCustom, "system:aggregate-to-edit"); err != nil {
		t.Fatal(err)
	}
	b, err := kube.RbacV1().ClusterRoleBindings().Get(ctx, clusterPrefix+ann.hash(), metav1.GetOptions{})
	if err != nil || b.RoleRef.Name != "system:aggregate-to-edit" || b.Labels[RoleLabel] != RoleCustom || b.Annotations[nameAnnotation] != ann.Name {
		t.Fatalf("binding = %+v, %v", b, err)
	}

	for _, bad := range []struct{ role, cr string }{{"admin", ""}, {RoleCustom, ""}, {RoleCustom, "Not Valid"}} {
		if err := SetClusterMember(ctx, kube, ann, bad.role, bad.cr); !errors.Is(err, ErrInvalid) {
			t.Fatalf("%v: err = %v", bad, err)
		}
	}

	if err := RemoveClusterMember(ctx, kube, ann); err != nil {
		t.Fatal(err)
	}
	if m, _ := ClusterMembers(ctx, kube); len(m.Members) != 0 {
		t.Fatal("removed")
	}
	if err := RemoveClusterMember(ctx, kube, ann); err != nil {
		t.Fatal("removing twice is fine")
	}
}

func TestBindingsGlowpodDidntMakeAreLeftAlone(t *testing.T) {
	kube := fake.NewClientset(&rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: clusterPrefix + ann.hash()},
		RoleRef:    rbacv1.RoleRef{Kind: "ClusterRole", Name: "view"},
	})
	if err := SetClusterMember(t.Context(), kube, ann, RoleOwner, ""); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
	if err := RemoveClusterMember(t.Context(), kube, ann); !errors.Is(err, ErrInvalid) {
		t.Fatalf("err = %v", err)
	}
}

func namespaces(names ...string) []*corev1.Namespace {
	out := make([]*corev1.Namespace, 0, len(names))
	for _, n := range names {
		out = append(out, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: n}})
	}
	return out
}

func TestProjects(t *testing.T) {
	kube := fake.NewClientset()
	for _, ns := range namespaces("payments-api", "payments-db", "shop", "default") {
		if _, err := kube.CoreV1().Namespaces().Create(t.Context(), ns, metav1.CreateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	ctx := t.Context()

	if err := CreateProject(ctx, kube, "payments", []string{"payments-api"}); err != nil {
		t.Fatal(err)
	}
	if err := SetProjectMember(ctx, kube, "payments", ops, RoleMember); err != nil {
		t.Fatal(err)
	}
	// A namespace added later gets the project's members.
	if err := SetProjectNamespaces(ctx, kube, "payments", []string{"payments-api", "payments-db"}); err != nil {
		t.Fatal(err)
	}
	all, err := ListProjects(ctx, kube)
	if err != nil {
		t.Fatal(err)
	}
	if len(all.Projects) != 1 || !slices.Equal(all.Projects[0].Namespaces, []string{"payments-api", "payments-db"}) {
		t.Fatalf("projects = %+v", all.Projects)
	}
	if !slices.Equal(all.Unassigned, []string{"default", "shop"}) {
		t.Fatalf("unassigned = %v", all.Unassigned)
	}
	m := all.Projects[0].Members
	if len(m) != 1 || m[0].Subject != ops || m[0].Role != RoleMember || len(m[0].Missing) != 0 {
		t.Fatalf("members = %+v", m)
	}
	rb, err := kube.RbacV1().RoleBindings("payments-db").Get(ctx, projectPrefix+"payments-"+ops.hash(), metav1.GetOptions{})
	if err != nil || rb.RoleRef.Name != "edit" {
		t.Fatalf("payments-db binding = %+v, %v", rb, err)
	}

	// A binding deleted outside Glowpod shows as missing.
	_ = kube.RbacV1().RoleBindings("payments-db").Delete(ctx, rb.Name, metav1.DeleteOptions{})
	all, _ = ListProjects(ctx, kube)
	if got := all.Projects[0].Members[0].Missing; !slices.Equal(got, []string{"payments-db"}) {
		t.Fatalf("missing = %v", got)
	}
	// Setting the member again fixes it.
	if err := SetProjectMember(ctx, kube, "payments", ops, RoleOwner); err != nil {
		t.Fatal(err)
	}
	rb, _ = kube.RbacV1().RoleBindings("payments-db").Get(ctx, projectPrefix+"payments-"+ops.hash(), metav1.GetOptions{})
	if rb == nil || rb.RoleRef.Name != "admin" {
		t.Fatalf("owner is admin: %+v", rb)
	}

	if err := CreateProject(ctx, kube, "shop", []string{"payments-api"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a namespace is in one project at a time: %v", err)
	}
	if err := CreateProject(ctx, kube, "payments", []string{"shop"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("names are unique: %v", err)
	}
	if err := CreateProject(ctx, kube, "Not_Valid", []string{"shop"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("names are DNS labels: %v", err)
	}

	// Taking a namespace out removes the project's bindings there.
	if err := SetProjectNamespaces(ctx, kube, "payments", []string{"payments-api"}); err != nil {
		t.Fatal(err)
	}
	if l, _ := kube.RbacV1().RoleBindings("payments-db").List(ctx, metav1.ListOptions{}); len(l.Items) != 0 {
		t.Fatal("bindings left in a namespace taken out")
	}
	ns, _ := kube.CoreV1().Namespaces().Get(ctx, "payments-db", metav1.GetOptions{})
	if _, ok := ns.Labels[ProjectLabel]; ok {
		t.Fatal("label left")
	}

	if err := RemoveProjectMember(ctx, kube, "payments", ops); err != nil {
		t.Fatal(err)
	}
	if err := DeleteProject(ctx, kube, "payments"); err != nil {
		t.Fatal(err)
	}
	all, _ = ListProjects(ctx, kube)
	if len(all.Projects) != 0 || len(all.Unassigned) != 4 {
		t.Fatalf("after delete: %+v", all)
	}
}
