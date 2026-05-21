package dnsreservation

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = (*dnsReservationResource)(nil)
	_ resource.ResourceWithConfigure   = (*dnsReservationResource)(nil)
	_ resource.ResourceWithImportState = (*dnsReservationResource)(nil)
)

// ProviderData is the interface the provider's wiring satisfies. It lets this
// package pick out its own client without importing the provider package
// (which would cause an import cycle).
type ProviderData interface {
	DNSReservationClient() *Client
}

func NewResource() resource.Resource {
	return &dnsReservationResource{}
}

type dnsReservationResource struct {
	client *Client
}

type dnsReservationModel struct {
	ID       types.String `tfsdk:"id"`
	Hostname types.String `tfsdk:"hostname"`
	MAC      types.String `tfsdk:"mac"`
	IPv4     types.String `tfsdk:"ipv4"`
}

func (r *dnsReservationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_dns_reservation"
}

func (r *dnsReservationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "A DHCP+DNS reservation managed by the homelab dnsmasq sidecar.",
		MarkdownDescription: "A DHCP+DNS reservation managed by the homelab dnsmasq sidecar.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description:         "Resource ID. Equals the hostname.",
				MarkdownDescription: "Resource ID. Equals the hostname.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"hostname": schema.StringAttribute{
				Description:         "Reservation hostname (lowercase, no FQDN). Renaming forces destroy and recreate.",
				MarkdownDescription: "Reservation hostname (lowercase, no FQDN). Renaming forces destroy and recreate.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"mac": schema.StringAttribute{
				Description:         "MAC address bound to this hostname. Uppercase, colon-separated.",
				MarkdownDescription: "MAC address bound to this hostname. Uppercase, colon-separated.",
				Required:            true,
				Validators: []validator.String{
					macValidator{},
				},
			},
			"ipv4": schema.StringAttribute{
				Description:         "IPv4 address allocated by the sidecar and bound to the hostname for its lifetime.",
				MarkdownDescription: "IPv4 address allocated by the sidecar and bound to the hostname for its lifetime.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *dnsReservationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}
	data, ok := req.ProviderData.(ProviderData)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected dnsreservation.ProviderData, got %T. Please report this as a provider bug.", req.ProviderData),
		)
		return
	}
	client := data.DNSReservationClient()
	if client == nil {
		resp.Diagnostics.AddError(
			"DNS reservation sidecar not configured",
			"homelab_dns_reservation requires dns_reservation_url and dns_reservation_token to be set on the provider block, "+
				"or HOMELAB_DNS_RESERVATION_URL and HOMELAB_DNS_RESERVATION_TOKEN in the environment.",
		)
		return
	}
	r.client = client
}

func (r *dnsReservationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan dnsReservationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Put(ctx, plan.Hostname.ValueString(), plan.MAC.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to create reservation", err.Error())
		return
	}

	plan.ID = types.StringValue(res.Hostname)
	plan.Hostname = types.StringValue(res.Hostname)
	plan.MAC = types.StringValue(res.MAC)
	plan.IPv4 = types.StringValue(res.IPv4)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsReservationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state dnsReservationModel
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
		resp.Diagnostics.AddError("Failed to read reservation", err.Error())
		return
	}

	state.ID = types.StringValue(res.Hostname)
	state.Hostname = types.StringValue(res.Hostname)
	state.MAC = types.StringValue(res.MAC)
	state.IPv4 = types.StringValue(res.IPv4)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *dnsReservationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan dnsReservationModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	res, err := r.client.Put(ctx, plan.Hostname.ValueString(), plan.MAC.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Failed to update reservation", err.Error())
		return
	}

	plan.ID = types.StringValue(res.Hostname)
	plan.Hostname = types.StringValue(res.Hostname)
	plan.MAC = types.StringValue(res.MAC)
	plan.IPv4 = types.StringValue(res.IPv4)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *dnsReservationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state dnsReservationModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.ID.ValueString()); err != nil && !IsNotFound(err) {
		resp.Diagnostics.AddError("Failed to delete reservation", err.Error())
	}
}

func (r *dnsReservationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
