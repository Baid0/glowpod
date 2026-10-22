package nodes

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"

	"glowpod/internal/kerr"
)

const (
	mirrorAnnotation = "kubernetes.io/config.mirror"
	evictWorkers     = 5
	// drainTimeout bounds the whole request; evictions don't wait for pods to stop.
	drainTimeout = 60 * time.Second
)

// Cordon marks the node (un)schedulable. Pods already on it keep running.
func Cordon(ctx context.Context, kube kubernetes.Interface, name string, cordon bool) error {
	patch := fmt.Appendf(nil, `{"spec":{"unschedulable":%t}}`, cordon)
	if _, err := kube.CoreV1().Nodes().Patch(ctx, name, types.MergePatchType, patch, metav1.PatchOptions{}); err != nil {
		return fmt.Errorf("cordon %s: %w", name, err)
	}
	return nil
}

// PodRef is a pod in a drain plan or result, with why it's there.
type PodRef struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason,omitempty"`
	// LocalData is true when the pod keeps data in an emptyDir, which goes with it.
	LocalData bool `json:"localData,omitempty"`
}

// Plan is what draining would do: which pods move and which stay.
type Plan struct {
	Evict []PodRef `json:"evict"`
	Skip  []PodRef `json:"skip"`
}

// PlanFor sorts a node's pods the way `kubectl drain` would, with one
// difference: pods no controller manages are left alone instead of being
// deleted, since nothing would bring them back.
func PlanFor(pods []corev1.Pod) Plan {
	p := Plan{Evict: []PodRef{}, Skip: []PodRef{}}
	for i := range pods {
		pod := &pods[i]
		ref := PodRef{Namespace: pod.Namespace, Name: pod.Name}
		owner := metav1.GetControllerOf(pod)
		switch {
		case pod.DeletionTimestamp != nil:
			ref.Reason = "Already shutting down."
			p.Skip = append(p.Skip, ref)
		case pod.Annotations[mirrorAnnotation] != "":
			ref.Reason = "A static pod: the node itself runs it."
			p.Skip = append(p.Skip, ref)
		case owner != nil && owner.Kind == "DaemonSet":
			ref.Reason = "Runs on every node (DaemonSet)."
			p.Skip = append(p.Skip, ref)
		case owner == nil && pod.Status.Phase != corev1.PodSucceeded && pod.Status.Phase != corev1.PodFailed:
			ref.Reason = "No controller manages it, so nothing would start it again elsewhere. Delete it yourself if that's OK."
			p.Skip = append(p.Skip, ref)
		default:
			ref.LocalData = slices.ContainsFunc(pod.Spec.Volumes, func(v corev1.Volume) bool { return v.EmptyDir != nil })
			p.Evict = append(p.Evict, ref)
		}
	}
	return p
}

// DrainResult says what happened to each pod.
type DrainResult struct {
	Evicted []PodRef `json:"evicted"`
	// Blocked pods are protected by a disruption budget right now.
	Blocked []PodRef `json:"blocked"`
	Failed  []PodRef `json:"failed"`
	Skipped []PodRef `json:"skipped"`
}

// Drain cordons the node and asks to evict every pod the plan moves. The
// Eviction API honours PodDisruptionBudgets: a pod they protect is
// reported as blocked, not forced. It doesn't wait for pods to stop.
func Drain(ctx context.Context, kube kubernetes.Interface, name string) (*DrainResult, error) {
	ctx, cancel := context.WithTimeout(ctx, drainTimeout)
	defer cancel()
	if err := Cordon(ctx, kube, name, true); err != nil {
		return nil, err
	}
	pods, err := PodsOn(ctx, kube, name)
	if err != nil {
		return nil, err
	}
	plan := PlanFor(pods)
	res := &DrainResult{Evicted: []PodRef{}, Blocked: []PodRef{}, Failed: []PodRef{}, Skipped: plan.Skip}

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, evictWorkers)
	for _, ref := range plan.Evict {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			err := kube.CoreV1().Pods(ref.Namespace).EvictV1(ctx, &policyv1.Eviction{
				ObjectMeta: metav1.ObjectMeta{Namespace: ref.Namespace, Name: ref.Name},
			})
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil, apierrors.IsNotFound(err):
				res.Evicted = append(res.Evicted, ref)
			case apierrors.IsTooManyRequests(err):
				ref.Reason = "A disruption budget (PodDisruptionBudget) doesn't allow it right now. Try again once other pods are ready."
				res.Blocked = append(res.Blocked, ref)
			default:
				ref.Reason = kerr.Classify(err).Message
				res.Failed = append(res.Failed, ref)
			}
		})
	}
	wg.Wait()
	for _, list := range [][]PodRef{res.Evicted, res.Blocked, res.Failed} {
		slices.SortFunc(list, func(a, b PodRef) int {
			if a.Namespace != b.Namespace {
				return cmp.Compare(a.Namespace, b.Namespace)
			}
			return cmp.Compare(a.Name, b.Name)
		})
	}
	return res, nil
}
