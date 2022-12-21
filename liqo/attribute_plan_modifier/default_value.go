// Package planmodifier provides a DefaultValue to attributes in provider
package planmodifier

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
)

type defaultValueAttributePlanModifier struct {
	DefaultValue attr.Value
}

// DefaultValue used to set a DefaultValue.
func DefaultValue(v attr.Value) tfsdk.AttributePlanModifier {
	return &defaultValueAttributePlanModifier{v}
}

var _ tfsdk.AttributePlanModifier = (*defaultValueAttributePlanModifier)(nil)

func (apm *defaultValueAttributePlanModifier) Description(ctx context.Context) string {
	return apm.MarkdownDescription(ctx)
}

func (apm *defaultValueAttributePlanModifier) MarkdownDescription(ctx context.Context) string {
	return fmt.Sprintf("Sets the default value %q (%s) if the attribute is not set", apm.DefaultValue, apm.DefaultValue.Type(ctx))
}

//nolint:gocritic,lll // Terraform Framework template code
func (apm *defaultValueAttributePlanModifier) Modify(_ context.Context, req tfsdk.ModifyAttributePlanRequest, res *tfsdk.ModifyAttributePlanResponse) {
	if !req.AttributeConfig.IsNull() {
		return
	}

	if !req.AttributePlan.IsUnknown() && !req.AttributePlan.IsNull() {
		return
	}

	res.AttributePlan = apm.DefaultValue
}
