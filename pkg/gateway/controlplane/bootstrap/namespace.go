package bootstrap

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	genericapiserver "k8s.io/apiserver/pkg/server"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	namespaceHookName = "namespace/bootstrap-kube-gateway-namespaces"
)

var (
	systemNamespaces = []string{"kube-gateway"}
)

func init() {
	AddPostStartHook(namespacePostStartHook())
}

func namespacePostStartHook() (string, genericapiserver.PostStartHookFunc) {
	return namespaceHookName, func(hookContext genericapiserver.PostStartHookContext) error {
		err := wait.Poll(1*time.Second, 30*time.Second, func() (done bool, err error) {
			coreclientset, err := corev1client.NewForConfig(hookContext.LoopbackClientConfig)
			if err != nil {
				utilruntime.HandleError(fmt.Errorf("unable to initialize client: %v", err))
				return false, nil
			}
			for _, ns := range systemNamespaces {
				if err := createNamespaceIfNeeded(coreclientset, ns); err != nil {
					utilruntime.HandleError(fmt.Errorf("unable to create required kube-gataway system namespace %s: %v", ns, err))
				}
			}

			return true, nil
		})
		// if we're never able to make it through initialization, kill the API server
		if err != nil {
			return fmt.Errorf("unable to initialize kube-gateway namespaces: %v", err)
		}
		return nil
	}
}

func createNamespaceIfNeeded(c corev1client.NamespacesGetter, ns string) error {
	if _, err := c.Namespaces().Get(context.TODO(), ns, metav1.GetOptions{}); err == nil {
		// the namespace already exists
		return nil
	}
	newNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ns,
			Namespace: "",
		},
	}
	_, err := c.Namespaces().Create(context.TODO(), newNs, metav1.CreateOptions{})
	if err != nil && errors.IsAlreadyExists(err) {
		err = nil
	}
	return err
}
