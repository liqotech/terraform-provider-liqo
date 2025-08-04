// Copyright 2019-2025 The Liqo Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package liqo provides resources and provider methods.
package liqo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	corev1beta1 "github.com/liqotech/liqo/apis/core/v1beta1"
	networkingv1beta1 "github.com/liqotech/liqo/apis/networking/v1beta1"
	offloadingv1beta1 "github.com/liqotech/liqo/apis/offloading/v1beta1"
	"github.com/mitchellh/go-homedir"
	apimachineryschema "k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func init() {
	utilruntime.Must(corev1beta1.AddToScheme(scheme.Scheme))
	utilruntime.Must(networkingv1beta1.AddToScheme(scheme.Scheme))
	utilruntime.Must(offloadingv1beta1.AddToScheme(scheme.Scheme))
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
// configureKubernetesClient configures Kubernetes client settings from provider configuration.
func configureKubernetesClient(kubeConfig *kubeConf) (*clientcmd.ConfigOverrides, *clientcmd.ClientConfigLoadingRules, error) {
	overrides := &clientcmd.ConfigOverrides{}
	loader := &clientcmd.ClientConfigLoadingRules{}

	configPaths := []string{}

	if !kubeConfig.KubeConfigPath.IsNull() {
		configPaths = []string{kubeConfig.KubeConfigPath.ValueString()}
	} else if len(kubeConfig.KubeConfigPaths) > 0 {
		for _, configPath := range kubeConfig.KubeConfigPaths {
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

		ctxNotOk := kubeConfig.KubeCtx.IsNull()
		authInfoNotOk := kubeConfig.KubeCtxAuthInfo.IsNull()
		clusterNotOk := kubeConfig.KubeCtxCluster.IsNull()

		if ctxNotOk || authInfoNotOk || clusterNotOk {
			if ctxNotOk {
				overrides.CurrentContext = kubeConfig.KubeCtx.ValueString()
			}

			overrides.Context = clientcmdapi.Context{}
			if authInfoNotOk {
				overrides.Context.AuthInfo = kubeConfig.KubeCtxAuthInfo.ValueString()
			}
			if clusterNotOk {
				overrides.Context.Cluster = kubeConfig.KubeCtxCluster.ValueString()
			}
		}
	}

	if !kubeConfig.KubeInsecure.IsNull() {
		overrides.ClusterInfo.InsecureSkipTLSVerify = !kubeConfig.KubeInsecure.ValueBool()
	}
	if !kubeConfig.KubeClusterCaCertData.IsNull() {
		overrides.ClusterInfo.CertificateAuthorityData = bytes.NewBufferString(kubeConfig.KubeClusterCaCertData.ValueString()).Bytes()
	}
	if !kubeConfig.KubeClientCertData.IsNull() {
		overrides.AuthInfo.ClientCertificateData = bytes.NewBufferString(kubeConfig.KubeClientCertData.ValueString()).Bytes()
	}
	if !kubeConfig.KubeHost.IsNull() {
		hasCA := len(overrides.ClusterInfo.CertificateAuthorityData) != 0
		hasCert := len(overrides.AuthInfo.ClientCertificateData) != 0
		defaultTLS := hasCA || hasCert || overrides.ClusterInfo.InsecureSkipTLSVerify
		host, _, err := rest.DefaultServerURL(kubeConfig.KubeHost.ValueString(), "", apimachineryschema.GroupVersion{}, defaultTLS)
		if err != nil {
			return nil, nil, err
		}

		overrides.ClusterInfo.Server = host.String()
	}
	if !kubeConfig.KubeUser.IsNull() {
		overrides.AuthInfo.Username = kubeConfig.KubeUser.ValueString()
	}
	if !kubeConfig.KubePassword.IsNull() {
		overrides.AuthInfo.Password = kubeConfig.KubePassword.ValueString()
	}
	if !kubeConfig.KubeClientKeyData.IsNull() {
		overrides.AuthInfo.ClientKeyData = bytes.NewBufferString(kubeConfig.KubeClientKeyData.ValueString()).Bytes()
	}
	if !kubeConfig.KubeToken.IsNull() {
		overrides.AuthInfo.Token = kubeConfig.KubeToken.ValueString()
	}

	if !kubeConfig.KubeProxyURL.IsNull() {
		overrides.ClusterDefaults.ProxyURL = kubeConfig.KubeProxyURL.ValueString()
	}

	if len(kubeConfig.KubeExec) > 0 {
		exec := &clientcmdapi.ExecConfig{}
		exec.InteractiveMode = clientcmdapi.IfAvailableExecInteractiveMode
		exec.APIVersion = kubeConfig.KubeExec[0].APIVersion.ValueString()
		exec.Command = kubeConfig.KubeExec[0].Command.ValueString()
		for _, arg := range kubeConfig.KubeExec[0].Args {
			exec.Args = append(exec.Args, arg.ValueString())
		}

		for kk, vv := range kubeConfig.KubeExec[0].Env.Elements() {
			exec.Env = append(exec.Env, clientcmdapi.ExecEnvVar{Name: kk, Value: vv.String()})
		}

		overrides.AuthInfo.Exec = exec
	}

	return overrides, loader, nil
}

// CheckParameters method used to check if kubernetes parameters are null.
func CheckParameters(config *liqoProviderModel) (*clientcmd.ConfigOverrides, *clientcmd.ClientConfigLoadingRules, error) {
	return configureKubernetesClient(config.Kubernetes)
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

func (p *liqoProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	schemaObj := schema.Schema{
		Description: "Interact with Liqo.",
		Attributes: map[string]schema.Attribute{
			"liqo_version": schema.StringAttribute{
				Optional:    true,
				Description: fmt.Sprintf("The version of Liqo to use for downloading liqoctl binary. Defaults to %s.", LiqoVersion),
			},
			"kubernetes": schema.SingleNestedAttribute{
				Optional: true,
				Attributes: map[string]schema.Attribute{
					"host": schema.StringAttribute{
						Optional:    true,
						Description: "The hostname (in form of URI) of Kubernetes master.",
					},
					"username": schema.StringAttribute{
						Optional:    true,
						Description: "The username to use for HTTP basic authentication when accessing the Kubernetes master endpoint.",
					},
					"password": schema.StringAttribute{
						Optional:    true,
						Sensitive:   true,
						Description: "The password to use for HTTP basic authentication when accessing the Kubernetes master endpoint.",
					},
					"insecure": schema.BoolAttribute{
						Optional:    true,
						Description: "Whether server should be accessed without verifying the TLS certificate.",
					},
					"client_certificate": schema.StringAttribute{
						Optional:    true,
						Description: "PEM-encoded client certificate for TLS authentication.",
					},
					"client_key": schema.StringAttribute{
						Optional:    true,
						Sensitive:   true,
						Description: "PEM-encoded client certificate key for TLS authentication.",
					},
					"cluster_ca_certificate": schema.StringAttribute{
						Optional:    true,
						Description: "PEM-encoded root certificates bundle for TLS authentication.",
					},
					"config_paths": schema.ListAttribute{
						ElementType: types.StringType,
						Optional:    true,
					},
					"config_path": schema.StringAttribute{
						Optional:    true,
						Description: "Path to the kube config file. Can be set with KubeConfigPath.",
					},
					"config_context": schema.StringAttribute{
						Optional: true,
					},
					"config_context_auth_info": schema.StringAttribute{
						Optional:    true,
						Description: "",
					},
					"config_context_cluster": schema.StringAttribute{
						Optional:    true,
						Description: "",
					},
					"token": schema.StringAttribute{
						Optional:    true,
						Sensitive:   true,
						Description: "Token to authenticate an service account",
					},
					"proxy_url": schema.StringAttribute{
						Optional:    true,
						Description: "URL to the proxy to be used for all API requests",
					},
					"exec": schema.SingleNestedAttribute{
						Optional: true,
						Attributes: map[string]schema.Attribute{
							"api_version": schema.StringAttribute{
								Required: true,
								Validators: []validator.String{
									stringvalidator.NoneOf("client.authentication.k8s.io/v1alpha1"),
								},
							},
							"command": schema.StringAttribute{
								Required: true,
							},
							"env": schema.MapAttribute{
								ElementType: types.StringType,
								Optional:    true,
							},
							"args": schema.ListAttribute{
								ElementType: types.StringType,
								Optional:    true,
							},
						},
					},
				},
			},
		},
	}
	resp.Schema = schemaObj
}

// Configure method to create the kubernetes Client using parameters passed in the provider instantiation in Terraform main
// After the creation the Client will be available in resources and data sources.
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
		NewPeerResource, NewOffloadResource,
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
	LiqoVersion types.String `tfsdk:"liqo_version"`
	Kubernetes  *kubeConf    `tfsdk:"kubernetes"`
}
