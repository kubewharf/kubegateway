package scheme

import (
	"github.com/kubewharf/apiserver-runtime/pkg/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	gatewayinstall "github.com/kubewharf/kubegateway/pkg/apis/install"
	"github.com/kubewharf/kubegateway/pkg/apis/proxy/v1alpha1"
)

var (
	Codecs = scheme.Codecs.LegacyCodec(schema.GroupVersion{
		Group:   v1alpha1.GroupVersion.Group,
		Version: v1alpha1.GroupVersion.Version,
	})
)

func init() {
	metav1.AddToGroupVersion(scheme.Scheme, schema.GroupVersion{Version: "v1"})
	gatewayinstall.Install(scheme.Scheme)
}
