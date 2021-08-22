package controllers

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TestParentNamespacePrefix = "parent"
	TestChildNamespacePrefix  = "child"
)

func parentProxyFromTemplate(namespace, name string) *contourv1.HTTPProxy {
	parent := &contourv1.HTTPProxy{
		ObjectMeta: v1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				allowInclusionAnnotation: "true",
			},
		},
		Spec: contourv1.HTTPProxySpec{
			VirtualHost: &contourv1.VirtualHost{
				Fqdn: "example.com",
			},
		},
	}
	return parent
}

func childProxyFromTemplate(namespace, name, parentNamespacedName, prefix string) *contourv1.HTTPProxy {
	child := &contourv1.HTTPProxy{
		ObjectMeta: v1.ObjectMeta{
			Namespace: namespace,
			Name:      name,
			Annotations: map[string]string{
				parentRefAnnotation: parentNamespacedName,
			},
		},
		Spec: contourv1.HTTPProxySpec{
			Routes: []contourv1.Route{
				{
					Services: []contourv1.Service{
						{
							Name: name,
							Port: 80,
						},
					},
				},
			},
		},
	}
	if prefix != "" {
		child.ObjectMeta.Annotations[pathPrefixAnnotation] = prefix
	}
	return child
}

func parentHasExpectedInclude(ctx context.Context, parent *contourv1.HTTPProxy, expectedInclude contourv1.Include, namespace, name string) error {
	err := k8sClient.Get(ctx, client.ObjectKey{
		Namespace: namespace,
		Name:      name,
	}, parent)
	Expect(err).NotTo(HaveOccurred())

	if !hasInclude(parent.Spec.Includes, expectedInclude) {
		return fmt.Errorf("child is not included in parent")
	}
	return nil
}

func hasInclude(includes []contourv1.Include, include contourv1.Include) bool {
	for _, i := range includes {
		if reflect.DeepEqual(i, include) {
			return true
		}
	}
	return false
}

func randomSuffix() string {
	n, err := rand.Int(rand.Reader, big.NewInt(99999))
	Expect(err).NotTo(HaveOccurred())
	return n.String()
}

func randomNames() (parentNamespace, parentName, childNamespace, childName, prefix string) {
	parentNamespace = fmt.Sprintf("%s-%s", TestParentNamespacePrefix, randomSuffix())
	parentName = fmt.Sprintf("%s-%s", TestParentNamespacePrefix, randomSuffix())
	childNamespace = fmt.Sprintf("%s-%s", TestChildNamespacePrefix, randomSuffix())
	childName = fmt.Sprintf("%s-%s", TestChildNamespacePrefix, randomSuffix())
	prefix = fmt.Sprintf("/%s", randomSuffix())
	return
}

var _ = Describe("HTTPProxy controller", func() {

	ctx := context.Background()
	Context("When creating child HTTPProxy", func() {
		It("Should update the parent HTTPProxy", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			expectedInclude := contourv1.Include{
				Namespace: child.Namespace,
				Name:      child.Name,
				Conditions: []contourv1.MatchCondition{
					{
						Prefix: prefix,
					},
				},
			}
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).Should(Succeed())
		})

		It("Should update the parent HTTPProxy with default prefix", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, _ := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), "")
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			expectedInclude := contourv1.Include{
				Namespace: child.Namespace,
				Name:      child.Name,
				Conditions: []contourv1.MatchCondition{
					{
						Prefix: fmt.Sprintf("/%s", childName),
					},
				},
			}
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).Should(Succeed())
		})

		It("Should update the parent's existing include", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			expectedInclude := contourv1.Include{
				Namespace: child.Namespace,
				Name:      child.Name,
				Conditions: []contourv1.MatchCondition{
					{
						Prefix: prefix,
					},
				},
			}
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).Should(Succeed())

			By("updating prefix")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: childNamespace,
				Name:      childName,
			}, child)).To(Succeed())
			newPrefix := fmt.Sprintf("/%s", randomSuffix())
			child.Annotations[pathPrefixAnnotation] = newPrefix
			Expect(k8sClient.Update(ctx, child)).To(Succeed())
			expectedInclude.Conditions = []contourv1.MatchCondition{
				{
					Prefix: newPrefix,
				},
			}
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).Should(Succeed())
		})

		It("Should not overwrite the parent's other includes", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			includes := []contourv1.Include{
				{
					Namespace: "hoge",
					Name:      "hoge",
					Conditions: []contourv1.MatchCondition{
						{
							Prefix: "/hoge",
						},
					},
				},
			}
			parent.Spec.Includes = includes
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			expectedInclude := contourv1.Include{
				Namespace: child.Namespace,
				Name:      child.Name,
				Conditions: []contourv1.MatchCondition{
					{
						Prefix: prefix,
					},
				},
			}
			Expect(parentHasExpectedInclude(ctx, parent, includes[0], parentNamespace, parentName))
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).Should(Succeed())
		})

		It("Should not allow duplicate prefixes", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			includes := []contourv1.Include{
				{
					Namespace: "hoge",
					Name:      "hoge",
					Conditions: []contourv1.MatchCondition{
						{
							Prefix: prefix,
						},
					},
				},
			}
			parent.Spec.Includes = includes
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child with duplicate prefix")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			expectedInclude := contourv1.Include{
				Namespace: child.Namespace,
				Name:      child.Name,
				Conditions: []contourv1.MatchCondition{
					{
						Prefix: prefix,
					},
				},
			}
			Expect(parentHasExpectedInclude(ctx, parent, includes[0], parentNamespace, parentName))
			Consistently(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).ShouldNot(Succeed())
		})

		It("Should cleanup parent upon child's deletion", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			expectedInclude := contourv1.Include{
				Namespace: child.Namespace,
				Name:      child.Name,
				Conditions: []contourv1.MatchCondition{
					{
						Prefix: prefix,
					},
				},
			}
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).Should(Succeed())

			By("deleting child")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: childNamespace,
				Name:      childName,
			}, child)).To(Succeed())
			Expect(k8sClient.Delete(ctx, child)).To(Succeed())
			Eventually(parentHasExpectedInclude(ctx, parent, expectedInclude, parentNamespace, parentName)).ShouldNot(Succeed())
		})

		It("Should not update non-existing parent", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			Consistently(func() error {
				parent := &contourv1.HTTPProxy{}
				return k8sClient.Get(ctx, client.ObjectKey{
					Namespace: parentNamespace,
					Name:      parentName,
				}, parent)
			}).ShouldNot(Succeed())
		})

		It("Should not update parent when inclusion is not allowed", func() {
			By("creating namespaces")
			parentNamespace, parentName, childNamespace, childName, prefix := randomNames()

			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: parentNamespace},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: v1.ObjectMeta{Name: childNamespace},
			})).To(Succeed())

			By("creating parent without annotation")
			parent := parentProxyFromTemplate(parentNamespace, parentName)
			parent.Annotations = map[string]string{}
			Expect(k8sClient.Create(ctx, parent)).To(Succeed())

			By("creating child")
			child := childProxyFromTemplate(childNamespace, childName, fmt.Sprintf("%s/%s", parentNamespace, parentName), prefix)
			Expect(k8sClient.Create(ctx, child)).To(Succeed())

			By("getting parent")
			Consistently(func() error {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Namespace: parentNamespace,
					Name:      parentName,
				}, parent)
				Expect(err).NotTo(HaveOccurred())
				if parent.Spec.Includes == nil {
					return nil
				}
				return fmt.Errorf("parent should not have any includes")
			}).Should(Succeed())
		})
	})
})
