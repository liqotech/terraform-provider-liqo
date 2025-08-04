//go:generate ../scripts/generate_version.sh

package liqo

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	execCmd "os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// Constants for peer status values and file extensions.
const (
	statusReady        = "ready"
	statusTimeout      = "timeout"
	statusError        = "error"
	statusEstablishing = "establishing"
	windowsOS          = "windows"
	exeExtension       = ".exe"
)

var (
	_ resource.Resource              = &peerResource{}
	_ resource.ResourceWithConfigure = &peerResource{}
)

// NewPeerResource provides the initialization of Peer Resource.
func NewPeerResource() resource.Resource {
	return &peerResource{}
}

type peerResource struct {
	config liqoProviderModel
}

func (p *peerResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_peer"
}

func (p *peerResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Enable peering towards a remote provider cluster using liqoctl.",
		Attributes: map[string]schema.Attribute{
			// Required kubeconfig paths
			"remote_kubeconfig": schema.StringAttribute{
				Required:    true,
				Description: "Path to the remote (provider) cluster kubeconfig file.",
			},

			// Optional parameters matching liqoctl peer command
			"timeout": schema.StringAttribute{
				Optional:    true,
				Description: "Timeout for peering completion (e.g., '10m').",
			},
			"skip_validation": schema.BoolAttribute{
				Optional:    true,
				Description: "Skip the validation.",
			},

			// Liqo namespace configuration
			"liqo_namespace": schema.StringAttribute{
				Optional:    true,
				Description: "Namespace where Liqo is installed in local cluster.",
			},
			"remote_liqo_namespace": schema.StringAttribute{
				Optional:    true,
				Description: "Namespace where Liqo is installed in remote cluster.",
			},

			// Networking configuration
			"networking_disabled": schema.BoolAttribute{
				Optional:    true,
				Description: "Disable networking between the two clusters.",
			},
			"gw_server_service_type": schema.StringAttribute{
				Optional:    true,
				Description: "Service type of the Gateway Server service (LoadBalancer, NodePort, ClusterIP).",
			},
			"gw_server_service_port": schema.Int64Attribute{
				Optional:    true,
				Description: "Port of the Gateway Server service.",
			},
			"gw_server_service_nodeport": schema.Int64Attribute{
				Optional:    true,
				Description: "Force the NodePort of the Gateway Server service.",
			},
			"gw_server_service_loadbalancer_ip": schema.StringAttribute{
				Optional:    true,
				Description: "IP of the LoadBalancer for the Gateway Server service.",
			},
			"gw_client_address": schema.StringAttribute{
				Optional:    true,
				Description: "Address used by the gateway client to connect to the gateway server.",
			},
			"gw_client_port": schema.Int64Attribute{
				Optional:    true,
				Description: "Port used by the gateway client to connect to the gateway server.",
			},
			"mtu": schema.Int64Attribute{
				Optional:    true,
				Description: "MTU of the Gateway server and client.",
			},

			// Authentication configuration
			"create_resource_slice": schema.BoolAttribute{
				Optional:    true,
				Description: "Create a ResourceSlice for the peering.",
			},
			"resource_slice_class": schema.StringAttribute{
				Optional:    true,
				Description: "The class of the ResourceSlice.",
			},
			"in_band": schema.BoolAttribute{
				Optional:    true,
				Description: "Use in-band authentication.",
			},
			"proxy_url": schema.StringAttribute{
				Optional:    true,
				Description: "The URL of the proxy to use for communication with the remote cluster.",
			},

			// Resource configuration
			"create_virtual_node": schema.BoolAttribute{
				Optional:    true,
				Description: "Create a VirtualNode for the peering.",
			},
			"cpu": schema.StringAttribute{
				Optional:    true,
				Description: "The amount of CPU requested for the VirtualNode.",
			},
			"memory": schema.StringAttribute{
				Optional:    true,
				Description: "The amount of memory requested for the VirtualNode.",
			},
			"pods": schema.StringAttribute{
				Optional:    true,
				Description: "The amount of pods requested for the VirtualNode.",
			},

			// Output/status attributes
			"peer_status": schema.StringAttribute{
				Computed:    true,
				Description: "Status of the peering operation.",
			},
		},
	}
}

// Create implements peering using liqoctl peer command
//
//nolint:gocritic // Terraform Framework template code
func (p *peerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan peerResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate required parameters
	if plan.RemoteKubeconfig.IsNull() || plan.RemoteKubeconfig.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Invalid Configuration",
			"remote_kubeconfig is required",
		)
		return
	}

	// Get kubeconfig path from provider config
	var localKubeconfig string
	if p.config.Kubernetes != nil && !p.config.Kubernetes.KubeConfigPath.IsNull() {
		localKubeconfig = p.config.Kubernetes.KubeConfigPath.ValueString()
	} else {
		// Use default kubeconfig location or let kubectl handle it
		localKubeconfig = "" // Will use default kubectl behavior
	}

	// Download liqoctl if not available
	liqoctlPath, err := downloadLiqoctl(ctx, p.config)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Download liqoctl",
			err.Error(),
		)
		return
	}

	// Build the liqoctl peer command
	args := buildPeerCommand(&plan, localKubeconfig)

	// Execute the liqoctl peer command
	//nolint:gosec // liqoctlPath is validated and args are constructed safely
	cmd := execCmd.CommandContext(ctx, liqoctlPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Diagnostics.AddError(
			"Peering Failed",
			fmt.Sprintf("liqoctl peer command failed: %s\nOutput: %s", err.Error(), string(output)),
		)
		return
	}

	// Wait for peering completion with a timeout (default 5 minutes)
	timeout := 5 * time.Minute
	if !plan.Timeout.IsNull() && plan.Timeout.ValueString() != "" {
		if parsedTimeout, err := time.ParseDuration(plan.Timeout.ValueString()); err == nil {
			timeout = parsedTimeout
		}
	}

	// Poll for peering status until completion
	status, err := waitForPeeringCompletion(ctx, liqoctlPath, localKubeconfig, &plan, timeout)
	if err != nil {
		resp.Diagnostics.AddError(
			"Peering Status Check Failed",
			fmt.Sprintf("Failed to wait for peering completion: %s", err.Error()),
		)
		return
	}

	// Update the plan with the actual status
	switch status {
	case statusReady:
		plan.PeerStatus = types.StringValue(statusReady)
	case statusTimeout:
		plan.PeerStatus = types.StringValue(statusTimeout)
		resp.Diagnostics.AddWarning(
			"Peering Timeout",
			"Peering command was executed but timeout occurred while waiting for completion. The peering may still be in progress.",
		)
	case statusError:
		plan.PeerStatus = types.StringValue(statusError)
		resp.Diagnostics.AddError(
			"Peering Failed",
			"Peering operation failed during status check.",
		)
		return
	default:
		plan.PeerStatus = types.StringValue(status)
	}

	// Save the state
	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

//nolint:gocritic // Terraform Framework template code
func (p *peerResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state peerResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

//nolint:gocritic // Terraform Framework template code
func (p *peerResource) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unable to Update Resource",
		"Update is not supported/permitted yet.",
	)
}

// Delete implements unpeering using liqoctl unpeer command
//
//nolint:gocritic // Terraform Framework template code
func (p *peerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data peerResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get kubeconfig path from provider config
	var localKubeconfig string
	if p.config.Kubernetes != nil && !p.config.Kubernetes.KubeConfigPath.IsNull() {
		localKubeconfig = p.config.Kubernetes.KubeConfigPath.ValueString()
	} else {
		// Use default kubeconfig location or let kubectl handle it
		localKubeconfig = "" // Will use default kubectl behavior
	}

	// Download liqoctl if not available
	liqoctlPath, err := downloadLiqoctl(ctx, p.config)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Download liqoctl",
			err.Error(),
		)
		return
	}

	// Build the liqoctl unpeer command
	args := buildUnpeerCommand(&data, localKubeconfig)

	// Execute the liqoctl unpeer command
	//nolint:gosec // liqoctlPath is validated and args are constructed safely
	cmd := execCmd.CommandContext(ctx, liqoctlPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		resp.Diagnostics.AddError(
			"Unpeering Failed",
			fmt.Sprintf("liqoctl unpeer command failed: %s\nOutput: %s", err.Error(), string(output)),
		)
		return
	}

	// Resource has been successfully deleted
}

// Configure method to obtain kubernetes Clients provided by provider.
func (p *peerResource) Configure(_ context.Context, req resource.ConfigureRequest, _ *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	p.config = req.ProviderData.(liqoProviderModel)
}

type peerResourceModel struct {
	// Required kubeconfig paths
	RemoteKubeconfig types.String `tfsdk:"remote_kubeconfig"`

	// Optional parameters matching liqoctl peer command
	Timeout        types.String `tfsdk:"timeout"`
	SkipValidation types.Bool   `tfsdk:"skip_validation"`

	// Liqo namespace configuration
	LiqoNamespace       types.String `tfsdk:"liqo_namespace"`
	RemoteLiqoNamespace types.String `tfsdk:"remote_liqo_namespace"`

	// Networking configuration
	NetworkingDisabled            types.Bool   `tfsdk:"networking_disabled"`
	GwServerServiceType           types.String `tfsdk:"gw_server_service_type"`
	GwServerServicePort           types.Int64  `tfsdk:"gw_server_service_port"`
	GwServerServiceNodeport       types.Int64  `tfsdk:"gw_server_service_nodeport"`
	GwServerServiceLoadbalancerIP types.String `tfsdk:"gw_server_service_loadbalancer_ip"`
	GwClientAddress               types.String `tfsdk:"gw_client_address"`
	GwClientPort                  types.Int64  `tfsdk:"gw_client_port"`
	MTU                           types.Int64  `tfsdk:"mtu"`

	// Authentication configuration
	CreateResourceSlice types.Bool   `tfsdk:"create_resource_slice"`
	ResourceSliceClass  types.String `tfsdk:"resource_slice_class"`
	InBand              types.Bool   `tfsdk:"in_band"`
	ProxyURL            types.String `tfsdk:"proxy_url"`

	// Resource configuration
	CreateVirtualNode types.Bool   `tfsdk:"create_virtual_node"`
	CPU               types.String `tfsdk:"cpu"`
	Memory            types.String `tfsdk:"memory"`
	Pods              types.String `tfsdk:"pods"`

	// Output/status attributes
	PeerStatus types.String `tfsdk:"peer_status"`
}

// downloadLiqoctl downloads the liqoctl executable if it's not available.
func downloadLiqoctl(ctx context.Context, config liqoProviderModel) (string, error) {
	// First check if liqoctl is already in PATH
	if path, err := execCmd.LookPath("liqoctl"); err == nil {
		return path, nil
	}

	// Create a temp directory for liqoctl
	tempDir, err := os.MkdirTemp("", "liqoctl-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}

	// Determine the download URL based on the OS and architecture
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	if goarch == "amd64" {
		goarch = "x86_64"
	}

	// Use the liqo version from provider configuration, fallback to default
	version := LiqoVersion
	if !config.LiqoVersion.IsNull() &&
		!config.LiqoVersion.IsUnknown() &&
		config.LiqoVersion.ValueString() != "" {
		version = config.LiqoVersion.ValueString()
	}
	filename := fmt.Sprintf("liqoctl-%s-%s", goos, goarch)
	if goos == "windows" {
		filename += exeExtension
	}

	downloadURL := fmt.Sprintf("https://github.com/liqotech/liqo/releases/download/%s/%s", version, filename)
	liqoctlPath := filepath.Join(tempDir, "liqoctl")
	if goos == windowsOS {
		liqoctlPath += exeExtension
	}

	// Download the file
	//nolint:gosec // downloadURL is constructed from trusted GitHub API response
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, http.NoBody)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download liqoctl from %s: %w", downloadURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download liqoctl: HTTP %d from %s", resp.StatusCode, downloadURL)
	}

	// Create the file
	//nolint:gosec // liqoctlPath is constructed safely using filepath.Join
	out, err := os.Create(liqoctlPath)
	if err != nil {
		return "", fmt.Errorf("failed to create liqoctl file: %w", err)
	}
	defer out.Close()

	// Copy the downloaded content
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write liqoctl file: %w", err)
	}

	// Make the file executable (Unix-like systems)
	if goos != windowsOS {
		//nolint:gosec // 0o755 permissions are required for liqoctl executable
		err = os.Chmod(liqoctlPath, 0o755)
		if err != nil {
			return "", fmt.Errorf("failed to make liqoctl executable: %w", err)
		}
	}

	return liqoctlPath, nil
}

// buildPeerCommand builds the liqoctl peer command arguments based on the resource model.
//
//nolint:gocyclo // High complexity due to extensive liqoctl peer command configuration options
func buildPeerCommand(plan *peerResourceModel, localKubeconfig string) []string {
	args := []string{"peer"}

	// Add kubeconfig paths
	if localKubeconfig != "" {
		args = append(args, "--kubeconfig", localKubeconfig)
	}
	if !plan.RemoteKubeconfig.IsNull() && plan.RemoteKubeconfig.ValueString() != "" {
		args = append(args, "--remote-kubeconfig", plan.RemoteKubeconfig.ValueString())
	}

	// Add timeout
	if !plan.Timeout.IsNull() && plan.Timeout.ValueString() != "" {
		args = append(args, "--timeout", plan.Timeout.ValueString())
	}

	// Add skip validation
	if !plan.SkipValidation.IsNull() && plan.SkipValidation.ValueBool() {
		args = append(args, "--skip-validation")
	}

	// Add Liqo namespaces
	if !plan.LiqoNamespace.IsNull() && plan.LiqoNamespace.ValueString() != "" {
		args = append(args, "--namespace", plan.LiqoNamespace.ValueString())
	}
	if !plan.RemoteLiqoNamespace.IsNull() && plan.RemoteLiqoNamespace.ValueString() != "" {
		args = append(args, "--remote-namespace", plan.RemoteLiqoNamespace.ValueString())
	}

	// Add networking configuration
	if !plan.NetworkingDisabled.IsNull() && plan.NetworkingDisabled.ValueBool() {
		args = append(args, "--networking-disabled")
	}
	if !plan.GwServerServiceType.IsNull() && plan.GwServerServiceType.ValueString() != "" {
		args = append(args, "--gw-server-service-type", plan.GwServerServiceType.ValueString())
	}
	if !plan.GwServerServicePort.IsNull() {
		args = append(args, "--gw-server-service-port", fmt.Sprintf("%d", plan.GwServerServicePort.ValueInt64()))
	}
	if !plan.GwServerServiceNodeport.IsNull() {
		args = append(args, "--gw-server-service-nodeport", fmt.Sprintf("%d", plan.GwServerServiceNodeport.ValueInt64()))
	}
	if !plan.GwServerServiceLoadbalancerIP.IsNull() && plan.GwServerServiceLoadbalancerIP.ValueString() != "" {
		args = append(args, "--gw-server-service-loadbalancerip", plan.GwServerServiceLoadbalancerIP.ValueString())
	}
	if !plan.GwClientAddress.IsNull() && plan.GwClientAddress.ValueString() != "" {
		args = append(args, "--gw-client-address", plan.GwClientAddress.ValueString())
	}
	if !plan.GwClientPort.IsNull() {
		args = append(args, "--gw-client-port", fmt.Sprintf("%d", plan.GwClientPort.ValueInt64()))
	}
	if !plan.MTU.IsNull() {
		args = append(args, "--mtu", fmt.Sprintf("%d", plan.MTU.ValueInt64()))
	}

	// Add authentication configuration
	if !plan.CreateResourceSlice.IsNull() && !plan.CreateResourceSlice.ValueBool() {
		args = append(args, "--create-resource-slice=false")
	}
	if !plan.ResourceSliceClass.IsNull() && plan.ResourceSliceClass.ValueString() != "" {
		args = append(args, "--resource-slice-class", plan.ResourceSliceClass.ValueString())
	}
	if !plan.InBand.IsNull() && plan.InBand.ValueBool() {
		args = append(args, "--in-band")
	}
	if !plan.ProxyURL.IsNull() && plan.ProxyURL.ValueString() != "" {
		args = append(args, "--proxy-url", plan.ProxyURL.ValueString())
	}

	// Add resource configuration
	if !plan.CreateVirtualNode.IsNull() && !plan.CreateVirtualNode.ValueBool() {
		args = append(args, "--create-virtual-node=false")
	}
	if !plan.CPU.IsNull() && plan.CPU.ValueString() != "" {
		args = append(args, "--cpu", plan.CPU.ValueString())
	}
	if !plan.Memory.IsNull() && plan.Memory.ValueString() != "" {
		args = append(args, "--memory", plan.Memory.ValueString())
	}
	if !plan.Pods.IsNull() && plan.Pods.ValueString() != "" {
		args = append(args, "--pods", plan.Pods.ValueString())
	}

	return args
}

// buildUnpeerCommand builds the liqoctl unpeer command arguments based on the resource model.
func buildUnpeerCommand(data *peerResourceModel, localKubeconfig string) []string {
	args := []string{"unpeer"}

	// Add kubeconfig paths
	if localKubeconfig != "" {
		args = append(args, "--kubeconfig", localKubeconfig)
	}
	if !data.RemoteKubeconfig.IsNull() && data.RemoteKubeconfig.ValueString() != "" {
		args = append(args, "--remote-kubeconfig", data.RemoteKubeconfig.ValueString())
	}

	// Add timeout for unpeer (default 120s)
	if !data.Timeout.IsNull() && data.Timeout.ValueString() != "" {
		args = append(args, "--timeout", data.Timeout.ValueString())
	}

	// Add Liqo namespaces
	if !data.LiqoNamespace.IsNull() && data.LiqoNamespace.ValueString() != "" {
		args = append(args, "--namespace", data.LiqoNamespace.ValueString())
	}
	if !data.RemoteLiqoNamespace.IsNull() && data.RemoteLiqoNamespace.ValueString() != "" {
		args = append(args, "--remote-namespace", data.RemoteLiqoNamespace.ValueString())
	}

	return args
}
