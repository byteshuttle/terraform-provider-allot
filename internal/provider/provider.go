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
)

type allotProvider struct{ version string }
type providerModel struct {
	Store     types.String `tfsdk:"store"`
	Endpoint  types.String `tfsdk:"endpoint"`
	Region    types.String `tfsdk:"region"`
	Profile   types.String `tfsdk:"profile"`
	PathStyle types.Bool   `tfsdk:"path_style"`
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
	}}
}
func (p *allotProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if data.Store.IsUnknown() || data.Endpoint.IsUnknown() || data.Region.IsUnknown() || data.Profile.IsUnknown() || data.PathStyle.IsUnknown() {
		resp.Diagnostics.AddError("Unknown storage configuration", "Storage configuration must be known before using Allot.")
		return
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
	resp.ResourceData = client
}
func (*allotProvider) Resources(context.Context) []func() resource.Resource {
	return []func() resource.Resource{func() resource.Resource { return &spaceResource{} }, func() resource.Resource { return &claimResource{} }}
}
func (*allotProvider) DataSources(context.Context) []func() datasource.DataSource { return nil }

type resourceClient struct{ client *allot.Client }

func (r *resourceClient) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	var ok bool
	r.client, ok = req.ProviderData.(*allot.Client)
	if !ok {
		resp.Diagnostics.AddError("Unexpected provider configuration", fmt.Sprintf("Expected an Allot client, got %T.", req.ProviderData))
	}
}
func (*resourceClient) Update(_ context.Context, _ resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError("Unsupported update", "Allocation changes require replacement; in-place moves and resizes are not supported.")
}
