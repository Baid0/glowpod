package events

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func event(ns, name, reason string, at time.Time) *corev1.Event {
	return &corev1.Event{
		ObjectMeta:     metav1.ObjectMeta{Namespace: ns, Name: name},
		Reason:         reason,
		Type:           corev1.EventTypeNormal,
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "web-1", Namespace: ns},
		LastTimestamp:  metav1.NewTime(at),
	}
}

func TestNewest(t *testing.T) {
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	objs := []runtime.Object{
		event("a", "old", "Pulled", now.Add(-time.Hour)),
		event("a", "new", "Started", now.Add(-time.Minute)),
		event("b", "mid", "Scheduled", now.Add(-10*time.Minute)),
	}
	tests := []struct {
		name       string
		namespaces []string
		limit      int
		want       []string
	}{
		{name: "all namespaces, newest first", namespaces: []string{""}, limit: 10, want: []string{"Started", "Scheduled", "Pulled"}},
		{name: "limit keeps the newest", namespaces: []string{""}, limit: 2, want: []string{"Started", "Scheduled"}},
		{name: "one namespace", namespaces: []string{"b"}, limit: 10, want: []string{"Scheduled"}},
		{name: "several namespaces", namespaces: []string{"a", "b"}, limit: 10, want: []string{"Started", "Scheduled", "Pulled"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Newest(context.Background(), fake.NewClientset(objs...), tt.namespaces, "", tt.limit)
			if err != nil {
				t.Fatal(err)
			}
			var reasons []string
			for _, e := range got {
				reasons = append(reasons, e.Reason)
			}
			if len(reasons) != len(tt.want) {
				t.Fatalf("got %v, want %v", reasons, tt.want)
			}
			for i := range reasons {
				if reasons[i] != tt.want[i] {
					t.Fatalf("got %v, want %v", reasons, tt.want)
				}
			}
		})
	}
}

func TestAbout(t *testing.T) {
	sel, err := fields.ParseSelector(About("Pod", "web-1"))
	if err != nil {
		t.Fatal(err)
	}
	match := fields.Set{"involvedObject.kind": "Pod", "involvedObject.name": "web-1"}
	other := fields.Set{"involvedObject.kind": "Pod", "involvedObject.name": "web-2"}
	if !sel.Matches(match) || sel.Matches(other) {
		t.Errorf("About = %q, want a selector for Pod web-1 only", sel)
	}
}
