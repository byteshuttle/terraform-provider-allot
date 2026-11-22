package provider

import (
	"context"
	"fmt"
	"os"

	"github.com/byteshuttle/allot"
	"github.com/byteshuttle/allot/backend"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
)

type allotProvider struct{ version string }
type providerModel struct {
	Store       types.String `tfsdk:"store"`
	Endpoint    types.String `tfsdk:"endpoint"`
	Region      types.String `tfsdk:"region"`
	Profile     types.String `tfsdk:"profile"`
	PathStyle   types.Bool   `tfsdk:"path_style"`
	DefaultTags types.Object `tfsdk:"default_tags"`
}
type defaultTagsModel struct {
	Tags types.Map `tfsdk:"tags"`
}
type configuredProvider struct {
	client      *allot.Client
	defaultTags map[string]string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider { return &allotProvider{version: version} }
}
func (p *allotProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "allot"
	resp.Version = p.version
}
func (p *allotProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{Description: "Hierarchical allocation through the shared Allot client.", Attributes: map[string]schema.Attribute{
		"store":      schema.StringAttribute{Optional: true, Description: "Local path, file:///path or s3://bucket/prefix. Defaults to ALLOT_STORE."},
		"endpoint":   schema.StringAttribute{Optional: true, Description: "Optional S3-compatible endpoint."},
		"region":     schema.StringAttribute{Optional: true, Description: "S3 signing region; otherwise uses AWS SDK configuration."},
		"profile":    schema.StringAttribute{Optional: true, Description: "AWS SDK shared configuration profile."},
		"path_style": schema.BoolAttribute{Optional: true, Description: "Use path-style S3 addressing."},
	}, Blocks: map[string]schema.Block{
		"default_tags": schema.SingleNestedBlock{Description: "Tags applied to every space and claim. Resource tags override matching keys.", Attributes: map[string]schema.Attribute{
			"tags": schema.MapAttribute{Optional: true, ElementType: types.StringType, Description: "Default string tags for all managed allocations."},
		}},
	}}
}
func (p *allotProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.Store.IsUnknown() || data.Endpoint.IsUnknown() || data.Region.IsUnknown() || data.Profile.IsUnknown() || data.PathStyle.IsUnknown() || data.DefaultTags.IsUnknown() {
		resp.Diagnostics.AddError("Unknown storage configuration", "Storage configuration must be known before using Allot.")
		return
	}
	var defaultTags map[string]string
	if !data.DefaultTags.IsNull() {
		var block defaultTagsModel
		resp.Diagnostics.Append(data.DefaultTags.As(ctx, &block, basetypes.ObjectAsOptions{})...)
		if resp.Diagnostics.HasError() {
			return
		}
		var err error
		defaultTags, err = tagsFrom(ctx, block.Tags)
		if err != nil {
			resp.Diagnostics.AddError("Invalid default tags", err.Error())
			return
		}
	}
	location := data.Store.ValueString()
	if data.Store.IsNull() {
		location = os.Getenv("ALLOT_STORE")
	}
	store, err := backend.Open(ctx, backend.Config{Store: location, Endpoint: data.Endpoint.ValueString(), Region: data.Region.ValueString(), Profile: data.Profile.ValueString(), UsePathStyle: data.PathStyle.ValueBool()})
	if err != nil {
		resp.Diagnostics.AddError("Cannot open Allot storage", err.Error())
		return
	}
	client, err := allot.NewClient(store, allot.ClientOptions{})
	if err != nil {
		resp.Diagnostics.AddError("Cannot configure Allot", err.Error())
		return
	}
	resp.ResourceData = &configuredProvider{client: client, defaultTags: defaultTags}
}
func (*allotProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &spaceResource{} }, func() resource.Resource { return &claimResource{} }}
}
func (*allotProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

type resourceClient struct {
	client      *allot.Client
	defaultTags map[string]string
}

func (r *resourceClient) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	configured, ok := req.ProviderData.(*configuredProvider)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider configuration", fmt.Sprintf("Expected an Allot provider configuration, got %T.", req.ProviderData))
		return
	}
	r.client, r.defaultTags = configured.client, configured.defaultTags
}
func (*resourceClient) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unsupported update", "Allocation changes require replacement; in-place moves and resizes are not supported.")
}
