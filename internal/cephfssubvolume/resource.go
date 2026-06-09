package cephfssubvolume

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pvginkel/HomelabTerraformProvider/internal/cephconn"
	"github.com/pvginkel/HomelabTerraformProvider/internal/quantity"
)

var (
	_ resource.Resource                = (*cephfsSubVolumeResource)(nil)
	_ resource.ResourceWithConfigure   = (*cephfsSubVolumeResource)(nil)
	_ resource.ResourceWithImportState = (*cephfsSubVolumeResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies so this package
// can pick up the shared Ceph connection without importing the provider.
type ProviderData interface {
	CephConn() *cephconn.Conn
}

func NewResource() resource.Resource {
	return &cephfsSubVolumeResource{}
}

type cephfsSubVolumeResource struct {
	client *Client
}

type cephfsSubVolumeModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Size types.String `tfsdk:"size"`
	Path types.String `tfsdk:"path"`
}

func (r *cephfsSubVolumeResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_cephfs_subvolume"
}

func (r *cephfsSubVolumeResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "A CephFS subvolume (RWX file storage) in the cephfs filesystem, under the provider's pool as subvolume group.",
		MarkdownDescription: "A CephFS subvolume (RWX file storage) in the `cephfs` filesystem, under the provider's pool as subvolume group.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "Resource ID. Equals the subvolume name.",
				MarkdownDescription: "Resource ID. Equals the subvolume name.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description:         "Subvolume name. Renaming forces destroy and recreate.",
				MarkdownDescription: "Subvolume name. Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size": schema.StringAttribute{
				Description:         "Quota as a Kubernetes quantity string (e.g. \"10Gi\"). Resized in place; the mgr refuses to set a quota below current usage.",
				MarkdownDescription: "Quota as a Kubernetes quantity string (e.g. `\"10Gi\"`). Resized in place; the mgr refuses to set a quota below current usage.",
				Required:            true,
			},
			"path": schema.StringAttribute{
				Description:         "Absolute subvolume path on the filesystem (e.g. /volumes/<group>/<name>/<uuid>). Use as the chart's rootPath.",
				MarkdownDescription: "Absolute subvolume path on the filesystem (e.g. `/volumes/<group>/<name>/<uuid>`). Use as the chart's `rootPath`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *cephfsSubVolumeResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected cephfssubvolume.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	conn := data.CephConn()
	if conn == nil {
		resp.Diagnostics.AddError(
			"Ceph not configured",
			"homelab_cephfs_subvolume requires ceph_mon_host, ceph_user, ceph_key and ceph_pool to be set on the provider block, "+
				"or the matching HOMELAB_CEPH_* environment variables.",
		)
		return
	}
	r.client = NewClient(conn)
}

func (r *cephfsSubVolumeResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan cephfsSubVolumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bytes, err := quantity.ParseBytes(plan.Size.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("size"), "Invalid size", err.Error())
		return
	}

	name := plan.Name.ValueString()
	if err := r.client.Create(name, bytes); err != nil {
		resp.Diagnostics.AddError("Failed to create CephFS subvolume", err.Error())
		return
	}

	info, err := r.client.Read(name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read back CephFS subvolume", err.Error())
		return
	}

	plan.ID = types.StringValue(name)
	plan.Path = types.StringValue(info.Path)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cephfsSubVolumeResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state cephfsSubVolumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	info, err := r.client.Read(state.Name.ValueString())
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read CephFS subvolume", err.Error())
		return
	}

	state.ID = types.StringValue(state.Name.ValueString())
	state.Path = types.StringValue(info.Path)
	if info.QuotaSet {
		state.Size = normalizeSize(state.Size, info.Bytes)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *cephfsSubVolumeResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan cephfsSubVolumeModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bytes, err := quantity.ParseBytes(plan.Size.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("size"), "Invalid size", err.Error())
		return
	}

	name := plan.Name.ValueString()
	if err := r.client.Resize(name, bytes); err != nil {
		resp.Diagnostics.AddError("Failed to resize CephFS subvolume", err.Error())
		return
	}

	info, err := r.client.Read(name)
	if err != nil {
		resp.Diagnostics.AddError("Failed to read back CephFS subvolume", err.Error())
		return
	}

	plan.ID = types.StringValue(name)
	plan.Path = types.StringValue(info.Path)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *cephfsSubVolumeResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state cephfsSubVolumeModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete CephFS subvolume", err.Error())
	}
}

func (r *cephfsSubVolumeResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// normalizeSize keeps the operator's literal in state when it still parses to
// the cluster's actual quota, so equivalent spellings don't churn the plan. On
// real drift it re-emits the canonical form of the actual quota.
func normalizeSize(literal types.String, actualBytes uint64) types.String {
	if !literal.IsNull() && !literal.IsUnknown() {
		if b, err := quantity.ParseBytes(literal.ValueString()); err == nil && b == actualBytes {
			return literal
		}
	}
	return types.StringValue(quantity.FormatBytes(actualBytes))
}
