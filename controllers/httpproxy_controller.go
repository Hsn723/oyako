/*
Copyright 2021.

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

package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	contourv1 "github.com/projectcontour/contour/apis/projectcontour/v1"
	"golang.org/x/xerrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	allowInclusionAnnotation = "oyako.atelierhsn.com/allow-inclusion"
	parentRefAnnotation      = "oyako.atelierhsn.com/parent"
	pathPrefixAnnotation     = "oyako.atelierhsn.com/prefix"
	finalizerName            = "oyako.atelierhsn.com/finalizer"
)

// HTTPProxyReconciler reconciles a HTTPProxy object.
type HTTPProxyReconciler struct {
	Client client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=projectcontour.io,resources=httpproxies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=projectcontour.io,resources=httpproxies/status,verbs=get
// +kubebuilder:rbac:groups=projectcontour.io,resources=httpproxies/finalizers,verbs=update

// Reconcile updates parent HTTPProxy objects.
func (r *HTTPProxyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("httpproxy", req.NamespacedName)

	httpProxy := &contourv1.HTTPProxy{}
	key := client.ObjectKey{
		Namespace: req.Namespace,
		Name:      req.Name,
	}
	err := r.Client.Get(ctx, key, httpProxy)
	if apierrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		log.Error(err, "unable to get HTTPProxy")
		return ctrl.Result{}, err
	}
	if httpProxy.Annotations[parentRefAnnotation] == "" {
		return ctrl.Result{}, nil
	}
	if httpProxy.ObjectMeta.DeletionTimestamp.IsZero() {
		if !r.hasFinalizer(httpProxy, finalizerName) {
			controllerutil.AddFinalizer(httpProxy, finalizerName)
			if err := r.Client.Update(ctx, httpProxy); err != nil {
				return ctrl.Result{}, err
			}
		}
	} else {
		if r.hasFinalizer(httpProxy, finalizerName) {
			if err := r.cleanupParentProxy(ctx, httpProxy, log); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(httpProxy, finalizerName)
			if err := r.Client.Update(ctx, httpProxy); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	stop, err := r.reconcileParentProxy(ctx, httpProxy, log)
	if err != nil {
		log.Error(err, "failed to reconcile HTTPProxy")
	}
	if stop {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, err
}

func (r *HTTPProxyReconciler) hasFinalizer(h *contourv1.HTTPProxy, finalizer string) bool {
	for _, f := range h.GetFinalizers() {
		if f == finalizer {
			return true
		}
	}
	return false
}

func (r *HTTPProxyReconciler) getParentProxy(ctx context.Context, parentRef string) (parent *contourv1.HTTPProxy, err error) {
	namespacedName := strings.Split(parentRef, "/")
	if len(namespacedName) != 2 {
		return nil, xerrors.Errorf("invalid parent %s", namespacedName)
	}
	key := client.ObjectKey{
		Namespace: namespacedName[0],
		Name:      namespacedName[1],
	}
	parent = &contourv1.HTTPProxy{}
	err = r.Client.Get(ctx, key, parent)
	return
}

func (r *HTTPProxyReconciler) isPrefixDuplicate(includes []contourv1.Include, childMeta v1.ObjectMeta, prefix string) bool {
	for _, include := range includes {
		if include.Namespace == childMeta.Namespace && include.Name == childMeta.Name {
			continue
		}
		for _, condition := range include.Conditions {
			if condition.Prefix == prefix {
				return true
			}
		}
	}
	return false
}

func (r *HTTPProxyReconciler) findIncludeRef(includes []contourv1.Include, childMeta v1.ObjectMeta) int {
	for idx, include := range includes {
		if include.Namespace == childMeta.Namespace && include.Name == childMeta.Name {
			return idx
		}
	}
	return -1
}

func (r *HTTPProxyReconciler) cleanupParentProxy(ctx context.Context, childProxy *contourv1.HTTPProxy, log logr.Logger) error {
	parentRef := childProxy.Annotations[parentRefAnnotation]
	parentProxy, err := r.getParentProxy(ctx, parentRef)
	if err != nil {
		return err
	}
	if parentProxy.Annotations[allowInclusionAnnotation] != "true" {
		return xerrors.Errorf("parent %s does not allow child inclusions", parentRef)
	}
	includes := parentProxy.Spec.Includes
	childIdx := r.findIncludeRef(includes, childProxy.ObjectMeta)
	if childIdx < 0 {
		return nil
	}
	includes = append(includes[:childIdx], includes[childIdx+1:]...)
	parentProxy.Spec.Includes = includes
	err = r.Client.Update(ctx, parentProxy, &client.UpdateOptions{
		FieldManager: "oyako",
	})
	if err != nil {
		return err
	}
	log.Info("cleaned up parent HTTPProxy")
	return nil
}

func (r *HTTPProxyReconciler) reconcileParentProxy(ctx context.Context, childProxy *contourv1.HTTPProxy, log logr.Logger) (bool, error) {
	parentRef := childProxy.Annotations[parentRefAnnotation]
	parentProxy, err := r.getParentProxy(ctx, parentRef)
	if err != nil {
		return false, err
	}
	if parentProxy.Annotations[allowInclusionAnnotation] != "true" {
		return true, xerrors.Errorf("parent %s does not allow child inclusions", parentRef)
	}
	prefix := childProxy.Annotations[pathPrefixAnnotation]
	if prefix == "" {
		prefix = fmt.Sprintf("/%s", childProxy.Name)
	}
	includes := parentProxy.Spec.Includes
	if r.isPrefixDuplicate(includes, childProxy.ObjectMeta, prefix) {
		return true, xerrors.Errorf("duplicate prefix")
	}
	prefixCondition := []contourv1.MatchCondition{
		{
			Prefix: prefix,
		},
	}

	childIdx := r.findIncludeRef(includes, childProxy.ObjectMeta)
	if childIdx >= 0 {
		parentProxy.Spec.Includes[childIdx].Conditions = prefixCondition
	} else {
		include := contourv1.Include{
			Namespace:  childProxy.Namespace,
			Name:       childProxy.Name,
			Conditions: prefixCondition,
		}
		parentProxy.Spec.Includes = append(parentProxy.Spec.Includes, include)
	}
	err = r.Client.Update(ctx, parentProxy, &client.UpdateOptions{
		FieldManager: "oyako",
	})
	if err != nil {
		return false, err
	}
	log.Info("HTTPProxy parent reconciled")
	return true, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HTTPProxyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&contourv1.HTTPProxy{}).
		Complete(r)
}
