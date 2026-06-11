package zfsdataset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*zfsDatasetResource)(nil)
	_ resource.ResourceWithConfigure   = (*zfsDatasetResource)(nil)
	_ resource.ResourceWithImportState = (*zfsDatasetResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies so this package
// can pick up its client without importing the provider package.
type ProviderData interface {
	ZFSDatasetClient() *Client
}

func NewResource() resource.Resource {
	return &zfsDatasetResource{}
}

type zfsDatasetResource struct {
	client *Client
}

type zfsDatasetModel struct {
	ID                 types.String `tfsdk:"id"`
	Pool               types.String `tfsdk:"pool"`
	Name               types.String `tfsdk:"name"`
	Quota              types.String `tfsdk:"quota"`
	Recordsize         types.String `tfsdk:"recordsize"`
	Compression        types.String `tfsdk:"compression"`
	Mountpoint         types.String `tfsdk:"mountpoint"`
	Properties         types.Map    `tfsdk:"properties"`
	GUID               types.String `tfsdk:"guid"`
	MountpointResolved types.String `tfsdk:"mountpoint_resolved"`
}

func (r *zfsDatasetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_zfs_dataset"
}

func (r *zfsDatasetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "A ZFS dataset managed on a Kubernetes node through the iac-provisioner agent.",
		MarkdownDescription: "A ZFS dataset managed on a Kubernetes node through the iac-provisioner agent.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "Resource ID. Equals <pool>/<name>.",
				MarkdownDescription: "Resource ID. Equals `<pool>/<name>`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"pool": schema.StringAttribute{
				Description:         "ZFS pool name (e.g. zpool2). Must have a zfs_pools mapping on the provider. Changing it forces destroy and recreate.",
				MarkdownDescription: "ZFS pool name (e.g. `zpool2`). Must have a `zfs_pools` mapping on the provider. Changing it forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"name": schema.StringAttribute{
				Description:         "Dataset path within the pool (e.g. k8s/prd-paperless-data). Renaming forces destroy and recreate.",
				MarkdownDescription: "Dataset path within the pool (e.g. `k8s/prd-paperless-data`). Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"quota": schema.StringAttribute{
				Description:         "ZFS quota (e.g. 20G). Unset means no quota.",
				MarkdownDescription: "ZFS quota (e.g. `20G`). Unset means no quota.",
				Optional:            true,
			},
			"recordsize": schema.StringAttribute{
				Description:         "ZFS recordsize. Defaults to 128K.",
				MarkdownDescription: "ZFS recordsize. Defaults to `128K`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("128K"),
			},
			"compression": schema.StringAttribute{
				Description:         "ZFS compression. Defaults to lz4.",
				MarkdownDescription: "ZFS compression. Defaults to `lz4`.",
				Optional:            true,
				Computed:            true,
				Default:             stringdefault.StaticString("lz4"),
			},
			"mountpoint": schema.StringAttribute{
				Description:         "Dataset mountpoint. Defaults to /<pool>/<name>.",
				MarkdownDescription: "Dataset mountpoint. Defaults to `/<pool>/<name>`.",
				Optional:            true,
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"properties": schema.MapAttribute{
				Description:         "Arbitrary extra zfs properties applied with zfs set. Must not repeat quota/recordsize/compression/mountpoint.",
				MarkdownDescription: "Arbitrary extra `zfs` properties applied with `zfs set`. Must not repeat `quota`/`recordsize`/`compression`/`mountpoint`.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"guid": schema.StringAttribute{
				Description:         "ZFS dataset GUID. Stable across property changes; the drift-detection key.",
				MarkdownDescription: "ZFS dataset GUID. Stable across property changes; the drift-detection key.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"mountpoint_resolved": schema.StringAttribute{
				Description:         "What zfs get mountpoint reports after create. Feeds a static-PV module's local: path.",
				MarkdownDescription: "What `zfs get mountpoint` reports after create. Feeds a static-PV module's `local:` path.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *zfsDatasetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected zfsdataset.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	client := data.ZFSDatasetClient()
	if client == nil {
		resp.Diagnostics.AddError(
			"ZFS provisioner not configured",
			"homelab_zfs_dataset requires zfs_pools and iac_provisioner_token to be set on the provider block, "+
				"or HOMELAB_IAC_PROVISIONER_TOKEN in the environment.",
		)
		return
	}
	r.client = client
}

func (r *zfsDatasetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan zfsDatasetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, diags := r.buildSpec(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ds, err := r.client.Put(ctx, plan.Pool.ValueString(), plan.Name.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.Append(putErrorDiag("create", err)...)
		return
	}

	applyComputed(&plan, ds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *zfsDatasetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state zfsDatasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	ds, err := r.client.Get(ctx, state.Pool.ValueString(), state.Name.ValueString())
	if err != nil {
		if IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Failed to read ZFS dataset", err.Error())
		return
	}

	diags := applyRefresh(ctx, &state, ds)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *zfsDatasetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan zfsDatasetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	spec, diags := r.buildSpec(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	ds, err := r.client.Put(ctx, plan.Pool.ValueString(), plan.Name.ValueString(), spec)
	if err != nil {
		resp.Diagnostics.Append(putErrorDiag("update", err)...)
		return
	}

	applyComputed(&plan, ds)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *zfsDatasetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state zfsDatasetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.Pool.ValueString(), state.Name.ValueString()); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete ZFS dataset", err.Error())
	}
}

func (r *zfsDatasetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	pool, name, ok := splitID(req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected <pool>/<name>, got %q.", req.ID),
		)
		return
	}
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("pool"), pool)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), name)...)
}

// buildSpec turns the plan into the agent request body, filling the mountpoint
// default (/<pool>/<name>) when the attribute was left to compute.
func (r *zfsDatasetResource) buildSpec(ctx context.Context, plan zfsDatasetModel) (Spec, diag.Diagnostics) {
	var diags diag.Diagnostics

	props := map[string]string{}
	if !plan.Properties.IsNull() && !plan.Properties.IsUnknown() {
		diags = append(diags, plan.Properties.ElementsAs(ctx, &props, false)...)
	}

	mountpoint := plan.Mountpoint.ValueString()
	if plan.Mountpoint.IsNull() || plan.Mountpoint.IsUnknown() {
		mountpoint = "/" + plan.Pool.ValueString() + "/" + plan.Name.ValueString()
	}

	return Spec{
		Quota:       plan.Quota.ValueString(),
		Recordsize:  plan.Recordsize.ValueString(),
		Compression: plan.Compression.ValueString(),
		Mountpoint:  mountpoint,
		Properties:  props,
	}, diags
}

// applyComputed fills the computed attributes from the agent response after a
// create/update, leaving the configured input attributes as the plan set them.
func applyComputed(plan *zfsDatasetModel, ds *Dataset) {
	plan.ID = types.StringValue(ds.Dataset)
	plan.Recordsize = types.StringValue(ds.Recordsize)
	plan.Compression = types.StringValue(ds.Compression)
	plan.Mountpoint = types.StringValue(ds.Mountpoint)
	plan.GUID = types.StringValue(ds.GUID)
	plan.MountpointResolved = types.StringValue(ds.Mountpoint)
}

// applyRefresh overwrites state from the agent response on Read, so out-of-band
// property edits surface as a plan diff.
func applyRefresh(ctx context.Context, state *zfsDatasetModel, ds *Dataset) diag.Diagnostics {
	state.ID = types.StringValue(ds.Dataset)
	state.Quota = stringOrNull(ds.Quota)
	state.Recordsize = types.StringValue(ds.Recordsize)
	state.Compression = types.StringValue(ds.Compression)
	state.Mountpoint = types.StringValue(ds.Mountpoint)
	state.GUID = types.StringValue(ds.GUID)
	state.MountpointResolved = types.StringValue(ds.Mountpoint)

	if len(ds.Properties) == 0 {
		state.Properties = types.MapNull(types.StringType)
		return nil
	}
	m, diags := types.MapValueFrom(ctx, types.StringType, ds.Properties)
	if diags.HasError() {
		return diags
	}
	state.Properties = m
	return nil
}

func stringOrNull(s string) types.String {
	if s == "" {
		return types.StringNull()
	}
	return types.StringValue(s)
}

// putErrorDiag turns a Put/Create/Update error into diagnostics, giving the
// unmapped-pool config error an attribute-scoped message.
func putErrorDiag(op string, err error) diag.Diagnostics {
	var diags diag.Diagnostics
	var unmapped *UnmappedPoolError
	if errors.As(err, &unmapped) {
		diags.AddAttributeError(path.Root("pool"), "Unknown ZFS pool", unmapped.Error())
		return diags
	}
	diags.AddError("Failed to "+op+" ZFS dataset", err.Error())
	return diags
}

// splitID parses an import id (<pool>/<name>) into its pool and dataset path.
func splitID(id string) (pool, name string, ok bool) {
	i := strings.Index(id, "/")
	if i <= 0 || i == len(id)-1 {
		return "", "", false
	}
	return id[:i], id[i+1:], true
}
