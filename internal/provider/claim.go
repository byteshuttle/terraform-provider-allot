package provider

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/netip"
	"reflect"

	"github.com/byteshuttle/allot"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type claimResource struct{ resourceClient }
type claimModel struct {
	ID      types.String `tfsdk:"id"`
	Key     types.String `tfsdk:"key"`
	Within  types.Object `tfsdk:"within"`
	CIDR    types.String `tfsdk:"cidr"`
	Value   types.String `tfsdk:"value"`
	Offset  types.Int64  `tfsdk:"offset"`
	Range   types.Object `tfsdk:"range"`
	Request types.Object `tfsdk:"request"`
	Ref     types.Object `tfsdk:"ref"`
	Result  types.Object `tfsdk:"result"`
	Tags    types.Map    `tfsdk:"tags"`
	TagsAll types.Map    `tfsdk:"tags_all"`
}
type claimRange struct {
	Start       types.String `tfsdk:"start"`
	End         types.String `tfsdk:"end"`
	StartOffset types.Int64  `tfsdk:"start_offset"`
	EndOffset   types.Int64  `tfsdk:"end_offset"`
}
type claimRequest struct {
	Kind         types.String `tfsdk:"kind"`
	Count        types.String `tfsdk:"count"`
	PrefixLength types.Int64  `tfsdk:"prefix_length"`
}

func (*claimResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_claim"
}
func (*claimResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	rangeAttrs := map[string]schema.Attribute{"start": schema.StringAttribute{Optional: true, Description: "Inclusive absolute lower bound."}, "end": schema.StringAttribute{Optional: true, Description: "Inclusive absolute upper bound."}, "start_offset": schema.Int64Attribute{Optional: true, Description: "Inclusive lower offset; negative values count from the end."}, "end_offset": schema.Int64Attribute{Optional: true, Description: "Inclusive upper offset; negative values count from the end."}}
	resp.Schema = schema.Schema{Description: "A UUID-identified allocation within a root or parent claim.", Attributes: map[string]schema.Attribute{
		"id":     schema.StringAttribute{Computed: true, Description: "Immutable allocation UUID.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
		"key":    schema.StringAttribute{Optional: true, Computed: true, Description: "Optional logical key. Defaults to the claim UUID; Name is an ordinary tag.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown(), stringplanmodifier.RequiresReplaceIfConfigured()}},
		"within": refSchema(true),
		"cidr":   schema.StringAttribute{Optional: true, Description: "Exact aligned IP prefix to claim."},
		"value":  schema.StringAttribute{Optional: true, Description: "Exact address or decimal integer to claim, as a string."},
		"offset": schema.Int64Attribute{Optional: true, Description: "Zero-based offset; negative values count from the end."},
		"range":  schema.SingleNestedAttribute{Optional: true, Description: "Inclusive start/end or start_offset/end_offset pair.", Attributes: rangeAttrs},
		"request": schema.SingleNestedAttribute{Optional: true, Computed: true, Description: "Automatic selection. Omitted defaults to one available value.", Attributes: map[string]schema.Attribute{
			"kind":  schema.StringAttribute{Optional: true, Computed: true, Description: "Inferred as value, range or prefix. If set, must match the request.", PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()}},
			"count": schema.StringAttribute{Optional: true, Description: "Number of contiguous values, as a positive decimal string; excludes prefix_length."}, "prefix_length": schema.Int64Attribute{Optional: true, Description: "Automatic aligned IP prefix length; excludes count."},
		}, PlanModifiers: []planmodifier.Object{objectplanmodifier.UseStateForUnknown()}},
		"tags":     schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Resource tags, including optional Name. Matching keys override provider default tags."},
		"tags_all": schema.MapAttribute{Computed: true, ElementType: types.StringType, Description: "All stored tags, including provider defaults and resource overrides."},
		"ref":      refSchema(false), "result": resultSchema(),
	}}
}
func selection(ctx context.Context, m claimModel) (allot.Selection, error) {
	var s allot.Selection
	count := 0
	if !m.CIDR.IsNull() {
		count++
		s.Prefix = m.CIDR.ValueString()
		if s.Prefix == "" {
			return s, errors.New("cidr cannot be empty")
		}
	}
	if !m.Value.IsNull() {
		count++
		s.Value = m.Value.ValueString()
		if s.Value == "" {
			return s, errors.New("value cannot be empty")
		}
	}
	if !m.Offset.IsNull() {
		count++
		v := m.Offset.ValueInt64()
		s.Offset = &v
	}
	if !m.Range.IsNull() {
		count++
		var b claimRange
		d := m.Range.As(ctx, &b, basetypes.ObjectAsOptions{})
		if d.HasError() {
			return s, errors.New(d.Errors()[0].Detail())
		}
		absolute := !b.Start.IsNull() || !b.End.IsNull()
		relative := !b.StartOffset.IsNull() || !b.EndOffset.IsNull()
		if absolute == relative {
			return s, errors.New("range requires either start/end or start_offset/end_offset")
		}
		if absolute {
			if b.Start.IsNull() || b.End.IsNull() {
				return s, errors.New("both start and end are required")
			}
			s.Range = &allot.Range{Start: b.Start.ValueString(), End: b.End.ValueString()}
		} else {
			if b.StartOffset.IsNull() || b.EndOffset.IsNull() {
				return s, errors.New("both offsets are required")
			}
			s.Offsets = &allot.Offsets{Start: b.StartOffset.ValueInt64(), End: b.EndOffset.ValueInt64()}
		}
	}
	if !m.Request.IsNull() && !m.Request.IsUnknown() {
		count++
		var q claimRequest
		d := m.Request.As(ctx, &q, basetypes.ObjectAsOptions{})
		if d.HasError() {
			return s, errors.New(d.Errors()[0].Detail())
		}
		a := &allot.Automatic{}
		if !q.Count.IsNull() {
			a.Count = q.Count.ValueString()
			if a.Count == "" {
				return s, errors.New("count cannot be empty")
			}
		}
		if !q.PrefixLength.IsNull() {
			v := q.PrefixLength.ValueInt64()
			if v < 0 || v > 128 {
				return s, errors.New("prefix_length must be between 0 and 128")
			}
			n := int(v)
			a.PrefixLength = &n
		}
		if a.Count != "" && a.PrefixLength != nil {
			return s, errors.New("count and prefix_length are exclusive")
		}
		kind := "value"
		if a.Count != "" {
			kind = "range"
		}
		if a.PrefixLength != nil {
			kind = "prefix"
		}
		if !q.Kind.IsNull() && !q.Kind.IsUnknown() && q.Kind.ValueString() != kind {
			return s, fmt.Errorf("request kind must be %s for these arguments", kind)
		}
		s.Automatic = a
	}
	if count > 1 {
		return s, errors.New("configure only one of cidr, value, offset, range or request")
	}
	if count == 0 {
		s.Automatic = &allot.Automatic{}
	}
	return s, nil
}
func (*claimResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var m claimModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for _, v := range []attr.Value{m.CIDR, m.Value, m.Offset, m.Range, m.Request} {
		if v.IsUnknown() {
			return
		}
		if obj, ok := v.(types.Object); ok && !obj.IsNull() {
			for _, nested := range obj.Attributes() {
				if nested.IsUnknown() {
					return
				}
			}
		}
	}
	if _, err := selection(ctx, m); err != nil {
		resp.Diagnostics.AddError("Invalid allocation request", err.Error())
	}
}
func setSelection(m *claimModel, s allot.Selection) {
	m.CIDR, m.Value = stringOrNull(s.Prefix), stringOrNull(s.Value)
	m.Offset = types.Int64Null()
	m.Range = types.ObjectNull(rangeTypes)
	m.Request = types.ObjectNull(requestTypes)
	if s.Offset != nil {
		m.Offset = types.Int64Value(*s.Offset)
	}
	if s.Range != nil {
		m.Range = types.ObjectValueMust(rangeTypes, map[string]attr.Value{"start": types.StringValue(s.Range.Start), "end": types.StringValue(s.Range.End), "start_offset": types.Int64Null(), "end_offset": types.Int64Null()})
	}
	if s.Offsets != nil {
		m.Range = types.ObjectValueMust(rangeTypes, map[string]attr.Value{"start": types.StringNull(), "end": types.StringNull(), "start_offset": types.Int64Value(s.Offsets.Start), "end_offset": types.Int64Value(s.Offsets.End)})
	}
	if s.Automatic != nil {
		kind, count, prefix := "value", types.StringNull(), types.Int64Null()
		if s.Automatic.Count != "" {
			kind = "range"
			count = types.StringValue(s.Automatic.Count)
		}
		if s.Automatic.PrefixLength != nil {
			kind = "prefix"
			prefix = types.Int64Value(int64(*s.Automatic.PrefixLength))
		}
		m.Request = types.ObjectValueMust(requestTypes, map[string]attr.Value{"kind": types.StringValue(kind), "count": count, "prefix_length": prefix})
	}
}
func fillClaim(m *claimModel, root allot.Space, a allot.Allocation) error {
	m.ID, m.Key = types.StringValue(a.ID), types.StringValue(a.Key)
	m.Within = refValue(root.Ref(), a.ParentID)
	m.Ref = refValue(root.Ref(), a.ID)
	if a.Selection == nil {
		return errors.New("the selected UUID identifies a root, not a child claim")
	}
	setSelection(m, *a.Selection)
	var err error
	m.Result, err = resultValue(root.Kind, a.Range)
	return err
}
func (r *claimResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var m claimModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parent, err := refFrom(ctx, m.Within)
	if err != nil {
		resp.Diagnostics.AddError("Invalid parent reference", err.Error())
		return
	}
	s, err := selection(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Invalid allocation request", err.Error())
		return
	}
	tags, err := resourceTags(ctx, m.Tags, r.defaultTags)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tags", err.Error())
		return
	}
	m.TagsAll, err = tagsAllValue(ctx, tags)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tags", err.Error())
		return
	}
	tree, err := r.client.Load(ctx, parent.root())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read allocation space", err.Error())
		return
	}
	id := allot.NewID()
	if m.Key.IsNull() || m.Key.IsUnknown() {
		m.Key = types.StringValue(id)
	}
	claim, err := r.client.Allocate(ctx, parent.root(), parent.ID.ValueString(), allot.Request{ID: id, Key: m.Key.ValueString(), Select: s, Tags: tags})
	if err != nil {
		if errors.Is(err, allot.ErrUncertain) {
			m.ID = types.StringValue(id)
			m.Ref = refValue(parent.root(), id)
			m.Result = types.ObjectNull(resultTypes)
			setSelection(&m, s)
			resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
		}
		resp.Diagnostics.AddError("Cannot allocate claim", operationError(id, err))
		return
	}
	if err = fillClaim(&m, tree.Space(), allot.Allocation{Claim: claim, ParentID: parent.ID.ValueString(), Selection: &s}); err != nil {
		resp.Diagnostics.AddError("Cannot describe claim", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *claimResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var m claimModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parent, err := refFrom(ctx, m.Within)
	if err != nil {
		resp.Diagnostics.AddError("Invalid saved reference", err.Error())
		return
	}
	tree, err := r.client.Inspect(ctx, parent.SpaceID.ValueString())
	if errors.Is(err, allot.ErrNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot read claim", err.Error())
		return
	}
	a, err := tree.Describe(m.ID.ValueString())
	if errors.Is(err, allot.ErrNotFound) || errors.Is(err, allot.ErrReleased) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError("Cannot inspect claim", err.Error())
		return
	}
	if tree.Space().ID != parent.SpaceID.ValueString() {
		resp.Diagnostics.AddError("Root UUID differs", "The allocation is not in the root recorded in state; it will not be adopted automatically.")
		return
	}
	saved, savedErr := selection(ctx, m)
	if err = fillClaim(&m, tree.Space(), a); err != nil {
		resp.Diagnostics.AddError("Cannot describe claim", err.Error())
		return
	}
	if savedErr == nil && automaticFits(saved, m.Result) {
		setSelection(&m, saved)
	}
	storedTags, err := tree.Tags(m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read tags", err.Error())
		return
	}
	m.Tags, m.TagsAll, err = readResourceTags(ctx, storedTags, m.Tags, m.TagsAll, r.defaultTags)
	if err != nil {
		resp.Diagnostics.AddError("Cannot read tags", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}
func (r *claimResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var m claimModel
	resp.Diagnostics.Append(req.State.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	parent, err := refFrom(ctx, m.Within)
	if err != nil {
		resp.Diagnostics.AddError("Invalid saved reference", err.Error())
		return
	}
	err = r.client.Release(ctx, parent.root(), m.ID.ValueString())
	if err != nil && !errors.Is(err, allot.ErrNotFound) {
		resp.Diagnostics.AddError("Cannot release claim", operationError(m.ID.ValueString(), err))
	}
}
func (r *claimResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	id, err := importID(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}
	spaces, err := r.client.ListSpaces(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Cannot list allocation spaces", err.Error())
		return
	}
	var tree *allot.Tree
	var a allot.Allocation
	for _, space := range spaces {
		if space.Released {
			continue
		}
		candidate, inspectErr := r.client.Inspect(ctx, space.ID)
		if inspectErr != nil {
			resp.Diagnostics.AddError("Cannot inspect allocation space", inspectErr.Error())
			return
		}
		found, lookupErr := candidate.Describe(id)
		if lookupErr == nil {
			tree, a = candidate, found
			break
		}
		if !errors.Is(lookupErr, allot.ErrNotFound) && !errors.Is(lookupErr, allot.ErrReleased) {
			resp.Diagnostics.AddError("Cannot inspect imported claim", lookupErr.Error())
			return
		}
	}
	if tree == nil {
		resp.Diagnostics.AddError("Cannot locate imported claim", "No live allocation has this UUID.")
		return
	}
	m := claimModel{}
	if err = fillClaim(&m, tree.Space(), a); err != nil {
		resp.Diagnostics.AddError("Cannot import claim", err.Error())
		return
	}
	storedTags, err := tree.Tags(id)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import tags", err.Error())
		return
	}
	m.Tags, m.TagsAll, err = readResourceTags(ctx, storedTags, m.Tags, m.TagsAll, r.defaultTags)
	if err != nil {
		resp.Diagnostics.AddError("Cannot import tags", err.Error())
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

// automaticFits compares the allocated interval with the requested capacity.
// It deliberately ignores the selector that originally created the allocation.
func automaticFits(s allot.Selection, result types.Object) bool {
	if s.Automatic == nil || result.IsNull() || result.IsUnknown() {
		return false
	}
	attrs := result.Attributes()
	text := func(name string) string {
		value, ok := attrs[name].(types.String)
		if !ok || value.IsNull() || value.IsUnknown() {
			return ""
		}
		return value.ValueString()
	}
	if s.Automatic.PrefixLength != nil {
		prefix, err := netip.ParsePrefix(text("cidr"))
		return err == nil && prefix == prefix.Masked() && prefix.Bits() == *s.Automatic.PrefixLength
	}
	number := func(value string) (*big.Int, bool) {
		if ip, err := netip.ParseAddr(value); err == nil {
			return new(big.Int).SetBytes(ip.AsSlice()), true
		}
		n, ok := new(big.Int).SetString(value, 10)
		return n, ok
	}
	start, ok := number(text("start"))
	if !ok {
		return false
	}
	end, ok := number(text("end"))
	if !ok {
		return false
	}
	count := big.NewInt(1)
	if s.Automatic.Count != "" {
		count, ok = new(big.Int).SetString(s.Automatic.Count, 10)
		if !ok || count.Sign() <= 0 {
			return false
		}
	}
	return new(big.Int).Add(new(big.Int).Sub(end, start), big.NewInt(1)).Cmp(count) == 0
}

func (r *claimResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.Plan.Raw.IsNull() {
		return
	}
	planTagsAll(ctx, req, resp, r.defaultTags)
	if resp.Diagnostics.HasError() {
		return
	}
	var config claimModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !req.State.Raw.IsNull() {
		var state claimModel
		resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
		if resp.Diagnostics.HasError() {
			return
		}
		// A matching UUID, key and parent remain owned even when Terraform changes
		// how it describes the required capacity. Unknown selectors defer the choice.
		known := true
		for _, v := range []attr.Value{config.CIDR, config.Value, config.Offset, config.Range, config.Request} {
			if v.IsUnknown() {
				known = false
			}
			if obj, ok := v.(types.Object); ok && !obj.IsNull() && !obj.IsUnknown() {
				for _, nested := range obj.Attributes() {
					if nested.IsUnknown() {
						known = false
					}
				}
			}
		}
		var plan claimModel
		resp.Diagnostics.Append(resp.Plan.Get(ctx, &plan)...)
		if resp.Diagnostics.HasError() {
			return
		}
		if known && plan.Key.Equal(state.Key) && plan.Within.Equal(state.Within) {
			desired, err := selection(ctx, config)
			if err == nil && automaticFits(desired, state.Result) {
				setSelection(&plan, desired)
				plan.ID, plan.Ref, plan.Result = state.ID, state.Ref, state.Result
				resp.Diagnostics.Append(resp.Plan.Set(ctx, &plan)...)
				return
			}
		}
		desired, err := selection(ctx, config)
		changed := !plan.Key.Equal(state.Key) || !plan.Within.Equal(state.Within) ||
			!plan.CIDR.Equal(state.CIDR) || !plan.Value.Equal(state.Value) ||
			!plan.Offset.Equal(state.Offset) || !plan.Range.Equal(state.Range) ||
			!plan.Request.Equal(state.Request)
		if known && err == nil && desired.Automatic != nil && !automaticFits(desired, state.Result) {
			changed = true
		}
		if !changed {
			return
		}
		// Selector replacement is decided here so compatible capacity requests
		// can update Terraform state without replacing the stored allocation.
		resp.RequiresReplace.Append(path.Root("request"))
	} else {
		return
	}
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("id"), types.StringUnknown())...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("ref"), types.ObjectUnknown(refTypes))...)
	resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("result"), types.ObjectUnknown(resultTypes))...)
	if config.Request.IsNull() {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("request"), types.ObjectUnknown(requestTypes))...)
	}
}

// Update changes tags and Terraform's description of a compatible allocation.
// The ledger retains its original selector for UUID-specific retries and recovery.
func (r *claimResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var m claimModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &m)...)
	if resp.Diagnostics.HasError() {
		return
	}
	desired, err := selection(ctx, m)
	if err != nil {
		resp.Diagnostics.AddError("Invalid allocation request", err.Error())
		return
	}
	tags, err := resourceTags(ctx, m.Tags, r.defaultTags)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tags", err.Error())
		return
	}
	m.TagsAll, err = tagsAllValue(ctx, tags)
	if err != nil {
		resp.Diagnostics.AddError("Invalid tags", err.Error())
		return
	}
	parent, err := refFrom(ctx, m.Within)
	if err != nil {
		resp.Diagnostics.AddError("Invalid parent reference", err.Error())
		return
	}
	tree, err := r.client.Load(ctx, parent.root())
	if err != nil {
		resp.Diagnostics.AddError("Cannot read allocation space", err.Error())
		return
	}
	a, err := tree.Describe(m.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Cannot inspect claim", err.Error())
		return
	}
	if a.Key != m.Key.ValueString() || a.ParentID != parent.ID.ValueString() {
		resp.Diagnostics.AddError("Allocation identity differs", "The allocation key and parent must match the managed claim.")
		return
	}
	if err = fillClaim(&m, tree.Space(), a); err != nil {
		resp.Diagnostics.AddError("Cannot describe claim", err.Error())
		return
	}
	if !reflect.DeepEqual(desired, *a.Selection) && !automaticFits(desired, m.Result) {
		resp.Diagnostics.AddError("Incompatible allocation request", "The existing allocation does not satisfy the requested capacity; replacement is required.")
		return
	}
	if err := r.client.SetTags(ctx, parent.root(), m.ID.ValueString(), tags); err != nil {
		resp.Diagnostics.AddError("Cannot update claim tags", operationError(m.ID.ValueString(), err))
		return
	}
	setSelection(&m, desired)
	resp.Diagnostics.Append(resp.State.Set(ctx, &m)...)
}

var _ resource.ResourceWithConfigure = (*claimResource)(nil)
var _ resource.ResourceWithImportState = (*claimResource)(nil)
var _ resource.ResourceWithValidateConfig = (*claimResource)(nil)
var _ resource.ResourceWithModifyPlan = (*claimResource)(nil)
