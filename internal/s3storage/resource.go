package s3storage

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
	_ resource.Resource                = (*s3StorageResource)(nil)
	_ resource.ResourceWithConfigure   = (*s3StorageResource)(nil)
	_ resource.ResourceWithImportState = (*s3StorageResource)(nil)
	_ resource.ResourceWithModifyPlan  = (*s3StorageResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies so this package
// can pick up its client without importing the provider.
type ProviderData interface {
	S3StorageClient() *Client
}

func NewResource() resource.Resource {
	return &s3StorageResource{}
}

type s3StorageResource struct {
	client *Client
}

type s3StorageModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Buckets         types.Set    `tfsdk:"buckets"`
	KeyRotation     types.String `tfsdk:"key_rotation"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
}

func (r *s3StorageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_s3_storage"
}

func (r *s3StorageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "A set of S3 buckets plus a dedicated RGW credential scoped to exactly those buckets and unable to create new ones.",
		MarkdownDescription: "A set of S3 buckets plus a dedicated RGW credential scoped to exactly those buckets and unable to create new ones.",
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
				Description:         "RGW user id and logical name. Renaming forces destroy and recreate.",
				MarkdownDescription: "RGW user id and logical name. Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"buckets": schema.SetAttribute{
				Description:         "Buckets owned by this credential. Adding creates+links a bucket; removing deletes the bucket and all its objects.",
				MarkdownDescription: "Buckets owned by this credential. Adding creates+links a bucket; removing deletes the bucket and all its objects.",
				Required:            true,
				ElementType:         types.StringType,
			},
			"key_rotation": schema.StringAttribute{
				Description:         "Opaque rotation trigger. Changing this value regenerates the access key on the same user; buckets and objects are untouched.",
				MarkdownDescription: "Opaque rotation trigger. Changing this value regenerates the access key on the same user; buckets and objects are untouched.",
				Optional:            true,
			},
			"access_key_id": schema.StringAttribute{
				Description:         "Minted S3 access key id for this allocation.",
				MarkdownDescription: "Minted S3 access key id for this allocation.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"secret_access_key": schema.StringAttribute{
				Description:         "Minted S3 secret access key for this allocation.",
				MarkdownDescription: "Minted S3 secret access key for this allocation.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *s3StorageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected s3storage.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	client := data.S3StorageClient()
	if client == nil {
		resp.Diagnostics.AddError(
			"S3 not configured",
			"homelab_s3_storage requires s3_endpoint, s3_admin_access_key and s3_admin_secret_key to be set on the provider block, "+
				"or the matching HOMELAB_S3_* environment variables.",
		)
		return
	}
	r.client = client
}

// ModifyPlan forces the computed key fields to unknown when key_rotation
// changes, so Terraform expects (and accepts) the rotated credential the
// Update produces. UseStateForUnknown keeps them stable on every other plan.
func (r *s3StorageResource) ModifyPlan(ctx context.Context, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() {
		return // create or destroy
	}

	var stateRotation, planRotation types.String
	resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("key_rotation"), &stateRotation)...)
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("key_rotation"), &planRotation)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !stateRotation.Equal(planRotation) {
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("access_key_id"), types.StringUnknown())...)
		resp.Diagnostics.Append(resp.Plan.SetAttribute(ctx, path.Root("secret_access_key"), types.StringUnknown())...)
	}
}

func (r *s3StorageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan s3StorageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	buckets := bucketSlice(ctx, plan.Buckets, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	st, err := r.client.Create(ctx, plan.Name.ValueString(), buckets)
	if err != nil {
		resp.Diagnostics.AddError("Failed to create S3 storage", err.Error())
		return
	}

	plan.ID = types.StringValue(st.Name)
	plan.AccessKeyID = types.StringValue(st.AccessKeyID)
	plan.SecretAccessKey = types.StringValue(st.SecretAccessKey)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *s3StorageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state s3StorageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	wantBuckets := bucketSlice(ctx, state.Buckets, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	st, found, err := r.client.Read(ctx, state.Name.ValueString(), state.AccessKeyID.ValueString(), wantBuckets)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read S3 storage", err.Error())
		return
	}
	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	state.ID = types.StringValue(st.Name)
	state.AccessKeyID = types.StringValue(st.AccessKeyID)
	state.SecretAccessKey = types.StringValue(st.SecretAccessKey)
	bucketSet, diags := types.SetValueFrom(ctx, types.StringType, st.Buckets)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	state.Buckets = bucketSet

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *s3StorageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state s3StorageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	name := plan.Name.ValueString()
	planBuckets := bucketSlice(ctx, plan.Buckets, &resp.Diagnostics)
	stateBuckets := bucketSlice(ctx, state.Buckets, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	added, removed := diffBuckets(stateBuckets, planBuckets)
	if err := r.client.AddBuckets(ctx, name, added); err != nil {
		resp.Diagnostics.AddError("Failed to add S3 buckets", err.Error())
		return
	}
	if err := r.client.RemoveBuckets(ctx, removed); err != nil {
		resp.Diagnostics.AddError("Failed to remove S3 buckets", err.Error())
		return
	}

	access := state.AccessKeyID.ValueString()
	secret := state.SecretAccessKey.ValueString()
	if !state.KeyRotation.Equal(plan.KeyRotation) {
		newAccess, newSecret, err := r.client.RotateKey(ctx, name, access)
		if err != nil {
			resp.Diagnostics.AddError("Failed to rotate S3 key", err.Error())
			return
		}
		access, secret = newAccess, newSecret
	}

	plan.ID = types.StringValue(name)
	plan.AccessKeyID = types.StringValue(access)
	plan.SecretAccessKey = types.StringValue(secret)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *s3StorageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state s3StorageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	buckets := bucketSlice(ctx, state.Buckets, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.Name.ValueString(), buckets); err != nil {
		resp.Diagnostics.AddError("Failed to delete S3 storage", err.Error())
	}
}

func (r *s3StorageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}
