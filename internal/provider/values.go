package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/byteshuttle/allot"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

var refTypes = map[string]attr.Type{"space_key": types.StringType, "space_id": types.StringType, "id": types.StringType}
var resultTypes = map[string]attr.Type{"start": types.StringType, "end": types.StringType, "value": types.StringType, "cidr": types.StringType}
var boundsTypes = map[string]attr.Type{"start": types.StringType, "end": types.StringType}
var rangeTypes = map[string]attr.Type{"start": types.StringType, "end": types.StringType, "start_offset": types.Int64Type, "end_offset": types.Int64Type}
var requestTypes = map[string]attr.Type{"kind": types.StringType, "count": types.StringType, "prefix_length": types.Int64Type}

type reference struct {
	SpaceKey types.String `tfsdk:"space_key"`
	SpaceID  types.String `tfsdk:"space_id"`
	ID       types.String `tfsdk:"id"`
}

func (r reference) root() allot.SpaceRef {
	return allot.SpaceRef{Key: r.SpaceKey.ValueString(), ID: r.SpaceID.ValueString()}
}
func refValue(root allot.SpaceRef, id string) types.Object {
	return types.ObjectValueMust(refTypes, map[string]attr.Value{"space_key": types.StringValue(root.Key), "space_id": types.StringValue(root.ID), "id": types.StringValue(id)})
}
func refFrom(ctx context.Context, v types.Object) (reference, error) {
	var r reference
	if v.IsNull() || v.IsUnknown() {
		return r, errors.New("parent reference is required and must be known")
	}
	d := v.As(ctx, &r, basetypes.ObjectAsOptions{})
	if d.HasError() {
		return r, errors.New(d.Errors()[0].Detail())
	}
	if r.SpaceKey.IsUnknown() || r.SpaceID.IsUnknown() || r.ID.IsUnknown() || r.SpaceKey.ValueString() == "" || r.SpaceID.ValueString() == "" || r.ID.ValueString() == "" {
		return r, errors.New("parent reference must include space_key, space_id and id")
	}
	return r, nil
}
func refSchema(input bool) schema.SingleNestedAttribute {
	attrs := map[string]schema.Attribute{}
	for name := range refTypes {
		description := map[string]string{"space_key": "Exact key of the root space.", "space_id": "UUID of the root allocation.", "id": "UUID of the referenced allocation."}[name]
		attrs[name] = schema.StringAttribute{Required: input, Computed: !input, Description: description}
	}
	s := schema.SingleNestedAttribute{Required: input, Computed: !input, Attributes: attrs, Description: "Allocation reference. Pass a parent resource ref to within."}
	if input {
		s.PlanModifiers = []planmodifier.Object{objectplanmodifier.RequiresReplace()}
	} else {
		s.PlanModifiers = []planmodifier.Object{objectplanmodifier.UseStateForUnknown()}
	}
	return s
}
func resultSchema() schema.SingleNestedAttribute {
	attrs := map[string]schema.Attribute{}
	for name := range resultTypes {
		description := map[string]string{"start": "Inclusive lower bound.", "end": "Inclusive upper bound.", "value": "Singleton value; null for larger intervals.", "cidr": "Aligned IP prefix; null for integers or non-CIDR intervals."}[name]
		attrs[name] = schema.StringAttribute{Computed: true, Description: description}
	}
	return schema.SingleNestedAttribute{Computed: true, Attributes: attrs, Description: "Resolved allocation bounds and values.", PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()}}
}
func resultValue(kind allot.Kind, bounds allot.Range) (types.Object, error) {
	cidr, err := allot.PrefixForRange(kind, bounds)
	if err != nil {
		return types.Object{}, err
	}
	value, prefix := types.StringNull(), types.StringNull()
	if bounds.Start == bounds.End {
		value = types.StringValue(bounds.Start)
	}
	if cidr != "" {
		prefix = types.StringValue(cidr)
	}
	return types.ObjectValueMust(resultTypes, map[string]attr.Value{"start": types.StringValue(bounds.Start), "end": types.StringValue(bounds.End), "value": value, "cidr": prefix}), nil
}
func boundsValue(r allot.Range) types.Object {
	return types.ObjectValueMust(boundsTypes, map[string]attr.Value{"start": types.StringValue(r.Start), "end": types.StringValue(r.End)})
}
func stringOrNull(value string) types.String {
	if value == "" {
		return types.StringNull()
	}
	return types.StringValue(value)
}
func importID(value string) (string, string, error) {
	index := strings.LastIndex(value, "/")
	if index < 1 || index == len(value)-1 {
		return "", "", errors.New("import ID must be SPACE_KEY/ALLOCATION_UUID")
	}
	return value[:index], value[index+1:], nil
}
func operationError(id string, err error) string {
	if errors.Is(err, allot.ErrUncertain) {
		return fmt.Sprintf("Allocation UUID: %s. %v. Inspect the space or explicitly import this UUID before choosing another identity.", id, err)
	}
	return fmt.Sprintf("Allocation UUID: %s. %v.", id, err)
}
