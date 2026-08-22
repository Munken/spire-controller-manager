package spireentry

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func boolPtr(b bool) *bool { return &b }

func newUnstructured(apiVersion, kind, namespace, name, uid, rv string, owner *metav1.OwnerReference) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetAPIVersion(apiVersion)
	obj.SetKind(kind)
	obj.SetNamespace(namespace)
	obj.SetName(name)
	obj.SetUID(types.UID(uid))
	obj.SetResourceVersion(rv)
	if owner != nil {
		obj.SetOwnerReferences([]metav1.OwnerReference{*owner})
	}
	return obj
}

func TestResolveUltimateOwner(t *testing.T) {
	deployment := newUnstructured("apps/v1", "Deployment", "ns", "web", "dep-uid", "10", nil)
	replicaSet := newUnstructured("apps/v1", "ReplicaSet", "ns", "web-abc", "rs-uid", "20", &metav1.OwnerReference{
		APIVersion: "apps/v1", Kind: "Deployment", Name: "web", UID: "dep-uid", Controller: boolPtr(true),
	})

	c := fake.NewClientBuilder().WithObjects(deployment, replicaSet).Build()

	t.Run("resolves to top of chain", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns",
				Name:      "web-abc-123",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "web-abc", UID: "rs-uid", Controller: boolPtr(true),
				}},
			},
		}
		owner, token, err := resolveUltimateOwner(context.Background(), c, pod)
		require.NoError(t, err)
		require.NotNil(t, owner)
		require.Equal(t, "Deployment", owner.Kind)
		require.Equal(t, "web", owner.Name)
		require.Equal(t, "dep-uid", owner.UID)
		require.Contains(t, token, "rs-uid/20")
		require.Contains(t, token, "dep-uid/10")
	})

	t.Run("bare pod returns nil", func(t *testing.T) {
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "bare"}}
		owner, token, err := resolveUltimateOwner(context.Background(), c, pod)
		require.NoError(t, err)
		require.Nil(t, owner)
		require.Empty(t, token)
	})

	t.Run("non-controller owner reference is ignored", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns", Name: "no-controller",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "web-abc", UID: "rs-uid", Controller: boolPtr(false),
				}},
			},
		}
		owner, _, err := resolveUltimateOwner(context.Background(), c, pod)
		require.NoError(t, err)
		require.Nil(t, owner)
	})

	t.Run("missing owner is an error", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns", Name: "orphan",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "does-not-exist", UID: "missing", Controller: boolPtr(true),
				}},
			},
		}
		owner, _, err := resolveUltimateOwner(context.Background(), c, pod)
		require.Error(t, err)
		require.Nil(t, owner)
	})

	t.Run("nil client is an error", func(t *testing.T) {
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "ns", Name: "x",
				OwnerReferences: []metav1.OwnerReference{{
					APIVersion: "apps/v1", Kind: "ReplicaSet", Name: "web-abc", UID: "rs-uid", Controller: boolPtr(true),
				}},
			},
		}
		_, _, err := resolveUltimateOwner(context.Background(), nil, pod)
		require.Error(t, err)
	})
}

func TestResolveUltimateOwnerCycle(t *testing.T) {
	a := newUnstructured("example.com/v1", "A", "ns", "a", "a-uid", "1", &metav1.OwnerReference{
		APIVersion: "example.com/v1", Kind: "B", Name: "b", UID: "b-uid", Controller: boolPtr(true),
	})
	b := newUnstructured("example.com/v1", "B", "ns", "b", "b-uid", "1", &metav1.OwnerReference{
		APIVersion: "example.com/v1", Kind: "A", Name: "a", UID: "a-uid", Controller: boolPtr(true),
	})
	c := fake.NewClientBuilder().WithObjects(a, b).Build()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns", Name: "cyclic",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "example.com/v1", Kind: "A", Name: "a", UID: "a-uid", Controller: boolPtr(true),
			}},
		},
	}
	_, _, err := resolveUltimateOwner(context.Background(), c, pod)
	require.Error(t, err)
}
