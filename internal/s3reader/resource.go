package s3reader

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*s3ReaderResource)(nil)
	_ resource.ResourceWithConfigure   = (*s3ReaderResource)(nil)
	_ resource.ResourceWithImportState = (*s3ReaderResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies so this package
// can pick up its client without importing the provider.
type ProviderData interface {
	S3ReaderClient() *Client
}

func NewResource() resource.Resource {
	return &s3ReaderResource{}
}

type s3ReaderResource struct {
	client *Client
}

type s3ReaderModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
}

func (r *s3ReaderResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_s3_reader"
}

func (r *s3ReaderResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "An RGW user that owns no buckets, can create none, and holds only the admin capability buckets=read. " +
			"It reads the buckets of every homelab_s3_storage with grant_backup_reader set, on a provider whose s3_backup_reader names it.",
		MarkdownDescription: "An RGW user that owns no buckets, can create none, and holds only the admin capability `buckets=read`. " +
			"It reads the buckets of every `homelab_s3_storage` with `grant_backup_reader` set, on a provider whose `s3_backup_reader` names it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "Resource ID. Equals the name (the RGW user id).",
				MarkdownDescription: "Resource ID. Equals the name (the RGW user id).",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description:         "RGW user id. Renaming forces destroy and recreate.",
				MarkdownDescription: "RGW user id. Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"access_key_id": schema.StringAttribute{
				Description:         "Minted S3 access key id of the reader.",
				MarkdownDescription: "Minted S3 access key id of the reader.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret_access_key": schema.StringAttribute{
				Description:         "Minted S3 secret access key of the reader.",
				MarkdownDescription: "Minted S3 secret access key of the reader.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *s3ReaderResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected s3reader.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	client := data.S3ReaderClient()
	if client == nil {
		resp.Diagnostics.AddError(
			"S3 not configured",
			"homelab_s3_reader requires s3_endpoint, s3_admin_access_key and s3_admin_secret_key to be set on the provider block, "+
				"or the matching HOMELAB_S3_* environment variables.",
		)
		return
	}
	r.client = client
}

func (r *s3ReaderResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan s3ReaderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rd, err := r.client.Create(ctx, plan.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create S3 reader", err.Error())
		return
	}

	plan.ID = types.StringValue(rd.Name)
	plan.AccessKeyID = types.StringValue(rd.AccessKeyID)
	plan.SecretAccessKey = types.StringValue(rd.SecretAccessKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *s3ReaderResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state s3ReaderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	rd, found, err := r.client.Read(ctx, state.Name.ValueString(), state.AccessKeyID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to read S3 reader", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(rd.Name)
	state.AccessKeyID = types.StringValue(rd.AccessKeyID)
	state.SecretAccessKey = types.StringValue(rd.SecretAccessKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update has nothing to change on the user: name forces replacement and every
// other attribute is computed.
func (r *s3ReaderResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan s3ReaderModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *s3ReaderResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state s3ReaderModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete S3 reader", err.Error())
	}
}

func (r *s3ReaderResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
