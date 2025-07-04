package client

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/client-go/util/retry"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"

	kerrors "k8s.io/apimachinery/pkg/api/errors"

	"github.com/openshift/zero-trust-workload-identity-manager/api/v1alpha1"
	"github.com/openshift/zero-trust-workload-identity-manager/pkg/controller/utils"
)

var (
	// cacheResources is the list of resources that the controller watches,
	// and creates informers for.
	cacheResources = []client.Object{
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&rbacv1.ClusterRole{},
		&rbacv1.ClusterRoleBinding{},
		&storagev1.CSIDriver{},
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&corev1.ConfigMap{},
		&appsv1.Deployment{},
		&appsv1.DaemonSet{},
		&appsv1.StatefulSet{},
		&admissionregistrationv1.ValidatingWebhookConfiguration{},
	}

	cacheResourceWithoutReqSelectors = []client.Object{
		&v1alpha1.ZeroTrustWorkloadIdentityManager{},
		&v1alpha1.SpireAgent{},
		&v1alpha1.SpiffeCSIDriver{},
		&v1alpha1.SpireServer{},
		&v1alpha1.SpireOIDCDiscoveryProvider{},
	}

	informerResources = []client.Object{
		&corev1.ServiceAccount{},
		&corev1.Service{},
		&rbacv1.Role{},
		&rbacv1.RoleBinding{},
		&rbacv1.ClusterRole{},
		&rbacv1.ClusterRoleBinding{},
		&storagev1.CSIDriver{},
		&corev1.ConfigMap{},
		&appsv1.Deployment{},
		&appsv1.DaemonSet{},
		&appsv1.StatefulSet{},
		&admissionregistrationv1.ValidatingWebhookConfiguration{},
		&v1alpha1.ZeroTrustWorkloadIdentityManager{},
		&v1alpha1.SpireAgent{},
		&v1alpha1.SpiffeCSIDriver{},
		&v1alpha1.SpireServer{},
		&v1alpha1.SpireOIDCDiscoveryProvider{},
	}
)

type customCtrlClientImpl struct {
	client.Client
	apiReader client.Reader
}

//go:generate go run github.com/maxbrunsfeld/counterfeiter/v6 -generate
//counterfeiter:generate -o fakes . CustomCtrlClient
type CustomCtrlClient interface {
	Get(context.Context, client.ObjectKey, client.Object) error
	List(context.Context, client.ObjectList, ...client.ListOption) error
	StatusUpdate(context.Context, client.Object, ...client.SubResourceUpdateOption) error
	Update(context.Context, client.Object, ...client.UpdateOption) error
	UpdateWithRetry(context.Context, client.Object, ...client.UpdateOption) error
	Create(context.Context, client.Object, ...client.CreateOption) error
	Delete(context.Context, client.Object, ...client.DeleteOption) error
	Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error
	Exists(context.Context, client.ObjectKey, client.Object) (bool, error)
	CreateOrUpdateObject(ctx context.Context, obj client.Object) error
	StatusUpdateWithRetry(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error
}

func NewCustomClient(m manager.Manager) (CustomCtrlClient, error) {
	c, err := BuildCustomClient(m)
	if err != nil {
		return nil, fmt.Errorf("failed to build custom client: %w", err)
	}
	return &customCtrlClientImpl{
		Client:    c,
		apiReader: m.GetAPIReader(),
	}, nil
}

func (c *customCtrlClientImpl) Get(
	ctx context.Context, key client.ObjectKey, obj client.Object,
) error {
	err := c.Client.Get(ctx, key, obj)
	if err != nil && kerrors.IsNotFound(err) {
		return c.apiReader.Get(ctx, key, obj)
	}
	return err
}

func (c *customCtrlClientImpl) List(
	ctx context.Context, list client.ObjectList, opts ...client.ListOption,
) error {
	return c.Client.List(ctx, list, opts...)
}

func (c *customCtrlClientImpl) Create(
	ctx context.Context, obj client.Object, opts ...client.CreateOption,
) error {
	return c.Client.Create(ctx, obj, opts...)
}

func (c *customCtrlClientImpl) Delete(
	ctx context.Context, obj client.Object, opts ...client.DeleteOption,
) error {
	return c.Client.Delete(ctx, obj, opts...)
}

func (c *customCtrlClientImpl) Update(
	ctx context.Context, obj client.Object, opts ...client.UpdateOption,
) error {
	return c.Client.Update(ctx, obj, opts...)
}

func (c *customCtrlClientImpl) UpdateWithRetry(
	ctx context.Context, obj client.Object, opts ...client.UpdateOption,
) error {
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
		if err := c.Client.Get(ctx, key, current); err != nil {
			return fmt.Errorf("failed to fetch latest %q for update: %w", key, err)
		}
		obj.SetResourceVersion(current.GetResourceVersion())
		if err := c.Client.Update(ctx, obj, opts...); err != nil {
			return fmt.Errorf("failed to update %q resource: %w", key, err)
		}
		return nil
	}); err != nil {
		return err
	}

	return nil
}

func (c *customCtrlClientImpl) StatusUpdateWithRetry(
	ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption,
) error {
	key := types.NamespacedName{Name: obj.GetName(), Namespace: obj.GetNamespace()}
	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current := reflect.New(reflect.TypeOf(obj).Elem()).Interface().(client.Object)
		if err := c.Client.Get(ctx, key, current); err != nil {
			return fmt.Errorf("failed to fetch latest %q for update: %w", key, err)
		}
		obj.SetResourceVersion(current.GetResourceVersion())
		if err := c.Client.Status().Update(ctx, obj, opts...); err != nil {
			return fmt.Errorf("failed to update %q resource: %w", key, err)
		}
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (c *customCtrlClientImpl) StatusUpdate(
	ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption,
) error {
	return c.Client.Status().Update(ctx, obj, opts...)
}

func (c *customCtrlClientImpl) Patch(
	ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption,
) error {
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func (c *customCtrlClientImpl) Exists(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
	if err := c.Client.Get(ctx, key, obj); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateOrUpdateObject tries to create the object, updates if already exists
func (c *customCtrlClientImpl) CreateOrUpdateObject(ctx context.Context, obj client.Object) error {
	err := c.Create(ctx, obj)
	if err != nil && errors.IsAlreadyExists(err) {
		return c.Update(ctx, obj)
	}
	return err
}

func BuildCustomClient(mgr ctrl.Manager) (client.Client, error) {
	spireServerManagedResourceAppManagedReq, err := labels.NewRequirement(utils.AppManagedByLabelKey, selection.Equals, []string{utils.AppManagedByLabelValue})
	if err != nil {
		return nil, err
	}
	managedResourceLabelReqSelector := labels.NewSelector().Add(*spireServerManagedResourceAppManagedReq)
	customCacheObjects := map[client.Object]cache.ByObject{}
	for _, resource := range cacheResources {
		customCacheObjects[resource] = cache.ByObject{
			Label: managedResourceLabelReqSelector,
		}
	}
	for _, resource := range cacheResourceWithoutReqSelectors {
		customCacheObjects[resource] = cache.ByObject{}
	}
	customCacheOpts := cache.Options{
		HTTPClient:                  mgr.GetHTTPClient(),
		Scheme:                      mgr.GetScheme(),
		Mapper:                      mgr.GetRESTMapper(),
		ByObject:                    customCacheObjects,
		ReaderFailOnMissingInformer: true,
	}
	customCache, err := cache.New(mgr.GetConfig(), customCacheOpts)
	if err != nil {
		return nil, err
	}
	for _, resource := range informerResources {
		if _, err = customCache.GetInformer(context.Background(), resource); err != nil {
			return nil, err
		}
	}

	err = mgr.Add(customCache)
	if err != nil {
		return nil, err
	}

	customClient, err := client.New(mgr.GetConfig(), client.Options{
		HTTPClient: mgr.GetHTTPClient(),
		Scheme:     mgr.GetScheme(),
		Mapper:     mgr.GetRESTMapper(),
		Cache: &client.CacheOptions{
			Reader: customCache,
		},
	})
	if err != nil {
		return nil, err
	}
	return customClient, nil
}
