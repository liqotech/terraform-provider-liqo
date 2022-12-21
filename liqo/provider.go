// Package liqo provides resources and provider methods.
package liqo

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/mitchellh/go-homedir"
	apimachineryschema "k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	discoveryv1alpha1 "github.com/liqotech/liqo/apis/discovery/v1alpha1"
	netv1alpha1 "github.com/liqotech/liqo/apis/net/v1alpha1"
	offloadingv1alpha1 "github.com/liqotech/liqo/apis/offloading/v1alpha1"
	sharingv1alpha1 "github.com/liqotech/liqo/apis/sharing/v1alpha1"
	planmodifier "github.com/liqotech/terraform-provider-liqo/liqo/attribute_plan_modifier"
)

func init() {
	utilruntime.Must(discoveryv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(netv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(offloadingv1alpha1.AddToScheme(scheme.Scheme))
	utilruntime.Must(sharingv1alpha1.AddToScheme(scheme.Scheme))
}

var (
	_ provider.Provider = &liqoProvider{}
)

// New provides the initialization of provider.
func New() provider.Provider {
	return &liqoProvider{}
}

type liqoProvider struct {
}

// CheckParameters method used to check if kubernetes parameters are null.
func CheckParameters(config *liqoProviderModel) (*clientcmd.ConfigOverrides, *clientcmd.ClientConfigLoadingRules, error) {
	overrides := &clientcmd.ConfigOverrides{}
	loader := &clientcmd.ClientConfigLoadingRules{}

	configPaths := []string{}

	if !config.Kubernetes.KubeConfigPath.IsNull() {
		configPaths = []string{config.Kubernetes.KubeConfigPath.ValueString()}
	} else if len(config.Kubernetes.KubeConfigPaths) > 0 {
		for _, configPath := range config.Kubernetes.KubeConfigPaths {
			configPaths = append(configPaths, configPath.ValueString())
		}
	} else if v := os.Getenv("KubeConfigPaths"); v != "" {
		configPaths = filepath.SplitList(v)
	}

	if len(configPaths) > 0 {
		expandedPaths := []string{}
		for _, p := range configPaths {
			path, err := homedir.Expand(p)
			if err != nil {
				return nil, nil, err
			}
			expandedPaths = append(expandedPaths, path)
		}

		if len(expandedPaths) == 1 {
			loader.ExplicitPath = expandedPaths[0]
		} else {
			loader.Precedence = expandedPaths
		}

		ctxNotOk := config.Kubernetes.KubeCtx.IsNull()
		authInfoNotOk := config.Kubernetes.KubeCtxAuthInfo.IsNull()
		clusterNotOk := config.Kubernetes.KubeCtxCluster.IsNull()

		if ctxNotOk || authInfoNotOk || clusterNotOk {
			if ctxNotOk {
				overrides.CurrentContext = config.Kubernetes.KubeCtx.ValueString()
			}

			overrides.Context = clientcmdapi.Context{}
			if authInfoNotOk {
				overrides.Context.AuthInfo = config.Kubernetes.KubeCtxAuthInfo.ValueString()
			}
			if clusterNotOk {
				overrides.Context.Cluster = config.Kubernetes.KubeCtxCluster.ValueString()
			}
		}
	}

	if !config.Kubernetes.KubeInsecure.IsNull() {
		overrides.ClusterInfo.InsecureSkipTLSVerify = !config.Kubernetes.KubeInsecure.ValueBool()
	}
	if !config.Kubernetes.KubeClusterCaCertData.IsNull() {
		overrides.ClusterInfo.CertificateAuthorityData = bytes.NewBufferString(config.Kubernetes.KubeClusterCaCertData.ValueString()).Bytes()
	}
	if !config.Kubernetes.KubeClientCertData.IsNull() {
		overrides.AuthInfo.ClientCertificateData = bytes.NewBufferString(config.Kubernetes.KubeClientCertData.ValueString()).Bytes()
	}
	if !config.Kubernetes.KubeHost.IsNull() {
		hasCA := len(overrides.ClusterInfo.CertificateAuthorityData) != 0
		hasCert := len(overrides.AuthInfo.ClientCertificateData) != 0
		defaultTLS := hasCA || hasCert || overrides.ClusterInfo.InsecureSkipTLSVerify
		host, _, err := rest.DefaultServerURL(config.Kubernetes.KubeHost.ValueString(), "", apimachineryschema.GroupVersion{}, defaultTLS)
		if err != nil {
			return nil, nil, err
		}

		overrides.ClusterInfo.Server = host.String()
	}
	if !config.Kubernetes.KubeUser.IsNull() {
		overrides.AuthInfo.Username = config.Kubernetes.KubeUser.ValueString()
	}
	if !config.Kubernetes.KubePassword.IsNull() {
		overrides.AuthInfo.Password = config.Kubernetes.KubePassword.ValueString()
	}
	if !config.Kubernetes.KubeClientKeyData.IsNull() {
		overrides.AuthInfo.ClientKeyData = bytes.NewBufferString(config.Kubernetes.KubeClientKeyData.ValueString()).Bytes()
	}
	if !config.Kubernetes.KubeToken.IsNull() {
		overrides.AuthInfo.Token = config.Kubernetes.KubeToken.ValueString()
	}

	if !config.Kubernetes.KubeProxyURL.IsNull() {
		overrides.ClusterDefaults.ProxyURL = config.Kubernetes.KubeProxyURL.ValueString()
	}

	if len(config.Kubernetes.KubeExec) > 0 {
		exec := &clientcmdapi.ExecConfig{}
		exec.InteractiveMode = clientcmdapi.IfAvailableExecInteractiveMode
		exec.APIVersion = config.Kubernetes.KubeExec[0].APIVersion.ValueString()
		exec.Command = config.Kubernetes.KubeExec[0].Command.ValueString()
		for _, arg := range config.Kubernetes.KubeExec[0].Args {
			exec.Args = append(exec.Args, arg.ValueString())
		}

		for kk, vv := range config.Kubernetes.KubeExec[0].Env.Elements() {
			exec.Env = append(exec.Env, clientcmdapi.ExecEnvVar{Name: kk, Value: vv.String()})
		}

		overrides.AuthInfo.Exec = exec
	}

	return overrides, loader, nil
}

// NewClients method to create CRClient and KubeClient.
func NewClients(overrides *clientcmd.ConfigOverrides, loader *clientcmd.ClientConfigLoadingRules) (client.Client, *kubernetes.Clientset, error) {
	clientCfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, overrides)
	if clientCfg == nil {
		return nil, nil, errors.New("error while creating clientCfg")
	}

	var restCfg *rest.Config

	restCfg, err := clientCfg.ClientConfig()
	if err != nil {
		return nil, nil, err
	}

	var CRClient client.Client

	CRClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, nil, err
	}

	KubeClient, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, nil, err
	}

	return CRClient, KubeClient, nil
}

func (p *liqoProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "liqo"
}

func (p *liqoProvider) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Interact with Liqo.",
		Attributes: map[string]tfsdk.Attribute{
			"kubernetes": {
				Optional: true,
				Computed: true,
				Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
					"host": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "The hostname (in form of URI) of Kubernetes master.",
					},
					"username": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "The username to use for HTTP basic authentication when accessing the Kubernetes master endpoint.",
					},
					"password": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "The password to use for HTTP basic authentication when accessing the Kubernetes master endpoint.",
					},
					"insecure": {
						Type:     types.BoolType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.BoolValue(false)),
						},
						Description: "Whether server should be accessed without verifying the TLS certificate.",
					},
					"client_certificate": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "PEM-encoded client certificate for TLS authentication.",
					},
					"client_key": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "PEM-encoded client certificate key for TLS authentication.",
					},
					"cluster_ca_certificate": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "PEM-encoded root certificates bundle for TLS authentication.",
					},
					"config_paths": {
						Type:     types.ListType{ElemType: types.StringType},
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.ListNull(types.StringType)),
						},
					},
					"config_path": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "Path to the kube config file. Can be set with KubeConfigPath.",
					},
					"config_context": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
					},
					"config_context_auth_info": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "",
					},
					"config_context_cluster": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "",
					},
					"token": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "Token to authenticate an service account",
					},
					"proxy_url": {
						Type:     types.StringType,
						Optional: true,
						PlanModifiers: []tfsdk.AttributePlanModifier{
							planmodifier.DefaultValue(types.StringValue("")),
						},
						Description: "URL to the proxy to be used for all API requests",
					},
					"exec": {
						Optional: true,
						Attributes: tfsdk.SingleNestedAttributes(map[string]tfsdk.Attribute{
							"api_version": {
								Type:     types.StringType,
								Required: true,
								PlanModifiers: []tfsdk.AttributePlanModifier{
									planmodifier.DefaultValue(types.StringValue("")),
								},
								Validators: []tfsdk.AttributeValidator{
									stringvalidator.NoneOf("client.authentication.k8s.io/v1alpha1"),
								},
							},
							"command": {
								Type:     types.StringType,
								Required: true,
								PlanModifiers: []tfsdk.AttributePlanModifier{
									planmodifier.DefaultValue(types.StringValue("")),
								},
							},
							"env": {
								Type:     types.MapType{ElemType: types.StringType},
								Optional: true,
								PlanModifiers: []tfsdk.AttributePlanModifier{
									planmodifier.DefaultValue(types.MapNull(types.StringType)),
								},
							},
							"args": {
								Type:     types.ListType{ElemType: types.StringType},
								Optional: true,
								PlanModifiers: []tfsdk.AttributePlanModifier{
									planmodifier.DefaultValue(types.ListNull(types.StringType)),
								},
							},
						}),
					},
				}),
			},
		},
	}, nil
}

// Configure method to create the two kubernetes Clients using parameters passed in the provider instantiation in Terraform main
// After the creation both Clients will be available in resources and data sources.
//
//nolint:gocritic // Terraform Framework template code
func (p *liqoProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var config liqoProviderModel
	diags := req.Config.Get(ctx, &config)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.ResourceData = config
}

func (p *liqoProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func (p *liqoProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewPeerResource, NewGenerateResource, NewOffloadResource,
	}
}

type exec struct {
	APIVersion types.String   `tfsdk:"api_version"`
	Command    types.String   `tfsdk:"command"`
	Env        types.Map      `tfsdk:"env"`
	Args       []types.String `tfsdk:"args"`
}

type kubeConf struct {
	KubeHost              types.String   `tfsdk:"host"`
	KubeUser              types.String   `tfsdk:"username"`
	KubePassword          types.String   `tfsdk:"password"`
	KubeInsecure          types.Bool     `tfsdk:"insecure"`
	KubeClientCertData    types.String   `tfsdk:"client_certificate"`
	KubeClientKeyData     types.String   `tfsdk:"client_key"`
	KubeClusterCaCertData types.String   `tfsdk:"cluster_ca_certificate"`
	KubeConfigPath        types.String   `tfsdk:"config_path"`
	KubeConfigPaths       []types.String `tfsdk:"config_paths"`
	KubeCtx               types.String   `tfsdk:"config_context"`
	KubeCtxAuthInfo       types.String   `tfsdk:"config_context_auth_info"`
	KubeCtxCluster        types.String   `tfsdk:"config_context_cluster"`
	KubeToken             types.String   `tfsdk:"token"`
	KubeProxyURL          types.String   `tfsdk:"proxy_url"`
	KubeExec              []exec         `tfsdk:"exec"`
}

type liqoProviderModel struct {
	Kubernetes *kubeConf `tfsdk:"kubernetes"`
}
