package ingress

import (
	"context"
	"github.com/wso2/k8s-api-operator/api-operator/pkg/ingress"
	"github.com/wso2/k8s-api-operator/api-operator/pkg/ingress/class"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/api/networking/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_ingress")

// Add creates a new Ingress Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileIngress{
		client:   mgr.GetClient(),
		scheme:   mgr.GetScheme(),
		recorder: mgr.GetEventRecorderFor("ingress-controller"),
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("ingress-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource Ingress
	err = c.Watch(&source.Kind{Type: &v1beta1.Ingress{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	return nil
}

// blank assignment to verify that ReconcileIngress implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileIngress{}

// ReconcileIngress reconciles a Ingress object
type ReconcileIngress struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client   client.Client
	scheme   *runtime.Scheme
	recorder record.EventRecorder
}

// Reconcile reads that state of the cluster for a Ingress object and makes changes based on the state read
// and what is in the Ingress.Spec
func (r *ReconcileIngress) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	ctx := context.Background()
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling Ingress")

	// Fetch the Ingress instance
	instance := &v1beta1.Ingress{}
	if err := r.client.Get(ctx, request.NamespacedName, instance); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if !class.IsValid(instance) {
		reqLogger.Info("Ignore ingress based on ingress class")
		return reconcile.Result{}, nil
	}

	// TODO: (renuka) sample record
	r.recorder.Event(instance, corev1.EventTypeNormal, "SampleRecord", "Example record to test :)")

	ingList := &v1beta1.IngressList{}
	if err := r.client.List(ctx, ingList, client.InNamespace("default")); err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	// filter out ingresses managed by the microgateway ingress controller
	ingresses := make([]*v1beta1.Ingress, 0, len(ingList.Items))
	for i := range ingList.Items {
		if class.IsValid(&ingList.Items[i]) {
			ingresses = append(ingresses, &ingList.Items[i])
		}
	}

	ingress.SortIngressSlice(ingresses)

	// Set Ingress instance as the owner and controller
	//if err := controllerutil.SetControllerReference(instance, pod, r.scheme); err != nil {
	//	return reconcile.Result{}, err
	//}

	// Check if this Pod already exists

	// Pod already exists - don't requeue
	//reqLogger.Info("Skip reconcile: Pod already exists", "Pod.Namespace", found.Namespace, "Pod.Name", found.Name)
	return reconcile.Result{}, nil
}