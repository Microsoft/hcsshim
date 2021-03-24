// +build functional

package cri_containerd

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/services/keyvault/v7.1/keyvault"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/azure/auth"
	"github.com/Microsoft/hcsshim/osversion"
	"github.com/containerd/containerd"
	eventtypes "github.com/containerd/containerd/api/events"
	eventsapi "github.com/containerd/containerd/api/services/events/v1"
	eventruntime "github.com/containerd/containerd/runtime"
	"github.com/containerd/typeurl"
	"github.com/gogo/protobuf/types"
	"google.golang.org/grpc"
	runtime "k8s.io/cri-api/pkg/apis/runtime/v1alpha2"
	kubeutil "k8s.io/kubernetes/pkg/kubelet/util"

	_ "github.com/Microsoft/hcsshim/test/functional/manifest"
	testutilities "github.com/Microsoft/hcsshim/test/functional/utilities"
)

const (
	connectTimeout = time.Second * 10
	testNamespace  = "cri-containerd-test"

	wcowProcessRuntimeHandler         = "runhcs-wcow-process"
	wcowHypervisorRuntimeHandler      = "runhcs-wcow-hypervisor"
	wcowHypervisor17763RuntimeHandler = "runhcs-wcow-hypervisor-17763"
	wcowHypervisor18362RuntimeHandler = "runhcs-wcow-hypervisor-18362"
	wcowHypervisor19041RuntimeHandler = "runhcs-wcow-hypervisor-19041"

	testDeviceUtilFilePath = "C:\\ContainerPlat\\device-util.exe"
	testDriversPath        = "C:\\ContainerPlat\\testdrivers"
	testGPUBootFiles       = "C:\\ContainerPlat\\LinuxBootFiles\\nvidiagpu"

	lcowRuntimeHandler   = "runhcs-lcow"
	imageLcowK8sPause    = "k8s.gcr.io/pause:3.1"
	imageLcowAlpine      = "docker.io/library/alpine:latest"
	imageLcowCosmos      = "cosmosarno/spark-master:2.4.1_2019-04-18_8e864ce"
	imageJobContainerHNS = "cplatpublic.azurecr.io/jobcontainer_hns:latest"
	imageJobContainerETW = "cplatpublic.azurecr.io/jobcontainer_etw:latest"
	imageJobContainerVHD = "cplatpublic.azurecr.io/jobcontainer_vhd:latest"
	alpineAspNet         = "mcr.microsoft.com/dotnet/core/aspnet:3.1-alpine3.11"
	alpineAspnetUpgrade  = "mcr.microsoft.com/dotnet/core/aspnet:3.1.2-alpine3.11"
	// Default account name for use with GMSA related tests. This will not be
	// present/you will not have access to the account on your machine unless
	// your environment is configured properly.
	gmsaAccount = "cplat"
	// Azure Key Vault keys for custom registry
	registryKey = "registryURL"
	usernameKey = "registryUsername"
	passwordKey = "registryPassword"
)

// Image definitions
var (
	imageWindowsNanoserver      = getWindowsNanoserverImage(osversion.Get().Build)
	imageWindowsServercore      = getWindowsServerCoreImage(osversion.Get().Build)
	imageWindowsNanoserver17763 = getWindowsNanoserverImage(osversion.RS5)
	imageWindowsNanoserver18362 = getWindowsNanoserverImage(osversion.V19H1)
	imageWindowsNanoserver19041 = getWindowsNanoserverImage(osversion.V20H1)
	imageWindowsServercore17763 = getWindowsServerCoreImage(osversion.RS5)
	imageWindowsServercore18362 = getWindowsServerCoreImage(osversion.V19H1)
	imageWindowsServercore19041 = getWindowsServerCoreImage(osversion.V20H1)
)

// Flags
var (
	flagFeatures       = testutilities.NewStringSetFlag()
	flagCRIEndpoint    = flag.String("cri-endpoint", "tcp://127.0.0.1:2376", "Address of CRI runtime and image service.")
	keyVaultConfigPath *string
	registryURL        *string
	registryUsername   *string
	registryPassword   *string
)

// Features
// Make sure you update allFeatures below with any new features you add.
const (
	featureLCOW           = "LCOW"
	featureWCOWProcess    = "WCOWProcess"
	featureWCOWHypervisor = "WCOWHypervisor"
	featureHostProcess    = "HostProcess"
	featureGMSA           = "GMSA"
	featureGPU            = "GPU"
)

var allFeatures = []string{
	featureLCOW,
	featureWCOWProcess,
	featureWCOWHypervisor,
	featureHostProcess,
	featureGMSA,
	featureGPU,
}

func init() {
	// Flag definitions must be in init rather than TestMain, as TestMain isn't
	// called if -help is passed, but we want the feature usage to show up.
	flag.Var(flagFeatures, "feature", fmt.Sprintf(
		"specifies which sets of functionality to test. can be set multiple times\n"+
			"supported features: %v", allFeatures))

	flag.StringVar(keyVaultConfigPath, "kv-config", "", "azure key vault config")
	flag.StringVar(registryURL, "registry-url", "", "custom container registry URL")
	flag.StringVar(registryUsername, "registry-username", "00000000-0000-0000-0000-000000000000", "custom container registry username")
	flag.StringVar(registryPassword, "registry-password", "", "custom container registry password")
}

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

// requireFeatures checks in flagFeatures to validate that each required feature
// was enabled, and skips the test if any are missing. If the flagFeatures set
// is empty, the function returns (by default all features are enabled).
func requireFeatures(t *testing.T, features ...string) {
	set := flagFeatures.ValueSet()
	if len(set) == 0 {
		return
	}
	for _, feature := range features {
		if _, ok := set[feature]; !ok {
			t.Skipf("skipping test due to feature not set: %s", feature)
		}
	}
}

type KeyVaultConfig struct {
	TenantID  string `json:"tenant_id"`
	ClientID  string `json:"client_id"`
	VaultName string `json:"vault_name"`
	// TODO: This probably should be thumbprint and we should figure out the path based on that instead
	CertPath string `json:"cert_path"`
}

// akvClient is a wrapper struct on top of keyvault.BaseClient to simplify secret lookups
type akvClient struct {
	ctx          context.Context
	client       keyvault.BaseClient
	vaultName    string
	vaultBaseURL string
}

// GetSecret reads secret from AKV
func (c *akvClient) GetSecret(key string) (string, error) {
	response, err := c.client.GetSecret(c.ctx, c.vaultBaseURL, key, "")
	if err != nil {
		return "", err
	}
	return *response.Value, nil
}

// registryConfig contains information required to pull images from custom registries
type registryConfig struct {
	username    string
	password    string
	registryURL string
}

// requireCustomRegistryConfig first loads registry configuration from AKV and with a fallback behavior
// to load from command line args
func requireCustomRegistryConfig(t *testing.T) *registryConfig {
	// First try to load config from AKV
	if kvConfig := loadKeyVaultConfig(t); kvConfig == nil {
		t.Log("failed to load AKV config, checking command line arguments instead")
	} else {
		if conf, err := newRegistryConfigFromKeyVault(kvConfig); err != nil {
			t.Logf("failed to load registry config from AKV: %s", err)
		} else {
			return conf
		}
	}

	// Fallback to loading from args
	conf, err := newRegistryConfigFromArgs()
	if err != nil {
		t.Skipf("failed to load registry config from args: %s", err)
	}
	return conf
}

// newRegistryConfigFromKeyVault tries to read registry information from AKV and creates registry
// config from them
func newRegistryConfigFromKeyVault(kvConfig *KeyVaultConfig) (*registryConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := newKeyVaultClient(ctx, kvConfig)
	if err != nil {
		return nil, err
	}

	var registry, user, pass string
	// lookup registry information key vault
	if registry, err = client.GetSecret(registryKey); err != nil {
		return nil, err
	}
	if user, err = client.GetSecret(usernameKey); err != nil {
		return nil, err
	}
	if pass, err = client.GetSecret(passwordKey); err != nil {
		return nil, err
	}

	return newRegistryConfig(registry, user, pass), nil
}

// newRegistryConfigFromArgs checks command line arguments and creates a registry config from them
func newRegistryConfigFromArgs() (*registryConfig, error) {
	if registryURL == nil || registryPassword == nil {
		return nil, errors.New("no registry url/password provided")
	}

	return newRegistryConfig(*registryURL, *registryUsername, *registryPassword), nil
}

// newRegistryConfig returns a custom registry config
func newRegistryConfig(regURL, regUser, regPass string) *registryConfig {
	if regURL == "" || regPass == "" {
		return nil
	}

	return &registryConfig{
		registryURL: regURL,
		username:    regUser,
		password:    regPass,
	}
}

// newKeyVaultClient returns `akvClient` with certificate authorizer
func newKeyVaultClient(ctx context.Context, kvConfig *KeyVaultConfig) (*akvClient, error) {
	clientCertConfig := auth.ClientCertificateConfig{
		CertificatePath:     kvConfig.CertPath,
		CertificatePassword: "",
		ClientID:            kvConfig.ClientID,
		TenantID:            kvConfig.TenantID,
		Resource:            strings.TrimSuffix(azure.PublicCloud.KeyVaultEndpoint, "/"),
		AADEndpoint:         azure.PublicCloud.ActiveDirectoryEndpoint,
	}
	authorizer, err := clientCertConfig.Authorizer()
	if err != nil {
		return nil, err
	}

	client := keyvault.New()
	client.Authorizer = authorizer

	return &akvClient{
		ctx:          ctx,
		client:       client,
		vaultName:    kvConfig.VaultName,
		vaultBaseURL: fmt.Sprintf("https://%s.%s", kvConfig.VaultName, azure.PublicCloud.KeyVaultDNSSuffix),
	}, nil
}

// loadKeyVaultConfig loads KeyVaultConfig from path provided to the test executable
func loadKeyVaultConfig(t *testing.T) *KeyVaultConfig {
	if keyVaultConfigPath == nil {
		return nil
	}

	content, err := ioutil.ReadFile(*keyVaultConfigPath)
	if err != nil {
		t.Logf("failed to read key vault file: %s", err)
		return nil
	}

	var kvConfig KeyVaultConfig
	if err := json.Unmarshal(content, &kvConfig); err != nil {
		t.Logf("failed to unmarshal KeyVault config:\n%s", string(content))
		return nil
	}

	return &kvConfig
}

func getWindowsNanoserverImage(build uint16) string {
	switch build {
	case osversion.RS5:
		return "mcr.microsoft.com/windows/nanoserver:1809"
	case osversion.V19H1:
		return "mcr.microsoft.com/windows/nanoserver:1903"
	case osversion.V20H1:
		return "mcr.microsoft.com/windows/nanoserver:2004"
	default:
		panic("unsupported build")
	}
}

func getWindowsServerCoreImage(build uint16) string {
	switch build {
	case osversion.RS5:
		return "mcr.microsoft.com/windows/servercore:1809"
	case osversion.V19H1:
		return "mcr.microsoft.com/windows/servercore:1903"
	case osversion.V20H1:
		return "mcr.microsoft.com/windows/servercore:2004"
	default:
		panic("unsupported build")
	}
}

func createGRPCConn(ctx context.Context) (*grpc.ClientConn, error) {
	addr, dialer, err := kubeutil.GetAddressAndDialer(*flagCRIEndpoint)
	if err != nil {
		return nil, err
	}
	return grpc.DialContext(ctx, addr, grpc.WithInsecure(), grpc.WithContextDialer(dialer))
}

func newTestRuntimeClient(t *testing.T) runtime.RuntimeServiceClient {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	conn, err := createGRPCConn(ctx)
	if err != nil {
		t.Fatalf("failed to dial runtime client: %v", err)
	}
	return runtime.NewRuntimeServiceClient(conn)
}

func newTestEventService(t *testing.T) containerd.EventService {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	conn, err := createGRPCConn(ctx)
	if err != nil {
		t.Fatalf("Failed to create a client connection %v", err)
	}
	return containerd.NewEventServiceFromClient(eventsapi.NewEventsClient(conn))
}

func newTestImageClient(t *testing.T) runtime.ImageServiceClient {
	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()
	conn, err := createGRPCConn(ctx)
	if err != nil {
		t.Fatalf("failed to dial runtime client: %v", err)
	}
	return runtime.NewImageServiceClient(conn)
}

func getTargetRunTopics() (topicNames []string, filters []string) {
	topicNames = []string{
		eventruntime.TaskCreateEventTopic,
		eventruntime.TaskStartEventTopic,
		eventruntime.TaskExitEventTopic,
		eventruntime.TaskDeleteEventTopic,
	}

	filters = make([]string, len(topicNames))

	for i, name := range topicNames {
		filters[i] = fmt.Sprintf(`topic=="%v"`, name)
	}
	return topicNames, filters
}

func convertEvent(e *types.Any) (string, interface{}, error) {
	id := ""
	evt, err := typeurl.UnmarshalAny(e)
	if err != nil {
		return "", nil, err
	}

	switch event := evt.(type) {
	case *eventtypes.TaskCreate:
		id = event.ContainerID
	case *eventtypes.TaskStart:
		id = event.ContainerID
	case *eventtypes.TaskDelete:
		id = event.ContainerID
	case *eventtypes.TaskExit:
		id = event.ContainerID
	default:
		return "", nil, errors.New("test does not support this event")
	}
	return id, evt, nil
}

func pullRequiredImages(t *testing.T, images []string) {
	pullRequiredImagesWithLabelsAndAuth(t, images, map[string]string{
		"sandbox-platform": "windows/amd64", // Not required for Windows but makes the test safer depending on defaults in the config.
	}, nil)
}

func pullRequiredImagesWithAuth(t *testing.T, images []string, regConf *registryConfig) {
	pullRequiredImagesWithLabelsAndAuth(t, images, map[string]string{
		"sandbox-platform": "windows/amd64", // Not required for Windows but makes the test safer depending on defaults in the config.
	}, regConf)
}

func pullRequiredLcowImages(t *testing.T, images []string) {
	pullRequiredImagesWithLabelsAndAuth(t, images, map[string]string{
		"sandbox-platform": "linux/amd64",
	}, nil)
}

func pullRequiredLcowImagesWithAuth(t *testing.T, images []string, regConf *registryConfig) {
	pullRequiredImagesWithLabelsAndAuth(t, images, map[string]string{
		"sandbox-platform": "linux/amd64",
	}, regConf)
}

func pullRequiredImagesWithLabelsAndAuth(t *testing.T, images []string, labels map[string]string, regConf *registryConfig) {
	if len(images) < 1 {
		return
	}

	client := newTestImageClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var auth *runtime.AuthConfig
	if regConf != nil {
		auth = &runtime.AuthConfig{
			Username: regConf.username,
			Auth:     regConf.password,
		}
	}
	sb := &runtime.PodSandboxConfig{
		Labels: labels,
	}
	for _, image := range images {
		_, err := client.PullImage(ctx, &runtime.PullImageRequest{
			Image: &runtime.ImageSpec{
				Image: image,
			},
			Auth:          auth,
			SandboxConfig: sb,
		})
		if err != nil {
			t.Fatalf("failed PullImage for image: %s, with error: %v", image, err)
		}
	}
}

func removeImages(t *testing.T, images []string) {
	if len(images) < 1 {
		return
	}

	client := newTestImageClient(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for _, image := range images {
		_, err := client.RemoveImage(ctx, &runtime.RemoveImageRequest{
			Image: &runtime.ImageSpec{
				Image: image,
			},
		})
		if err != nil {
			t.Fatalf("failed removeImage for image: %s, with error: %v", image, err)
		}
	}
}
