package rbdimage

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
	_ resource.Resource                = (*rbdImageResource)(nil)
	_ resource.ResourceWithConfigure   = (*rbdImageResource)(nil)
	_ resource.ResourceWithImportState = (*rbdImageResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies so this package
// can pick up the shared Ceph connection without importing the provider.
type ProviderData interface {
	CephConn() *cephconn.Conn
}

func NewResource() resource.Resource {
	return &rbdImageResource{}
}

type rbdImageResource struct {
	client *Client
}

type rbdImageModel struct {
	ID   types.String `tfsdk:"id"`
	Name types.String `tfsdk:"name"`
	Size types.String `tfsdk:"size"`
}

func (r *rbdImageResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_rbd_image"
}

func (r *rbdImageResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "A raw (unformatted) RBD block image in the provider's Ceph pool. The ceph-csi rbd driver formats it on first mount.",
		MarkdownDescription: "A raw (unformatted) RBD block image in the provider's Ceph pool. The ceph-csi rbd driver formats it on first mount.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "Resource ID. Equals the image name.",
				MarkdownDescription: "Resource ID. Equals the image name.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description:         "RBD image name. Renaming forces destroy and recreate.",
				MarkdownDescription: "RBD image name. Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"size": schema.StringAttribute{
				Description:         "Image capacity as a Kubernetes quantity string (e.g. \"10Gi\"). Grow-only; shrinking is rejected.",
				MarkdownDescription: "Image capacity as a Kubernetes quantity string (e.g. `\"10Gi\"`). Grow-only; shrinking is rejected.",
				Required:            true,
			},
		},
	}
}

func (r *rbdImageResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected rbdimage.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	conn := data.CephConn()
	if conn == nil {
		resp.Diagnostics.AddError(
			"Ceph not configured",
			"homelab_rbd_image requires ceph_mon_host, ceph_user, ceph_key and ceph_pool to be set on the provider block, "+
				"or the matching HOMELAB_CEPH_* environment variables.",
		)
		return
	}
	r.client = NewClient(conn)
}

func (r *rbdImageResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan rbdImageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bytes, err := quantity.ParseBytes(plan.Size.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("size"), "Invalid size", err.Error())
		return
	}

	if err := r.client.Create(plan.Name.ValueString(), bytes); err != nil {
		resp.Diagnostics.AddError("Failed to create RBD image", err.Error())
		return
	}

	plan.ID = types.StringValue(plan.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *rbdImageResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state rbdImageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	img, err := r.client.Read(state.Name.ValueString())
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read RBD image", err.Error())
		return
	}

	state.ID = types.StringValue(state.Name.ValueString())
	state.Size = normalizeSize(state.Size, img.Bytes)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *rbdImageResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan rbdImageModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	bytes, err := quantity.ParseBytes(plan.Size.ValueString())
	if err != nil {
		resp.Diagnostics.AddAttributeError(path.Root("size"), "Invalid size", err.Error())
		return
	}

	if err := r.client.Resize(plan.Name.ValueString(), bytes); err != nil {
		resp.Diagnostics.AddError("Failed to resize RBD image", err.Error())
		return
	}

	plan.ID = types.StringValue(plan.Name.ValueString())
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *rbdImageResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state rbdImageModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(state.Name.ValueString()); err != nil {
		resp.Diagnostics.AddError("Failed to delete RBD image", err.Error())
	}
}

func (r *rbdImageResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// normalizeSize keeps the operator's literal in state when it still parses to
// the cluster's actual byte count, so equivalent spellings (e.g. "10Gi" vs
// "10240Mi") don't churn the plan. On real drift it re-emits the canonical
// form of the actual size.
func normalizeSize(literal types.String, actualBytes uint64) types.String {
	if !literal.IsNull() && !literal.IsUnknown() {
		if b, err := quantity.ParseBytes(literal.ValueString()); err == nil && b == actualBytes {
			return literal
		}
	}
	return types.StringValue(quantity.FormatBytes(actualBytes))
}
