package liqo

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	discoveryv1alpha1 "github.com/liqotech/liqo/apis/discovery/v1alpha1"
	"github.com/liqotech/liqo/pkg/discovery"
	"github.com/liqotech/liqo/pkg/utils"
	authenticationtokenutils "github.com/liqotech/liqo/pkg/utils/authenticationtoken"
	foreigncluster "github.com/liqotech/liqo/pkg/utils/foreignCluster"
	planmodifier "github.com/liqotech/terraform-provider-liqo/liqo/attribute_plan_modifier"
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

func (p *peerResource) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Execute peering.",
		Attributes: map[string]tfsdk.Attribute{
			"cluster_id": {
				Type:        types.StringType,
				Required:    true,
				Description: "Provider cluster ID used for peering.",
			},
			"cluster_name": {
				Type:        types.StringType,
				Required:    true,
				Description: "Provider cluster name used for peering.",
			},
			"cluster_authurl": {
				Type:        types.StringType,
				Required:    true,
				Description: "Provider authentication url used for peering.",
			},
			"cluster_token": {
				Type:        types.StringType,
				Required:    true,
				Description: "Provider authentication token used for peering.",
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

// Creation of Peer Resource to execute peering between two clusters using auth parameters provided by Generate Resource
// This resource will reproduce the same effect and outputs of "liqoctl peer out-of-band" command.
//
//nolint:gocritic // Terraform Framework template code
func (p *peerResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan peerResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	overrides, loader, err := CheckParameters(&p.config)
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

	clusterIdentity, err := utils.GetClusterIdentityWithControllerClient(ctx, CRClient, plan.LiqoNamespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	if clusterIdentity.ClusterID == plan.ClusterID.ValueString() {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			"The Cluster ID of the remote cluster is the same of that of the local cluster",
		)
		return
	}

	//nolint:lll // Long due to method invocation parameters.
	err = authenticationtokenutils.StoreInSecret(ctx, KubeClient, plan.ClusterID.ValueString(), plan.ClusterToken.ValueString(), plan.LiqoNamespace.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	fc, err := foreigncluster.GetForeignClusterByID(ctx, CRClient, plan.ClusterID.ValueString())
	if kerrors.IsNotFound(err) {
		fc = &discoveryv1alpha1.ForeignCluster{ObjectMeta: metav1.ObjectMeta{Name: plan.ClusterName.ValueString(),
			Labels: map[string]string{discovery.ClusterIDLabel: plan.ClusterID.ValueString()}}}
	} else if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	_, err = controllerutil.CreateOrUpdate(ctx, CRClient, fc, func() error {
		if fc.Spec.PeeringType != discoveryv1alpha1.PeeringTypeUnknown && fc.Spec.PeeringType != discoveryv1alpha1.PeeringTypeOutOfBand {
			return fmt.Errorf("a peering of type %s already exists towards remote cluster %q, cannot be changed to %s",
				fc.Spec.PeeringType, plan.ClusterName.ValueString(), discoveryv1alpha1.PeeringTypeOutOfBand)
		}

		fc.Spec.PeeringType = discoveryv1alpha1.PeeringTypeOutOfBand
		fc.Spec.ClusterIdentity.ClusterID = plan.ClusterID.ValueString()
		if fc.Spec.ClusterIdentity.ClusterName == "" {
			fc.Spec.ClusterIdentity.ClusterName = plan.ClusterName.ValueString()
		}

		fc.Spec.ForeignAuthURL = plan.ClusterAuthURL.ValueString()
		fc.Spec.ForeignProxyURL = ""
		fc.Spec.OutgoingPeeringEnabled = discoveryv1alpha1.PeeringEnabledYes
		if fc.Spec.IncomingPeeringEnabled == "" {
			fc.Spec.IncomingPeeringEnabled = discoveryv1alpha1.PeeringEnabledAuto
		}
		if fc.Spec.InsecureSkipTLSVerify == nil {
			fc.Spec.InsecureSkipTLSVerify = pointer.BoolPtr(true)
		}
		return nil
	})

	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)

		return
	}

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
func (p *peerResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unable to Update Resource",
		"Update is not supported/permitted yet.",
	)
}

//nolint:gocritic // Terraform Framework template code
func (p *peerResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data peerResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	overrides, loader, err := CheckParameters(&p.config)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Delete Resource",
			err.Error(),
		)
		return
	}

	CRClient, _, err := NewClients(overrides, loader)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Delete Resource",
			err.Error(),
		)
		return
	}

	var foreignCluster discoveryv1alpha1.ForeignCluster
	if err := CRClient.Get(ctx, kubeTypes.NamespacedName{Name: data.ClusterName.ValueString()}, &foreignCluster); err != nil {
		return
	}

	if foreignCluster.Spec.PeeringType != discoveryv1alpha1.PeeringTypeOutOfBand {
		return
	}

	foreignCluster.Spec.OutgoingPeeringEnabled = discoveryv1alpha1.PeeringEnabledNo
	if err := CRClient.Update(ctx, &foreignCluster); err != nil {
		resp.Diagnostics.AddError(
			"Unable to Delete Resource",
			err.Error(),
		)
		return
	}
}

// Configure method to obtain kubernetes Clients provided by provider.
func (p *peerResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	p.config = req.ProviderData.(liqoProviderModel)
}

type peerResourceModel struct {
	ClusterID      types.String `tfsdk:"cluster_id"`
	ClusterName    types.String `tfsdk:"cluster_name"`
	ClusterAuthURL types.String `tfsdk:"cluster_authurl"`
	ClusterToken   types.String `tfsdk:"cluster_token"`
	LiqoNamespace  types.String `tfsdk:"liqo_namespace"`
}
