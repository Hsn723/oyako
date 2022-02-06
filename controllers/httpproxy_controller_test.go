package controllers

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"golang.org/x/xerrors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
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

func parentHasExpectedInclude(ctx context.Context, namespace, name, childNamespace, childName, childPrefix string) error {
	parent := &contourv1.HTTPProxy{}
	key := client.ObjectKey{Namespace: namespace, Name: name}
	err := k8sClient.Get(ctx, key, parent)
	if err != nil {
		return err
	}

	if !hasInclude(parent, childNamespace, childName, childPrefix) {
		return xerrors.Errorf("child is not included in parent")
	}
	return nil
}

func hasInclude(parent *contourv1.HTTPProxy, namespace, name, prefix string) bool {
	for _, i := range parent.Spec.Includes {
		if i.Namespace == namespace && i.Name == name {
			for _, cond := range i.Conditions {
				if cond.Prefix == prefix {
					return true
				}
			}
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
	var stopFunc func()
	ctx := context.Background()
	BeforeEach(func() {
		k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:             scheme,
			LeaderElection:     false,
			MetricsBindAddress: "0",
		})
		Expect(err).NotTo(HaveOccurred())
		reconciler := &HTTPProxyReconciler{
			Client: k8sManager.GetClient(),
			Scheme: k8sManager.GetScheme(),
			Log:    ctrl.Log.WithName("controllers").WithName("HTTPProxy"),
		}
		Expect(reconciler.SetupWithManager(k8sManager)).To(Succeed())

		ctx, cancel := context.WithCancel(ctx)
		stopFunc = cancel
		go func() {
			err := k8sManager.Start(ctx)
			if err != nil {
				panic(err)
			}
		}()
		time.Sleep(100 * time.Millisecond)
	})

	AfterEach(func() {
		stopFunc()
		time.Sleep(100 * time.Millisecond)
	})

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
			time.Sleep(time.Second)
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).Should(Succeed())
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
			time.Sleep(time.Second)
			prefix := fmt.Sprintf("/%s", childName)
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).Should(Succeed())
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
			time.Sleep(time.Second)
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).Should(Succeed())

			By("updating prefix")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: childNamespace,
				Name:      childName,
			}, child)).To(Succeed())
			newPrefix := fmt.Sprintf("/%s", randomSuffix())
			child.Annotations[pathPrefixAnnotation] = newPrefix
			Expect(k8sClient.Update(ctx, child)).To(Succeed())

			By("getting parent")
			time.Sleep(time.Second)
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, newPrefix)
			}).Should(Succeed())
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
			time.Sleep(time.Second)
			Expect(parentHasExpectedInclude(ctx, parentNamespace, parentName, "hoge", "hoge", "/hoge"))
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).Should(Succeed())
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
			time.Sleep(time.Second)
			Expect(parentHasExpectedInclude(ctx, parentNamespace, parentName, "hoge", "hoge", prefix))
			Consistently(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).ShouldNot(Succeed())
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
			time.Sleep(time.Second)
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
			time.Sleep(time.Second)
			Consistently(func() error {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Namespace: parentNamespace,
					Name:      parentName,
				}, parent)
				Expect(err).NotTo(HaveOccurred())
				if parent.Spec.Includes == nil {
					return nil
				}
				return xerrors.Errorf("parent should not have any includes")
			}).Should(Succeed())
		})
	})

	Context("When deleting child HTTPProxy", func() {
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
			time.Sleep(time.Second)
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).Should(Succeed())

			By("deleting child")
			Expect(k8sClient.Get(ctx, client.ObjectKey{
				Namespace: childNamespace,
				Name:      childName,
			}, child)).To(Succeed())
			Expect(k8sClient.Delete(ctx, child)).To(Succeed())

			By("getting parent")
			time.Sleep(time.Second)
			Eventually(func() error {
				return parentHasExpectedInclude(ctx, parentNamespace, parentName, childNamespace, childName, prefix)
			}).ShouldNot(Succeed())
			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{
					Namespace: childNamespace,
					Name:      childName,
				}, child)
			}).ShouldNot(Succeed())
		})
	})
})
