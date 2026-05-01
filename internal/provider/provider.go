package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pvginkel/HomelabTerraformProvider/internal/dnsreservation"
	"github.com/pvginkel/HomelabTerraformProvider/internal/httplog"
)

const (
	envURL   = "HOMELAB_DNS_RESERVATION_URL"
	envToken = "HOMELAB_DNS_RESERVATION_TOKEN"
)

var _ provider.Provider = (*HomelabProvider)(nil)

type HomelabProvider struct {
	version string
}

type homelabProviderModel struct {
	URL   types.String `tfsdk:"dns_reservation_url"`
	Token types.String `tfsdk:"dns_reservation_token"`
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &HomelabProvider{version: version}
	}
}

func (p *HomelabProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "homelab"
	resp.Version = p.version
}

func (p *HomelabProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description:         "Provider for homelab APIs (currently the dnsmasq reservation sidecar).",
		MarkdownDescription: "Provider for homelab APIs (currently the dnsmasq reservation sidecar).",
		Attributes: map[string]schema.Attribute{
			"dns_reservation_url": schema.StringAttribute{
				Description:         "Base URL of the dnsmasq reservation sidecar. Falls back to HOMELAB_DNS_RESERVATION_URL.",
				MarkdownDescription: "Base URL of the dnsmasq reservation sidecar. Falls back to `HOMELAB_DNS_RESERVATION_URL`.",
				Optional:            true,
			},
			"dns_reservation_token": schema.StringAttribute{
				Description:         "Bearer token for the dnsmasq reservation sidecar. Falls back to HOMELAB_DNS_RESERVATION_TOKEN.",
				MarkdownDescription: "Bearer token for the dnsmasq reservation sidecar. Falls back to `HOMELAB_DNS_RESERVATION_TOKEN`.",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *HomelabProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg homelabProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Defer until plan-time when these come from another resource's output.
	if cfg.URL.IsUnknown() || cfg.Token.IsUnknown() {
		return
	}

	url := resolveStringConfig(cfg.URL, envURL)
	token := resolveStringConfig(cfg.Token, envToken)

	if url == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("dns_reservation_url"),
			"Missing dns_reservation_url",
			"Set the dns_reservation_url provider attribute or the "+envURL+" environment variable.",
		)
	}
	if token == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("dns_reservation_token"),
			"Missing dns_reservation_token",
			"Set the dns_reservation_token provider attribute or the "+envToken+" environment variable.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = httplog.Register(ctx)
	client := dnsreservation.NewClient(url, token, p.version)

	resp.ResourceData = client
	resp.DataSourceData = client
}

func (p *HomelabProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		dnsreservation.NewResource,
	}
}

func (p *HomelabProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}

func resolveStringConfig(v types.String, envName string) string {
	if !v.IsNull() && !v.IsUnknown() {
		if s := strings.TrimSpace(v.ValueString()); s != "" {
			return s
		}
	}
	return strings.TrimSpace(os.Getenv(envName))
}
