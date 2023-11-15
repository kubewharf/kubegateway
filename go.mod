module github.com/kubewharf/kubegateway

go 1.16

require (
	github.com/go-openapi/spec v0.19.3
	github.com/gobeam/stringy v0.0.5
	github.com/gogo/protobuf v1.3.2
	github.com/kubewharf/apiserver-runtime v0.0.0
	github.com/libp2p/go-reuseport v0.2.0
	github.com/pires/go-proxyproto v0.6.2
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.0.0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	github.com/zoumo/golib v0.0.0-20211216092524-c9bb48ad7bef
	github.com/zoumo/goset v0.2.0
	golang.org/x/net v0.0.0-20211101194204-95aca89e93de
	k8s.io/api v0.18.10
	k8s.io/apiextensions-apiserver v0.18.10
	k8s.io/apimachinery v0.18.19
	k8s.io/apiserver v0.18.10
	k8s.io/client-go v0.18.10
	k8s.io/component-base v0.18.10
	k8s.io/klog v1.0.0
	k8s.io/kube-aggregator v0.18.10
	k8s.io/kube-openapi v0.0.0-20200410145947-61e04a5be9a6
	k8s.io/kubernetes v1.18.10
	sigs.k8s.io/controller-runtime v0.6.0
)

replace (
	github.com/go-logr/logr => github.com/go-logr/logr v0.1.0
	github.com/go-logr/zapr => github.com/go-logr/zapr v0.1.0
	github.com/golang/groupcache => github.com/golang/groupcache v0.0.0-20160516000752-02826c3e7903
	github.com/golang/protobuf => github.com/golang/protobuf v1.3.2
	github.com/google/go-cmp => github.com/google/go-cmp v0.3.0
	github.com/googleapis/gnostic => github.com/googleapis/gnostic v0.1.0
	github.com/gorilla/websocket => github.com/gorilla/websocket v1.4.0
	github.com/grpc-ecosystem/go-grpc-middleware => github.com/grpc-ecosystem/go-grpc-middleware v1.0.1-0.20190118093823-f849b5445de4
	github.com/imdario/mergo => github.com/imdario/mergo v0.3.5
	github.com/konsorten/go-windows-terminal-sequences => github.com/konsorten/go-windows-terminal-sequences v1.0.1
	github.com/kubernetes-incubator/reference-docs => github.com/kubernetes-sigs/reference-docs v0.0.0-20170929004150-fcf65347b256
	github.com/kubewharf/apiserver-runtime => ./staging/src/github.com/kubewharf/apiserver-runtime
	github.com/markbates/inflect => github.com/markbates/inflect v1.0.4
	github.com/onsi/gomega => github.com/onsi/gomega v1.7.0
	github.com/pkg/errors => github.com/pkg/errors v0.9.1
	github.com/tmc/grpc-websocket-proxy => github.com/tmc/grpc-websocket-proxy v0.0.0-20170815181823-89b8d40f7ca8
	go.uber.org/atomic => go.uber.org/atomic v1.3.2
	golang.org/x/sys => golang.org/x/sys v0.0.0-20190813064441-fde4db37ae7a
	golang.org/x/tools => golang.org/x/tools v0.0.0-20190821162956-65e3620a7ae7
	golang.org/x/xerrors => golang.org/x/xerrors v0.0.0-20190717185122-a985d3407aa7
	gopkg.in/check.v1 => gopkg.in/check.v1 v1.0.0-20180628173108-788fd7840127
	k8s.io/api => k8s.io/api v0.18.10
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.10
	k8s.io/apimachinery => k8s.io/apimachinery v0.18.19
	k8s.io/apiserver => github.com/kubewharf/apiserver v0.0.0-20230515081716-5cd2041a3c4d
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.18.10
	k8s.io/client-go => k8s.io/client-go v0.18.10
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.18.10
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.18.10
	k8s.io/code-generator => k8s.io/code-generator v0.18.18-rc.0
	k8s.io/component-base => k8s.io/component-base v0.18.10
	k8s.io/cri-api => k8s.io/cri-api v0.18.18-rc.0
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.18.10
	k8s.io/gengo => k8s.io/gengo v0.0.0-20200114144118-36b2048a9120
	k8s.io/heapster => k8s.io/heapster v1.2.0-beta.1
	k8s.io/klog => k8s.io/klog v1.0.0
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.18.10
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.18.10
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20200410145947-61e04a5be9a6
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.18.10
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.18.10
	k8s.io/kubectl => k8s.io/kubectl v0.18.10
	k8s.io/kubelet => k8s.io/kubelet v0.18.10
	k8s.io/kubernetes => k8s.io/kubernetes v1.18.10
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.18.10
	k8s.io/metrics => k8s.io/metrics v0.18.10
	k8s.io/repo-infra => k8s.io/repo-infra v0.0.1-alpha.1
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.18.10
	k8s.io/system-validators => k8s.io/system-validators v1.0.4
	k8s.io/utils => k8s.io/utils v0.0.0-20200324210504-a9aa75ae1b89
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.6.0
)
