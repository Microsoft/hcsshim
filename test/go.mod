module github.com/Microsoft/hcsshim/test

go 1.16

require (
	github.com/Azure/azure-sdk-for-go v52.4.0+incompatible
	github.com/Azure/go-autorest/autorest v0.11.17
	github.com/Azure/go-autorest/autorest/azure/auth v0.5.7
	github.com/Microsoft/go-winio v0.4.17-0.20210211115548-6eac466e5fa3
	github.com/Microsoft/hcsshim v0.8.15
	github.com/containerd/containerd v1.5.0-beta.4
	github.com/containerd/go-runc v0.0.0-20201020171139-16b287bc67d0
	github.com/containerd/ttrpc v1.0.2
	github.com/containerd/typeurl v1.0.1
	github.com/gogo/protobuf v1.3.2
	github.com/opencontainers/runtime-spec v1.0.3-0.20200929063507-e6143ca7d51d
	github.com/opencontainers/runtime-tools v0.0.0-20181011054405-1d69bd0f9c39
	github.com/pkg/errors v0.9.1
	github.com/sirupsen/logrus v1.7.0
	golang.org/x/sync v0.0.0-20201207232520-09787c993a3a
	google.golang.org/grpc v1.33.2
	k8s.io/cri-api v0.20.1
	k8s.io/kubernetes v1.20.4
)

replace (
	github.com/Microsoft/hcsshim => ../
	// These are all required due to how k8s handles dependencies. The primary k8s.io/kubernetes
	// module only depends on v0.0.0 for these, which does not exist. Replace directives are
	// then used to redirect to the actual source for the module. Since replace directives are
	// not inherited when we depend on k8s.io/kubernetes, we need to put our own in here.
	//
	// These replace directives all point to v1.20.4's commit ID. Putting the version tag instead
	// didn't seem to work.
	k8s.io/api => k8s.io/kubernetes/staging/src/k8s.io/api v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/apiextensions-apiserver => k8s.io/kubernetes/staging/src/k8s.io/apiextensions-apiserver v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/apimachinery => k8s.io/kubernetes/staging/src/k8s.io/apimachinery v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/apiserver => k8s.io/kubernetes/staging/src/k8s.io/apiserver v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/cli-runtime => k8s.io/kubernetes/staging/src/k8s.io/cli-runtime v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/client-go => k8s.io/kubernetes/staging/src/k8s.io/client-go v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/cloud-provider => k8s.io/kubernetes/staging/src/k8s.io/cloud-provider v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/cluster-bootstrap => k8s.io/kubernetes/staging/src/k8s.io/cluster-bootstrap v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/code-generator => k8s.io/kubernetes/staging/src/k8s.io/code-generator v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/component-base => k8s.io/kubernetes/staging/src/k8s.io/component-base v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/component-helpers => k8s.io/kubernetes/staging/src/k8s.io/component-helpers v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/controller-manager => k8s.io/kubernetes/staging/src/k8s.io/controller-manager v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/cri-api => k8s.io/kubernetes/staging/src/k8s.io/cri-api v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/csi-translation-lib => k8s.io/kubernetes/staging/src/k8s.io/csi-translation-lib v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/kube-aggregator => k8s.io/kubernetes/staging/src/k8s.io/kube-aggregator v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/kube-controller-manager => k8s.io/kubernetes/staging/src/k8s.io/kube-controller-manager v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/kube-proxy => k8s.io/kubernetes/staging/src/k8s.io/kube-proxy v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/kube-scheduler => k8s.io/kubernetes/staging/src/k8s.io/kube-scheduler v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/kubectl => k8s.io/kubernetes/staging/src/k8s.io/kubectl v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/kubelet => k8s.io/kubernetes/staging/src/k8s.io/kubelet v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/legacy-cloud-providers => k8s.io/kubernetes/staging/src/k8s.io/legacy-cloud-providers v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/metrics => k8s.io/kubernetes/staging/src/k8s.io/metrics v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/mount-utils => k8s.io/kubernetes/staging/src/k8s.io/mount-utils v0.0.0-20210218160201-e87da0bd6e03
	k8s.io/sample-apiserver => k8s.io/kubernetes/staging/src/k8s.io/sample-apiserver v0.0.0-20210218160201-e87da0bd6e03
)
