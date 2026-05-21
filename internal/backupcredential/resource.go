package backupcredential

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
	_ resource.Resource                = (*backupCredentialResource)(nil)
	_ resource.ResourceWithConfigure   = (*backupCredentialResource)(nil)
	_ resource.ResourceWithImportState = (*backupCredentialResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies. It lets this
// package pick out its own client without importing the provider package
// (which would cause an import cycle).
type ProviderData interface {
	BackupCredentialClient() *Client
}

func NewResource() resource.Resource {
	return &backupCredentialResource{}
}

type backupCredentialResource struct {
	client *Client
}

type backupCredentialModel struct {
	ID        types.String `tfsdk:"id"`
	Scope     types.String `tfsdk:"scope"`
	Retention types.Int64  `tfsdk:"retention"`
	Token     types.String `tfsdk:"token"`
}

func (r *backupCredentialResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_backup_credential"
}

func (r *backupCredentialResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "An upload credential managed by the homelab backup server.",
		MarkdownDescription: "An upload credential managed by the homelab backup server.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "Resource ID. Equals the scope.",
				MarkdownDescription: "Resource ID. Equals the scope.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"scope": schema.StringAttribute{
				Description:         "Scope key. URL segment and folder name under the rclone destination. Renaming forces destroy and recreate.",
				MarkdownDescription: "Scope key. URL segment and folder name under the rclone destination. Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"retention": schema.Int64Attribute{
				Description:         "Number of stored objects to retain in this scope. Integer in [1, 100].",
				MarkdownDescription: "Number of stored objects to retain in this scope. Integer in `[1, 100]`.",
				Required:            true,
			},
			"token": schema.StringAttribute{
				Description:         "Server-minted bearer token authorized to upload to this scope. Stable for the lifetime of the scope; rotate by tainting the resource.",
				MarkdownDescription: "Server-minted bearer token authorized to upload to this scope. Stable for the lifetime of the scope; rotate by tainting the resource.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *backupCredentialResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected backupcredential.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	client := data.BackupCredentialClient()
	if client == nil {
		resp.Diagnostics.AddError(
			"Backup server not configured",
			"homelab_backup_credential requires backup_server_url and backup_server_token to be set on the provider block, "+
				"or HOMELAB_BACKUP_SERVER_URL and HOMELAB_BACKUP_SERVER_TOKEN in the environment.",
		)
		return
	}
	r.client = client
}

func (r *backupCredentialResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan backupCredentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Put(ctx, plan.Scope.ValueString(), int(plan.Retention.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to create backup credential", err.Error())
		return
	}

	plan.ID = types.StringValue(res.Scope)
	plan.Scope = types.StringValue(res.Scope)
	plan.Retention = types.Int64Value(int64(res.Retention))
	plan.Token = types.StringValue(res.Token)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backupCredentialResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state backupCredentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Get(ctx, state.ID.ValueString())
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read backup credential", err.Error())
		return
	}

	state.ID = types.StringValue(res.Scope)
	state.Scope = types.StringValue(res.Scope)
	state.Retention = types.Int64Value(int64(res.Retention))
	state.Token = types.StringValue(res.Token)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *backupCredentialResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan backupCredentialModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Put(ctx, plan.Scope.ValueString(), int(plan.Retention.ValueInt64()))
	if err != nil {
		resp.Diagnostics.AddError("Failed to update backup credential", err.Error())
		return
	}

	plan.ID = types.StringValue(res.Scope)
	plan.Scope = types.StringValue(res.Scope)
	plan.Retention = types.Int64Value(int64(res.Retention))
	plan.Token = types.StringValue(res.Token)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *backupCredentialResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state backupCredentialModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.ID.ValueString()); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete backup credential", err.Error())
	}
}

func (r *backupCredentialResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
