/*
Copyright 2021 SPIRE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package spireentry

import (
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const maxOwnerChainDepth = 10

type ownerInfo struct {
	APIVersion string
	Kind       string
	Name       string
	Namespace  string
	UID        string
}

// resolveUltimateOwner walks the ownerReferences chain starting from the pod
// up to the topmost controlling owner (e.g. Pod -> ReplicaSet -> Deployment).
// It returns the ultimate owner, a version token that changes whenever any
// object in the resolved chain changes, and an error.
//
// A nil ownerInfo with a nil error is returned for pods without any controlling
// owner reference.
func resolveUltimateOwner(ctx context.Context, c client.Client, pod *corev1.Pod) (*ownerInfo, string, error) {
	if c == nil {
		return nil, "", fmt.Errorf("no Kubernetes client available to resolve ultimate owner")
	}

	controller := controllerRef(pod.OwnerReferences)
	if controller == nil {
		return nil, "", nil
	}

	var tokenParts []string
	var current *ownerInfo
	namespace := pod.Namespace
	ref := controller

	for depth := 0; ; depth++ {
		if depth >= maxOwnerChainDepth {
			return nil, "", fmt.Errorf("owner reference chain exceeded maximum depth of %d", maxOwnerChainDepth)
		}

		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return nil, "", fmt.Errorf("invalid owner APIVersion %q: %w", ref.APIVersion, err)
		}

		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(gv.WithKind(ref.Kind))
		key := types.NamespacedName{Namespace: namespace, Name: ref.Name}
		if err := c.Get(ctx, key, obj); err != nil {
			return nil, "", fmt.Errorf("failed to get owner %s/%s %s: %w", gv.WithKind(ref.Kind).GroupKind(), namespace, ref.Name, err)
		}

		current = &ownerInfo{
			APIVersion: obj.GetAPIVersion(),
			Kind:       obj.GetKind(),
			Name:       obj.GetName(),
			Namespace:  obj.GetNamespace(),
			UID:        string(obj.GetUID()),
		}
		tokenParts = append(tokenParts, current.UID+"/"+obj.GetResourceVersion())

		next := controllerRef(obj.GetOwnerReferences())
		if next == nil {
			break
		}
		ref = next
		namespace = obj.GetNamespace()
	}

	return current, strings.Join(tokenParts, ","), nil
}

func controllerRef(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		if refs[i].Controller != nil && *refs[i].Controller {
			return &refs[i]
		}
	}
	return nil
}
