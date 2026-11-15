package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/byteshuttle/allot"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type spaceResource struct{ resourceClient }
type spaceModel struct {
	ID     types.String `tfsdk:"id"`
	Key    types.String `tfsdk:"key"`
	Kind   types.String `tfsdk:"kind"`
	CIDR   types.String `tfsdk:"cidr"`
	Range  types.Object `tfsdk:"range"`
	Ref    types.Object `tfsdk:"ref"`
	Result types.Object `tfsdk:"result"`
}
type boundsModel struct {
	Start types.String `tfsdk:"start"`
	End   types.String `tfsdk:"end"`
}

func (*spaceResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_space"
}
func (*spaceResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "A root IPv4, IPv6 or integer allocation space.", Attributes: map[string]schema.Attribute{
		"id":    schema.StringAttribute{Computed: true, Description: "Immutable allocation UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"key":   schema.StringAttribute{Required: true, Description: "Exact root key, unique in the storage namespace. Changes require replacement.", PlanModifiers: []planmodifier.String{stringplanmodifier.RequiresReplace()}},
		"kind":  schema.StringAttribute{Optional: true, Computed: true, Description: "ipv4, ipv6 or integer; inferred from cidr when omitted.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplaceIfConfigured()}},
		"cidr":  schema.StringAttribute{Optional: true, Computed: true, Description: "Aligned IP prefix. For explicit bounds, computed when the range is CIDR-aligned.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplaceIfConfigured()}},
		"range": schema.SingleNestedAttribute{Optional: true, Computed: true, Description: "Inclusive bounds; inferred from cidr when omitted.", Attributes: map[string]schema.Attribute{"start": schema.StringAttribute{Required: true, Description: "Inclusive lower bound as an IP or decimal integer string."}, "end": schema.StringAttribute{Required: true, Description: "Inclusive upper bound as an IP or decimal integer string."}}, PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown(), objectplanmodifier.RequiresReplaceIfConfigured()}},
		"ref":   refSchema(false), "result": resultSchema(),
	}}
}
func spaceSpec(ctx context.Context, m spaceModel, id string) (allot.Space, error) {
	s := allot.Space{ID: id, Key: m.Key.ValueString(), Kind: allot.Kind(m.Kind.ValueString())}
	if !m.CIDR.IsNull() && !m.CIDR.IsUnknown() {
		kind, bounds, err := allot.PrefixRange(m.CIDR.ValueString())
		if err != nil {
			return s, err
		}
		if s.Kind != "" && s.Kind != kind {
			return s, fmt.Errorf("kind does not match cidr")
		}
		s.Kind, s.Range = kind, bounds
	}
	if !m.Range.IsNull() && !m.Range.IsUnknown() {
		var b boundsModel
		d := m.Range.As(ctx, &b, basetypes.ObjectAsOptions{})
		if d.HasError() {
			return s, errors.New(d.Errors()[0].Detail())
		}
		bounds := allot.Range{Start: b.Start.ValueString(), End: b.End.ValueString()}
		if s.Range.Start != "" && s.Range != bounds {
			return s, errors.New("cidr and range describe different bounds")
		}
		s.Range = bounds
	}
	if s.Range.Start == "" || s.Range.End == "" {
		return s, errors.New("configure cidr or a range with kind")
	}
	t, err := allot.New(s)
	if err != nil {
		return s, err
	}
	return t.Space(), nil
}
func (*spaceResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m spaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if m.Key.IsUnknown() || m.Kind.IsUnknown() || m.CIDR.IsUnknown() || m.Range.IsUnknown() {
		return
	}
	if !m.Range.IsNull() {
		for _, v := range m.Range.Attributes() {
			if v.IsUnknown() {
				return
			}
		}
	}
	if _, err := spaceSpec(ctx, m, "00000000-0000-4000-8000-000000000001"); err != nil {
		resp.Diagnostics.AddError("Invalid allocation space", err.Error())
	}
}
func fillSpace(m *spaceModel, s allot.Space) error {
	m.ID, m.Key, m.Kind = types.StringValue(s.ID), types.StringValue(s.Key), types.StringValue(string(s.Kind))
	if m.CIDR.IsNull() || m.CIDR.IsUnknown() {
		prefix, err := allot.PrefixForRange(s.Kind, s.Range)
		if err != nil {
			return err
		}
		m.CIDR = stringOrNull(prefix)
	}
	if m.Range.IsNull() || m.Range.IsUnknown() {
		m.Range = boundsValue(s.Range)
	}
	m.Ref = refValue(s.Ref(), s.ID)
	var err error
	m.Result, err = resultValue(s.Kind, s.Range)
	return err
}
func (r *spaceResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m spaceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := allot.NewID()
	s, err := spaceSpec(ctx, m, id)
	if err != nil {
		resp.Diagnostics.AddError("Invalid allocation space", err.Error())
		return
	}
	if err = fillSpace(&m, s); err != nil {
		resp.Diagnostics.AddError("Cannot describe space", err.Error())
		return
	}
	_, err = r.client.Create(ctx, s)
	if err != nil {
		if errors.Is(err, allot.ErrUncertain) {
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		}
		resp.Diagnostics.AddError("Cannot create allocation space", operationError(id, err))
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *spaceResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m spaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	tree, err := r.client.Inspect(ctx, m.Key.ValueString())
	if errors.Is(err, allot.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read allocation space", err.Error())
		return
	}
	_, err = tree.Lookup(m.ID.ValueString())
	if errors.Is(err, allot.ErrReleased) {
		resp.State.RemoveResource(ctx)
		return
	}
	if tree.Space().ID != m.ID.ValueString() {
		resp.Diagnostics.AddError("Root UUID differs", "The configured store contains a different root UUID; it will not be adopted automatically.")
		return
	}
	if err = fillSpace(&m, tree.Space()); err != nil {
		resp.Diagnostics.AddError("Cannot describe space", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *spaceResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m spaceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	err := r.client.Release(ctx, allot.SpaceRef{Key: m.Key.ValueString(), ID: m.ID.ValueString()}, m.ID.ValueString())
	if err != nil && !errors.Is(err, allot.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot release allocation space", operationError(m.ID.ValueString(), err))
	}
}
func (r *spaceResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	key, id, err := importID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), id)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("key"), key)...)
}
func (*spaceResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() || len(resp.RequiresReplace) == 0 {
		return
	}
	var config spaceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), types.StringUnknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ref"), types.ObjectUnknown(refTypes))...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("result"), types.ObjectUnknown(resultTypes))...)
	if config.CIDR.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("cidr"), types.StringUnknown())...)
	}
	if config.Range.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("range"), types.ObjectUnknown(boundsTypes))...)
	}
	if config.Kind.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("kind"), types.StringUnknown())...)
	}
}

var _ resource.ResourceWithConfigure = (*spaceResource)(nil)
var _ resource.ResourceWithImportState = (*spaceResource)(nil)
var _ resource.ResourceWithValidateConfig = (*spaceResource)(nil)
var _ resource.ResourceWithModifyPlan = (*spaceResource)(nil)
