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

// Package planmodifier provides DefaultValue plan modifiers for different attribute types
package planmodifier

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const defaultValueDescription = "Sets the default value if the attribute is not set"

// String DefaultValue plan modifier.
type stringDefaultValuePlanModifier struct {
	DefaultValue types.String
}

// Bool DefaultValue plan modifier.
type boolDefaultValuePlanModifier struct {
	DefaultValue types.Bool
}

// List DefaultValue plan modifier.
type listDefaultValuePlanModifier struct {
	DefaultValue types.List
}

// Map DefaultValue plan modifier.
type mapDefaultValuePlanModifier struct {
	DefaultValue types.Map
}

// DefaultValue creates a plan modifier that sets a default value for string attributes.
func DefaultValue(v types.String) planmodifier.String {
	return &stringDefaultValuePlanModifier{DefaultValue: v}
}

// DefaultBoolValue creates a plan modifier that sets a default value for bool attributes.
func DefaultBoolValue(v types.Bool) planmodifier.Bool {
	return &boolDefaultValuePlanModifier{DefaultValue: v}
}

// DefaultListValue creates a plan modifier that sets a default value for list attributes.
func DefaultListValue(v types.List) planmodifier.List {
	return &listDefaultValuePlanModifier{DefaultValue: v}
}

// DefaultMapValue creates a plan modifier that sets a default value for map attributes.
func DefaultMapValue(v types.Map) planmodifier.Map {
	return &mapDefaultValuePlanModifier{DefaultValue: v}
}

// String plan modifier implementation.
var _ planmodifier.String = (*stringDefaultValuePlanModifier)(nil)

func (m *stringDefaultValuePlanModifier) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (m *stringDefaultValuePlanModifier) MarkdownDescription(_ context.Context) string {
	return fmt.Sprintf("Sets the default value %q if the attribute is not set", m.DefaultValue.ValueString())
}

//nolint:gocritic // req parameter type is required by Terraform plugin framework
func (m *stringDefaultValuePlanModifier) PlanModifyString(_ context.Context, req planmodifier.StringRequest, resp *planmodifier.StringResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}

	if !req.PlanValue.IsUnknown() && !req.PlanValue.IsNull() {
		return
	}

	resp.PlanValue = m.DefaultValue
}

// Bool plan modifier implementation.
var _ planmodifier.Bool = (*boolDefaultValuePlanModifier)(nil)

func (m *boolDefaultValuePlanModifier) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (m *boolDefaultValuePlanModifier) MarkdownDescription(_ context.Context) string {
	return fmt.Sprintf("Sets the default value %t if the attribute is not set", m.DefaultValue.ValueBool())
}

//nolint:gocritic // req parameter type is required by Terraform plugin framework
func (m *boolDefaultValuePlanModifier) PlanModifyBool(_ context.Context, req planmodifier.BoolRequest, resp *planmodifier.BoolResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}

	if !req.PlanValue.IsUnknown() && !req.PlanValue.IsNull() {
		return
	}

	resp.PlanValue = m.DefaultValue
}

// List plan modifier implementation.
var _ planmodifier.List = (*listDefaultValuePlanModifier)(nil)

func (m *listDefaultValuePlanModifier) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (m *listDefaultValuePlanModifier) MarkdownDescription(_ context.Context) string {
	return defaultValueDescription
}

//nolint:gocritic // req parameter type is required by Terraform plugin framework
func (m *listDefaultValuePlanModifier) PlanModifyList(_ context.Context, req planmodifier.ListRequest, resp *planmodifier.ListResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}

	if !req.PlanValue.IsUnknown() && !req.PlanValue.IsNull() {
		return
	}

	resp.PlanValue = m.DefaultValue
}

// Map plan modifier implementation.
var _ planmodifier.Map = (*mapDefaultValuePlanModifier)(nil)

func (m *mapDefaultValuePlanModifier) Description(ctx context.Context) string {
	return m.MarkdownDescription(ctx)
}

func (m *mapDefaultValuePlanModifier) MarkdownDescription(_ context.Context) string {
	return defaultValueDescription
}

//nolint:gocritic // req parameter type is required by Terraform plugin framework
func (m *mapDefaultValuePlanModifier) PlanModifyMap(_ context.Context, req planmodifier.MapRequest, resp *planmodifier.MapResponse) {
	if !req.ConfigValue.IsNull() {
		return
	}

	if !req.PlanValue.IsUnknown() && !req.PlanValue.IsNull() {
		return
	}

	resp.PlanValue = m.DefaultValue
}
