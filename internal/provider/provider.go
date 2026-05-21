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

	"github.com/pvginkel/HomelabTerraformProvider/internal/backupcredential"
	"github.com/pvginkel/HomelabTerraformProvider/internal/dnsreservation"
	"github.com/pvginkel/HomelabTerraformProvider/internal/httplog"
)

const (
	envDNSURL      = "HOMELAB_DNS_RESERVATION_URL"
	envDNSToken    = "HOMELAB_DNS_RESERVATION_TOKEN"
	envBackupURL   = "HOMELAB_BACKUP_SERVER_URL"
	envBackupToken = "HOMELAB_BACKUP_SERVER_TOKEN"
)

var _ provider.Provider = (*HomelabProvider)(nil)

type HomelabProvider struct {
	version string
}

type homelabProviderModel struct {
	DNSURL      types.String `tfsdk:"dns_reservation_url"`
	DNSToken    types.String `tfsdk:"dns_reservation_token"`
	BackupURL   types.String `tfsdk:"backup_server_url"`
	BackupToken types.String `tfsdk:"backup_server_token"`
}

// providerClients is the per-resource client bundle handed to every resource
// via ResourceData. Either client may be nil when its provider pair was left
// unset; the resource itself reports the missing configuration.
type providerClients struct {
	dns    *dnsreservation.Client
	backup *backupcredential.Client
}

func (p *providerClients) DNSReservationClient() *dnsreservation.Client {
	return p.dns
}

func (p *providerClients) BackupCredentialClient() *backupcredential.Client {
	return p.backup
}

var (
	_ dnsreservation.ProviderData   = (*providerClients)(nil)
	_ backupcredential.ProviderData = (*providerClients)(nil)
)

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
		Description:         "Provider for homelab APIs (dnsmasq reservation sidecar and backup server).",
		MarkdownDescription: "Provider for homelab APIs (dnsmasq reservation sidecar and backup server).",
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
			"backup_server_url": schema.StringAttribute{
				Description:         "Base URL of the backup server. Falls back to HOMELAB_BACKUP_SERVER_URL.",
				MarkdownDescription: "Base URL of the backup server. Falls back to `HOMELAB_BACKUP_SERVER_URL`.",
				Optional:            true,
			},
			"backup_server_token": schema.StringAttribute{
				Description:         "Management bearer token for the backup server. Falls back to HOMELAB_BACKUP_SERVER_TOKEN.",
				MarkdownDescription: "Management bearer token for the backup server. Falls back to `HOMELAB_BACKUP_SERVER_TOKEN`.",
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
	if cfg.DNSURL.IsUnknown() || cfg.DNSToken.IsUnknown() || cfg.BackupURL.IsUnknown() || cfg.BackupToken.IsUnknown() {
		return
	}

	dnsURL := resolveStringConfig(cfg.DNSURL, envDNSURL)
	dnsToken := resolveStringConfig(cfg.DNSToken, envDNSToken)
	backupURL := resolveStringConfig(cfg.BackupURL, envBackupURL)
	backupToken := resolveStringConfig(cfg.BackupToken, envBackupToken)

	// Half-configured pair is always an error — clearly a misconfiguration,
	// not "I don't use this service".
	if (dnsURL == "") != (dnsToken == "") {
		if dnsURL == "" {
			resp.Diagnostics.AddAttributeError(
				path.Root("dns_reservation_url"),
				"Missing dns_reservation_url",
				"dns_reservation_token is set but dns_reservation_url is empty. Set both, or unset both, via the provider attributes or "+envDNSURL+"/"+envDNSToken+".",
			)
		} else {
			resp.Diagnostics.AddAttributeError(
				path.Root("dns_reservation_token"),
				"Missing dns_reservation_token",
				"dns_reservation_url is set but dns_reservation_token is empty. Set both, or unset both, via the provider attributes or "+envDNSURL+"/"+envDNSToken+".",
			)
		}
	}
	if (backupURL == "") != (backupToken == "") {
		if backupURL == "" {
			resp.Diagnostics.AddAttributeError(
				path.Root("backup_server_url"),
				"Missing backup_server_url",
				"backup_server_token is set but backup_server_url is empty. Set both, or unset both, via the provider attributes or "+envBackupURL+"/"+envBackupToken+".",
			)
		} else {
			resp.Diagnostics.AddAttributeError(
				path.Root("backup_server_token"),
				"Missing backup_server_token",
				"backup_server_url is set but backup_server_token is empty. Set both, or unset both, via the provider attributes or "+envBackupURL+"/"+envBackupToken+".",
			)
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	ctx = httplog.Register(ctx)

	clients := &providerClients{}
	if dnsURL != "" && dnsToken != "" {
		clients.dns = dnsreservation.NewClient(dnsURL, dnsToken, p.version)
	}
	if backupURL != "" && backupToken != "" {
		clients.backup = backupcredential.NewClient(backupURL, backupToken, p.version)
	}

	resp.ResourceData = clients
	resp.DataSourceData = clients
}

func (p *HomelabProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		dnsreservation.NewResource,
		backupcredential.NewResource,
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
