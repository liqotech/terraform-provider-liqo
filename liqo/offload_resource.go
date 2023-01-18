package liqo

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	offloadingv1alpha1 "github.com/liqotech/liqo/apis/offloading/v1alpha1"
	"github.com/liqotech/liqo/pkg/consts"
	planmodifier "github.com/liqotech/terraform-provider-liqo/liqo/attribute_plan_modifier"
)

var (
	_ resource.Resource              = &offloadResource{}
	_ resource.ResourceWithConfigure = &offloadResource{}
)

// NewOffloadResource provides the initialization of Offload Resource.
func NewOffloadResource() resource.Resource {
	return &offloadResource{}
}

type offloadResource struct {
	config liqoProviderModel
}

func (o *offloadResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_offload"
}

func (o *offloadResource) GetSchema(_ context.Context) (tfsdk.Schema, diag.Diagnostics) {
	return tfsdk.Schema{
		Description: "Offload a namespace.",
		Attributes: map[string]tfsdk.Attribute{
			"namespace": {
				Type:        types.StringType,
				Required:    true,
				Description: "Offload a namespace.",
			},
			"pod_offloading_strategy": {
				Type:     types.StringType,
				Optional: true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					planmodifier.DefaultValue(types.StringValue("LocalAndRemote")),
				},
				Computed:    true,
				Description: "Namespace to offload.",
			},
			"namespace_mapping_strategy": {
				Type:     types.StringType,
				Optional: true,
				PlanModifiers: []tfsdk.AttributePlanModifier{
					planmodifier.DefaultValue(types.StringValue("DefaultName")),
				},
				Computed:    true,
				Description: "Naming strategy used to create the remote namespace.",
			},
			"cluster_selector_terms": {
				Optional: true,
				Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
					"match_expressions": {
						Optional: true,
						Computed: true,
						Attributes: tfsdk.ListNestedAttributes(map[string]tfsdk.Attribute{
							"key": {
								Type:        types.StringType,
								Required:    true,
								Description: " The label key that the selector applies to.",
							},
							"operator": {
								Type:        types.StringType,
								Required:    true,
								Description: "Represents a key's relationship to a set of values.",
							},
							"values": {
								Type:        types.ListType{ElemType: types.StringType},
								Optional:    true,
								Description: "An array of string values.",
							},
						}),
						Description: "A list of cluster selector.",
					},
				}),
				Description: "Selectors to restrict the set of remote clusters.",
			},
		},
	}, nil
}

// Creation of Offload Resource to offload a specific namespace,
// additionally there is a possibility to select clusters with match_expressione
// This resource will reproduce the same effect and outputs of "liqoctl offload" command.
//
//nolint:gocritic // Terraform Framework template code
func (o *offloadResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan offloadResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	overrides, loader, err := CheckParameters(&o.config)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	CRClient, _, err := NewClients(overrides, loader)
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Resource",
			err.Error(),
		)
		return
	}

	var clusterSelector [][]metav1.LabelSelectorRequirement

	for _, selector := range plan.ClusterSelectorTerms {
		s := &metav1.LabelSelector{
			MatchLabels:      map[string]string{},
			MatchExpressions: []metav1.LabelSelectorRequirement{},
		}

		for _, matchExpression := range selector.MatchExpressions {
			var values []string

			for _, value := range matchExpression.Values {
				values = append(values, value.ValueString())
			}
			req := metav1.LabelSelectorRequirement{
				Key:      matchExpression.Key.ValueString(),
				Operator: metav1.LabelSelectorOperator(matchExpression.Operator.ValueString()),
				Values:   values,
			}
			s.MatchExpressions = append(s.MatchExpressions, req)
		}

		clusterSelector = append(clusterSelector, s.MatchExpressions)
	}

	terms := []corev1.NodeSelectorTerm{}

	for _, selector := range clusterSelector {
		var requirements []corev1.NodeSelectorRequirement

		for _, r := range selector {
			requirements = append(requirements, corev1.NodeSelectorRequirement{
				Key:      r.Key,
				Operator: corev1.NodeSelectorOperator(r.Operator),
				Values:   r.Values,
			})
		}

		terms = append(terms, corev1.NodeSelectorTerm{MatchExpressions: requirements})
	}

	nsoff := &offloadingv1alpha1.NamespaceOffloading{ObjectMeta: metav1.ObjectMeta{
		Name: consts.DefaultNamespaceOffloadingName, Namespace: plan.Namespace.ValueString()}}

	_, err = controllerutil.CreateOrUpdate(ctx, CRClient, nsoff, func() error {
		nsoff.Spec.PodOffloadingStrategy = offloadingv1alpha1.PodOffloadingStrategyType(plan.PodOffloadingStrategy.ValueString())
		nsoff.Spec.NamespaceMappingStrategy = offloadingv1alpha1.NamespaceMappingStrategyType(plan.NamespaceMappingStrategy.ValueString())
		nsoff.Spec.ClusterSelector = corev1.NodeSelector{NodeSelectorTerms: terms}
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
func (o *offloadResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state offloadResourceModel
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
func (o *offloadResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"Unable to Update Resource",
		"Update is not supported/permitted yet.",
	)
}

//nolint:gocritic // Terraform Framework template code
func (o *offloadResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var data offloadResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)

	overrides, loader, err := CheckParameters(&o.config)
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

	nsoff := &offloadingv1alpha1.NamespaceOffloading{ObjectMeta: metav1.ObjectMeta{
		Name: consts.DefaultNamespaceOffloadingName, Namespace: data.Namespace.ValueString()}}
	if err := CRClient.Delete(ctx, nsoff); client.IgnoreNotFound(err) != nil {
		resp.Diagnostics.AddError(
			"Unable to Delete Resource",
			err.Error(),
		)
		return
	}
}

// Configure method to obtain kubernetes Clients provided by provider.
func (o *offloadResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	o.config = req.ProviderData.(liqoProviderModel)
}

type matchExpression struct {
	Key      types.String   `tfsdk:"key"`
	Operator types.String   `tfsdk:"operator"`
	Values   []types.String `tfsdk:"values"`
}

type matchExpressions struct {
	MatchExpressions []matchExpression `tfsdk:"match_expressions"`
}

type offloadResourceModel struct {
	Namespace                types.String       `tfsdk:"namespace"`
	PodOffloadingStrategy    types.String       `tfsdk:"pod_offloading_strategy"`
	NamespaceMappingStrategy types.String       `tfsdk:"namespace_mapping_strategy"`
	ClusterSelectorTerms     []matchExpressions `tfsdk:"cluster_selector_terms"`
}
