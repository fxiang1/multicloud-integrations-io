/*
Copyright 2022.

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

package application

import (
	"context"
	"strconv"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	corev1 "k8s.io/api/core/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

var _ = Describe("Application Pull controller", func() {

	const (
		appName          = "app-1"
		appName2         = "app-2"
		appName3         = "app-3"
		appNamespace     = "default"
		clusterName      = "cluster1"
		localClusterName = "local-cluster"
	)

	appKey := types.NamespacedName{Name: appName, Namespace: appNamespace}
	appKey2 := types.NamespacedName{Name: appName2, Namespace: appNamespace}
	appKey3 := types.NamespacedName{Name: appName3, Namespace: appNamespace}
	ctx := context.Background()

	Context("When Application without OCM pull label is created", func() {
		It("Should not create ManifestWork", func() {
			By("Creating the Application without OCM pull label")
			app1 := &unstructured.Unstructured{}
			app1.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app1.SetName(appName)
			app1.SetNamespace(appNamespace)
			app1.SetAnnotations(map[string]string{AnnotationKeyOCMManagedCluster: clusterName})
			_ = unstructured.SetNestedField(app1.Object, "default", "spec", "project")
			_ = unstructured.SetNestedField(app1.Object, "https://github.com/argoproj/argocd-example-apps.git", "spec", "source", "repoURL")
			_ = unstructured.SetNestedMap(app1.Object, map[string]interface{}{
				"server":    KubernetesInternalAPIServerAddr,
				"namespace": appNamespace,
			}, "spec", "destination")
			Expect(k8sClient.Create(ctx, app1)).Should(Succeed())
			Expect(k8sClient.Get(ctx, appKey, app1)).Should(Succeed())

			mwKey := types.NamespacedName{Name: generateManifestWorkName(app1.GetName(), app1.GetUID()), Namespace: clusterName}
			mw := workv1.ManifestWork{}
			Consistently(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return false
				}
				return true
			}).Should(BeFalse())
		})
	})

	Context("When Application with OCM pull label is created/updated/deleted", func() {
		It("Should create/update/delete ManifestWork", func() {
			By("Creating the OCM ManagedCluster")
			managedCluster := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &managedCluster)).Should(Succeed())

			By("Creating the OCM ManagedCluster namespace")
			managedClusterNs := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: clusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &managedClusterNs)).Should(Succeed())

			By("Creating the Application with OCM pull label")
			app2 := &unstructured.Unstructured{}
			app2.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app2.SetName(appName2)
			app2.SetNamespace(appNamespace)
			app2.SetLabels(map[string]string{LabelKeyPull: strconv.FormatBool(true)})
			app2.SetAnnotations(map[string]string{AnnotationKeyOCMManagedCluster: clusterName})
			app2.SetFinalizers([]string{ResourcesFinalizerName})
			_ = unstructured.SetNestedField(app2.Object, "default", "spec", "project")
			_ = unstructured.SetNestedField(app2.Object, "https://github.com/argoproj/argocd-example-apps.git", "spec", "source", "repoURL")
			_ = unstructured.SetNestedMap(app2.Object, map[string]interface{}{
				"server":    KubernetesInternalAPIServerAddr,
				"namespace": appNamespace,
			}, "spec", "destination")
			Expect(k8sClient.Create(ctx, app2)).Should(Succeed())
			Expect(k8sClient.Get(ctx, appKey2, app2)).Should(Succeed())

			mwKey := types.NamespacedName{Name: generateManifestWorkName(app2.GetName(), app2.GetUID()), Namespace: clusterName}
			mw := workv1.ManifestWork{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return false
				}
				return true
			}).Should(BeTrue())

			By("Updating the Application")
			spec := map[string]interface{}{
				"project": "somethingelse",
				"source":  map[string]interface{}{"repoURL": "dummy"},
				"destination": map[string]interface{}{
					"server":    KubernetesInternalAPIServerAddr,
					"namespace": appNamespace,
				},
			}
			Expect(unstructured.SetNestedField(app2.Object, spec, "spec")).Should(Succeed())
			Expect(k8sClient.Update(ctx, app2)).Should(Succeed())
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return false
				}
				if len(mw.Spec.Workload.Manifests) == 0 {
					return false
				}
				if mw.Spec.Workload.Manifests[0].RawExtension.Raw == nil {
					return false
				}
				return strings.Contains(string(mw.Spec.Workload.Manifests[0].RawExtension.Raw), "somethingelse")
			}).Should(BeTrue())

			By("Updating the Application with operation")
			operation := map[string]interface{}{
				"info": []interface{}{
					map[string]interface{}{
						"name":  "Reason",
						"value": "ApplicationSet RollingSync triggered a sync of this Application resource.",
					},
				},
				"initiatedBy": map[string]interface{}{
					"automated": true,
					"username":  "applicationset-controller",
				},
				"retry": map[string]interface{}{},
				"sync": map[string]interface{}{
					"syncOptions": []interface{}{
						"CreateNamespace=true",
					},
				},
			}
			Expect(unstructured.SetNestedField(app2.Object, operation, "operation")).Should(Succeed())
			Expect(k8sClient.Update(ctx, app2)).Should(Succeed())
			Eventually(func() bool {
				updatedApp := &unstructured.Unstructured{}
				updatedApp.SetGroupVersionKind(schema.GroupVersionKind{
					Group:   "argoproj.io",
					Version: "v1alpha1",
					Kind:    "Application",
				})
				err := k8sClient.Get(ctx, appKey2, updatedApp)
				if err != nil {
					return false
				}
				_, ok := updatedApp.Object["operation"]
				return !ok
			}).Should(BeTrue())

			By("Deleting the Application")
			Expect(k8sClient.Get(ctx, appKey2, app2)).Should(Succeed())
			Expect(k8sClient.Delete(ctx, app2)).Should(Succeed())
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return true
				}
				return false
			}).Should(BeTrue())
		})
	})

	Context("When Application with OCM pull label is created for local-cluster", func() {
		It("Should not create ManifestWork", func() {
			By("Creating the OCM ManagedCluster")
			managedCluster := clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:   localClusterName,
					Labels: map[string]string{"local-cluster": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, &managedCluster)).Should(Succeed())

			By("Creating the OCM ManagedCluster namespace")
			managedClusterNs := corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: localClusterName,
				},
			}
			Expect(k8sClient.Create(ctx, &managedClusterNs)).Should(Succeed())

			By("Creating the Application with OCM pull label and local-cluster")
			app3 := &unstructured.Unstructured{}
			app3.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   "argoproj.io",
				Version: "v1alpha1",
				Kind:    "Application",
			})
			app3.SetName(appName3)
			app3.SetNamespace(appNamespace)
			app3.SetLabels(map[string]string{LabelKeyPull: strconv.FormatBool(true)})
			app3.SetAnnotations(map[string]string{AnnotationKeyOCMManagedCluster: localClusterName})
			app3.SetFinalizers([]string{ResourcesFinalizerName})
			_ = unstructured.SetNestedField(app3.Object, "default", "spec", "project")
			_ = unstructured.SetNestedField(app3.Object, "https://github.com/argoproj/argocd-example-apps.git", "spec", "source", "repoURL")
			_ = unstructured.SetNestedMap(app3.Object, map[string]interface{}{
				"server":    KubernetesInternalAPIServerAddr,
				"namespace": appNamespace,
			}, "spec", "destination")
			Expect(k8sClient.Create(ctx, app3)).Should(Succeed())
			Expect(k8sClient.Get(ctx, appKey3, app3)).Should(Succeed())

			mwKey := types.NamespacedName{Name: generateManifestWorkName(app3.GetName(), app3.GetUID()), Namespace: clusterName}
			mw := workv1.ManifestWork{}
			Consistently(func() bool {
				if err := k8sClient.Get(ctx, mwKey, &mw); err != nil {
					return true
				}
				return false
			}).Should(BeTrue())
		})
	})
})
