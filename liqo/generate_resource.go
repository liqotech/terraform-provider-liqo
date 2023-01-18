package liqo

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/liqotech/liqo/pkg/auth"
	"github.com/liqotech/liqo/pkg/utils"
	foreigncluster "github.com/liqotech/liqo/pkg/utils/foreignCluster"
	planmodifier "github.com/liqotech/terraform-provider-liqo/liqo/attribute_plan_modifier"
)

var (
	_ resource.Resource              = &generateResource{}
	_ resource.ResourceWithConfigure = &generateResource{}
)

// NewGenerateResource provides the initialization of Generate Resource.
func NewGenerateResource() resource.Resource {
	return &generateResource{}
}

type generateResource struct {
	config     liqoProviderModel
	CRClient   client.Client
	KubeClient *kubernetes.Clientset
}

func (r *generateResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_generate"
}

func (r *generateResource) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Generate peering parameters for remote clusters",
		Attributes: map[string]tfsdk.Attribute{
			"cluster_id": {
				Type:        types.StringType,
				Computed:    true,
				Description: "Provider cluster ID.",
			},
			"cluster_name": {
				Type:        types.StringType,
				Computed:    true,
				Description: "Provider cluster name.",
			},
			"auth_ep": {
				Type:        types.StringType,
				Computed:    true,
				Description: "Provider authentication endpoint.",
			},
			"local_token": {
				Type:        types.StringType,
				Computed:    true,
				Description: "Provider authentication token.",
			},
			"liqo_namespace": {
				Type:     types.StringType,
				Optional: true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					planmodifier.DefaultValue(types.StringValue("liqo")),
				},
				Computed:    true,
				Description: "Namespace where is Liqo installed in provider cluster.",
			},
		},
	}, nil
}

// Creation of Generate Resource to obtain necessary pairing parameters used by Peer Resources
// This resource will reproduce the same effect and outputs of "liqoctl generate peer-command" command.
//
//nolint:gocritic // Terraform Framework template code
func (r *generateResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan generateResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	overrides, loader, err := CheckParameters(&r.config)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	CRClient, KubeClient, err := NewClients(overrides, loader)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	r.CRClient = CRClient
	r.KubeClient = KubeClient

	clusterIdentity, err := utils.GetClusterIdentityWithControllerClient(ctx, r.CRClient, plan.LiqoNamespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	localToken, err := auth.GetToken(ctx, r.CRClient, plan.LiqoNamespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	authEP, err := foreigncluster.GetHomeAuthURL(ctx, r.CRClient, plan.LiqoNamespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	if clusterIdentity.ClusterName == "" {
		clusterIdentity.ClusterName = clusterIdentity.ClusterID
	}

	plan.ClusterID = types.StringValue(clusterIdentity.ClusterID)
	plan.ClusterName = types.StringValue(clusterIdentity.ClusterName)
	plan.LocalToken = types.StringValue(localToken)
	plan.AuthEP = types.StringValue(authEP)

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
}

//nolint:gocritic // Terraform Framework template code
func (r *generateResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state generateResourceModel
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
func (r *generateResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unable to Update Resource",
		"Update is not supported/permitted yet.",
	)
}

//nolint:gocritic // Terraform Framework template code
func (r *generateResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
}

// Configure method to obtain kubernetes Clients provided by provider.
func (r *generateResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	r.config = req.ProviderData.(liqoProviderModel)
}

type generateResourceModel struct {
	ClusterID     types.String `tfsdk:"cluster_id"`
	ClusterName   types.String `tfsdk:"cluster_name"`
	AuthEP        types.String `tfsdk:"auth_ep"`
	LocalToken    types.String `tfsdk:"local_token"`
	LiqoNamespace types.String `tfsdk:"liqo_namespace"`
}
