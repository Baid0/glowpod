package members

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

// Project is a named group of namespaces and who may work in them.
type Project struct {
	Name       string          `json:"name"`
	Namespaces []string        `json:"namespaces"`
	Members    []ProjectMember `json:"members"`
}

// ProjectMember has a project role in the project's namespaces.
type ProjectMember struct {
	Subject
	Role string `json:"role"`
	// Missing are the project's namespaces without this member's binding,
	// such as one added to the project with kubectl.
	Missing []string `json:"missing,omitempty"`
}

// Projects are a cluster's projects, and the namespaces in none.
type Projects struct {
	Projects   []Project `json:"projects"`
	Unassigned []string  `json:"unassigned"`
}

// ListProjects reads projects from namespace labels, and their members
// from the bindings Glowpod made in those namespaces.
func ListProjects(ctx context.Context, kube kubernetes.Interface) (Projects, error) {
	nss, err := kube.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return Projects{}, fmt.Errorf("list namespaces: %w", err)
	}
	byName := map[string]*Project{}
	out := Projects{Projects: []Project{}, Unassigned: []string{}}
	for _, ns := range nss.Items {
		p := ns.Labels[ProjectLabel]
		if p == "" {
			out.Unassigned = append(out.Unassigned, ns.Name)
			continue
		}
		if byName[p] == nil {
			byName[p] = &Project{Name: p}
		}
		byName[p].Namespaces = append(byName[p].Namespaces, ns.Name)
	}
	for _, p := range byName {
		members, err := projectMembers(ctx, kube, p)
		if err != nil {
			return Projects{}, err
		}
		p.Members = members
		slices.Sort(p.Namespaces)
		out.Projects = append(out.Projects, *p)
	}
	slices.SortFunc(out.Projects, func(a, b Project) int { return strings.Compare(a.Name, b.Name) })
	slices.Sort(out.Unassigned)
	return out, nil
}

func projectMembers(ctx context.Context, kube kubernetes.Interface, p *Project) ([]ProjectMember, error) {
	type key struct {
		s    Subject
		role string
	}
	has := map[key][]string{}
	sel := metav1.ListOptions{LabelSelector: managedByLabel + "=" + ManagedBy + "," + ProjectLabel + "=" + p.Name}
	for _, ns := range p.Namespaces {
		l, err := kube.RbacV1().RoleBindings(ns).List(ctx, sel)
		if apierrors.IsForbidden(err) {
			continue // someone who can't see this namespace's bindings
		}
		if err != nil {
			return nil, fmt.Errorf("list role bindings in %s: %w", ns, err)
		}
		for _, b := range l.Items {
			for _, s := range people(b.Subjects) {
				k := key{s, b.Labels[RoleLabel]}
				has[k] = append(has[k], ns)
			}
		}
	}
	members := make([]ProjectMember, 0, len(has))
	for k, nss := range has {
		m := ProjectMember{Subject: k.s, Role: k.role}
		for _, ns := range p.Namespaces {
			if !slices.Contains(nss, ns) {
				m.Missing = append(m.Missing, ns)
			}
		}
		members = append(members, m)
	}
	slices.SortFunc(members, func(a, b ProjectMember) int {
		if c := strings.Compare(a.Name, b.Name); c != 0 {
			return c
		}
		return strings.Compare(a.Kind, b.Kind)
	})
	return members, nil
}

func checkProject(name string) error {
	if len(validation.IsDNS1123Label(name)) > 0 {
		return invalid("a project's name is lowercase letters, digits and dashes, such as payments")
	}
	return nil
}

func project(ctx context.Context, kube kubernetes.Interface, name string) (*Project, error) {
	all, err := ListProjects(ctx, kube)
	if err != nil {
		return nil, err
	}
	for i := range all.Projects {
		if all.Projects[i].Name == name {
			return &all.Projects[i], nil
		}
	}
	return nil, invalid("there's no project %s", name)
}

// CreateProject puts namespaces in a new project.
func CreateProject(ctx context.Context, kube kubernetes.Interface, name string, namespaces []string) error {
	if err := checkProject(name); err != nil {
		return err
	}
	if len(namespaces) == 0 {
		return invalid("pick at least one namespace")
	}
	if p, err := project(ctx, kube, name); err == nil && p != nil {
		return invalid("there's already a project %s", name)
	}
	return SetProjectNamespaces(ctx, kube, name, namespaces)
}

// SetProjectNamespaces makes namespaces the project's: new ones get the
// project's label and its members' bindings; ones left out lose both.
func SetProjectNamespaces(ctx context.Context, kube kubernetes.Interface, name string, namespaces []string) error {
	if err := checkProject(name); err != nil {
		return err
	}
	all, err := ListProjects(ctx, kube)
	if err != nil {
		return err
	}
	cur := &Project{Name: name}
	for i := range all.Projects {
		if all.Projects[i].Name == name {
			cur = &all.Projects[i]
		}
	}
	for _, ns := range namespaces {
		for _, p := range all.Projects {
			if p.Name != name && slices.Contains(p.Namespaces, ns) {
				return invalid("%s is in project %s; take it out of that one first", ns, p.Name)
			}
		}
	}
	for _, ns := range namespaces {
		if slices.Contains(cur.Namespaces, ns) {
			continue
		}
		if err := labelNamespace(ctx, kube, ns, name); err != nil {
			return err
		}
		for _, m := range cur.Members {
			if err := replace(ctx, projectBinding(kube, ns, name, m.Subject, m.Role)); err != nil {
				return err
			}
		}
	}
	for _, ns := range cur.Namespaces {
		if slices.Contains(namespaces, ns) {
			continue
		}
		if err := removeProjectBindings(ctx, kube, ns, name, nil); err != nil {
			return err
		}
		if err := labelNamespace(ctx, kube, ns, ""); err != nil {
			return err
		}
	}
	return nil
}

// labelNamespace sets or (with "") removes the project label.
func labelNamespace(ctx context.Context, kube kubernetes.Interface, ns, project string) error {
	var value any
	if project != "" {
		value = project
	}
	patch, err := json.Marshal(map[string]any{"metadata": map[string]any{"labels": map[string]any{ProjectLabel: value}}})
	if err != nil {
		return fmt.Errorf("encode the label: %w", err)
	}
	if _, err := kube.CoreV1().Namespaces().Patch(ctx, ns, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("label namespace %s: %w", ns, err)
	}
	return nil
}

// SetProjectMember gives s a project role in each of the project's
// namespaces.
func SetProjectMember(ctx context.Context, kube kubernetes.Interface, name string, s Subject, role string) error {
	if err := s.check(); err != nil {
		return err
	}
	if _, ok := projectRoles[role]; !ok {
		return invalid("the role must be owner, member or read-only")
	}
	p, err := project(ctx, kube, name)
	if err != nil {
		return err
	}
	for _, ns := range p.Namespaces {
		if err := replace(ctx, projectBinding(kube, ns, name, s, role)); err != nil {
			return err
		}
	}
	return nil
}

// RemoveProjectMember takes s's project role away in every namespace.
func RemoveProjectMember(ctx context.Context, kube kubernetes.Interface, name string, s Subject) error {
	if err := s.check(); err != nil {
		return err
	}
	p, err := project(ctx, kube, name)
	if err != nil {
		return err
	}
	for _, ns := range p.Namespaces {
		if err := removeProjectBindings(ctx, kube, ns, name, &s); err != nil {
			return err
		}
	}
	return nil
}

// DeleteProject takes its namespaces out of it and removes its members'
// bindings. The namespaces and everything in them stay.
func DeleteProject(ctx context.Context, kube kubernetes.Interface, name string) error {
	if err := checkProject(name); err != nil {
		return err
	}
	return SetProjectNamespaces(ctx, kube, name, nil)
}

// removeProjectBindings deletes the project's bindings in ns: all of them,
// or only s's.
func removeProjectBindings(ctx context.Context, kube kubernetes.Interface, ns, name string, s *Subject) error {
	if s != nil {
		err := kube.RbacV1().RoleBindings(ns).Delete(ctx, projectPrefix+name+"-"+s.hash(), metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("remove a member in %s: %w", ns, err)
		}
		return nil
	}
	sel := metav1.ListOptions{LabelSelector: managedByLabel + "=" + ManagedBy + "," + ProjectLabel + "=" + name}
	l, err := kube.RbacV1().RoleBindings(ns).List(ctx, sel)
	if err != nil {
		return fmt.Errorf("list role bindings in %s: %w", ns, err)
	}
	for _, b := range l.Items {
		if err := kube.RbacV1().RoleBindings(ns).Delete(ctx, b.Name, metav1.DeleteOptions{}); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("remove %s in %s: %w", b.Name, ns, err)
		}
	}
	return nil
}

func projectBinding(kube kubernetes.Interface, ns, project string, s Subject, role string) binding {
	return roleBinding{kube, &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:        projectPrefix + project + "-" + s.hash(),
			Namespace:   ns,
			Labels:      map[string]string{managedByLabel: ManagedBy, RoleLabel: role, ProjectLabel: project},
			Annotations: s.annotations(),
		},
		RoleRef:  rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: projectRoles[role]},
		Subjects: []rbacv1.Subject{s.rbac()},
	}}
}

type roleBinding struct {
	kube kubernetes.Interface
	b    *rbacv1.RoleBinding
}

func (r roleBinding) name() string { return r.b.Namespace + "/" + r.b.Name }
func (r roleBinding) ref() string  { return r.b.RoleRef.Name }
func (r roleBinding) create(ctx context.Context) error {
	_, err := r.kube.RbacV1().RoleBindings(r.b.Namespace).Create(ctx, r.b, metav1.CreateOptions{})
	return err //nolint:wrapcheck // replace wraps it
}
func (r roleBinding) current(ctx context.Context) (string, bool, error) {
	cur, err := r.kube.RbacV1().RoleBindings(r.b.Namespace).Get(ctx, r.b.Name, metav1.GetOptions{})
	if err != nil {
		return "", false, err //nolint:wrapcheck // replace wraps it
	}
	return cur.RoleRef.Name, managed(cur.Labels), nil
}
func (r roleBinding) remove(ctx context.Context) error {
	return r.kube.RbacV1().RoleBindings(r.b.Namespace).Delete(ctx, r.b.Name, metav1.DeleteOptions{}) //nolint:wrapcheck // replace wraps it
}
func (r roleBinding) refresh(ctx context.Context) error {
	cur, err := r.kube.RbacV1().RoleBindings(r.b.Namespace).Get(ctx, r.b.Name, metav1.GetOptions{})
	if err != nil {
		return err //nolint:wrapcheck // replace wraps it
	}
	cur.Labels, cur.Annotations, cur.Subjects = r.b.Labels, r.b.Annotations, r.b.Subjects
	_, err = r.kube.RbacV1().RoleBindings(r.b.Namespace).Update(ctx, cur, metav1.UpdateOptions{})
	return err //nolint:wrapcheck // replace wraps it
}
