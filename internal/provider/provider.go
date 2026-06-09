package provider

import (
	"context"
	"os"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/pvginkel/HomelabTerraformProvider/internal/backupcredential"
	"github.com/pvginkel/HomelabTerraformProvider/internal/cephconn"
	"github.com/pvginkel/HomelabTerraformProvider/internal/cephfssubvolume"
	"github.com/pvginkel/HomelabTerraformProvider/internal/dnsreservation"
	"github.com/pvginkel/HomelabTerraformProvider/internal/httplog"
	"github.com/pvginkel/HomelabTerraformProvider/internal/rbdimage"
	"github.com/pvginkel/HomelabTerraformProvider/internal/s3storage"
)

const (
	envDNSURL      = "HOMELAB_DNS_RESERVATION_URL"
	envDNSToken    = "HOMELAB_DNS_RESERVATION_TOKEN"
	envBackupURL   = "HOMELAB_BACKUP_SERVER_URL"
	envBackupToken = "HOMELAB_BACKUP_SERVER_TOKEN"
	envCephMonHost = "HOMELAB_CEPH_MON_HOST"
	envCephUser    = "HOMELAB_CEPH_USER"
	envCephKey     = "HOMELAB_CEPH_KEY"
	envCephPool    = "HOMELAB_CEPH_POOL"
	envS3Endpoint  = "HOMELAB_S3_ENDPOINT"
	envS3AccessKey = "HOMELAB_S3_ADMIN_ACCESS_KEY"
	envS3SecretKey = "HOMELAB_S3_ADMIN_SECRET_KEY"
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
	CephMonHost types.String `tfsdk:"ceph_mon_host"`
	CephUser    types.String `tfsdk:"ceph_user"`
	CephKey     types.String `tfsdk:"ceph_key"`
	CephPool    types.String `tfsdk:"ceph_pool"`
	S3Endpoint  types.String `tfsdk:"s3_endpoint"`
	S3AccessKey types.String `tfsdk:"s3_admin_access_key"`
	S3SecretKey types.String `tfsdk:"s3_admin_secret_key"`
}

// providerClients is the per-resource client bundle handed to every resource
// via ResourceData. Any client may be nil when its provider group was left
// unset; the resource itself reports the missing configuration.
type providerClients struct {
	dns    *dnsreservation.Client
	backup *backupcredential.Client
	ceph   *cephconn.Conn
	s3     *s3storage.Client
}

func (p *providerClients) DNSReservationClient() *dnsreservation.Client {
	return p.dns
}

func (p *providerClients) BackupCredentialClient() *backupcredential.Client {
	return p.backup
}

func (p *providerClients) CephConn() *cephconn.Conn {
	return p.ceph
}

func (p *providerClients) S3StorageClient() *s3storage.Client {
	return p.s3
}

var (
	_ dnsreservation.ProviderData   = (*providerClients)(nil)
	_ backupcredential.ProviderData = (*providerClients)(nil)
	_ rbdimage.ProviderData         = (*providerClients)(nil)
	_ cephfssubvolume.ProviderData  = (*providerClients)(nil)
	_ s3storage.ProviderData        = (*providerClients)(nil)
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
		Description:         "Provider for homelab APIs (dnsmasq reservation sidecar, backup server, and Ceph storage / S3 allocation).",
		MarkdownDescription: "Provider for homelab APIs (dnsmasq reservation sidecar, backup server, and Ceph storage / S3 allocation).",
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
			"ceph_mon_host": schema.StringAttribute{
				Description:         "Ceph mon address(es), comma-separated. Falls back to HOMELAB_CEPH_MON_HOST. Required together with ceph_user, ceph_key and ceph_pool for the rbd/cephfs resources.",
				MarkdownDescription: "Ceph mon address(es), comma-separated. Falls back to `HOMELAB_CEPH_MON_HOST`. Required together with `ceph_user`, `ceph_key` and `ceph_pool` for the rbd/cephfs resources.",
				Optional:            true,
			},
			"ceph_user": schema.StringAttribute{
				Description:         "Cephx user without the \"client.\" prefix. Falls back to HOMELAB_CEPH_USER.",
				MarkdownDescription: "Cephx user without the `client.` prefix. Falls back to `HOMELAB_CEPH_USER`.",
				Optional:            true,
			},
			"ceph_key": schema.StringAttribute{
				Description:         "Cephx base64 key. Falls back to HOMELAB_CEPH_KEY.",
				MarkdownDescription: "Cephx base64 key. Falls back to `HOMELAB_CEPH_KEY`.",
				Optional:            true,
				Sensitive:           true,
			},
			"ceph_pool": schema.StringAttribute{
				Description:         "Pool name, used as both the RBD pool and the CephFS subvolume group. Falls back to HOMELAB_CEPH_POOL.",
				MarkdownDescription: "Pool name, used as both the RBD pool and the CephFS subvolume group. Falls back to `HOMELAB_CEPH_POOL`.",
				Optional:            true,
			},
			"s3_endpoint": schema.StringAttribute{
				Description:         "RGW endpoint URL (e.g. http://ceph:7480). Falls back to HOMELAB_S3_ENDPOINT. Required together with s3_admin_access_key and s3_admin_secret_key for the s3 resource.",
				MarkdownDescription: "RGW endpoint URL (e.g. `http://ceph:7480`). Falls back to `HOMELAB_S3_ENDPOINT`. Required together with `s3_admin_access_key` and `s3_admin_secret_key` for the s3 resource.",
				Optional:            true,
			},
			"s3_admin_access_key": schema.StringAttribute{
				Description:         "Access key of the RGW admin user (caps users=*;buckets=*). Falls back to HOMELAB_S3_ADMIN_ACCESS_KEY.",
				MarkdownDescription: "Access key of the RGW admin user (caps `users=*;buckets=*`). Falls back to `HOMELAB_S3_ADMIN_ACCESS_KEY`.",
				Optional:            true,
				Sensitive:           true,
			},
			"s3_admin_secret_key": schema.StringAttribute{
				Description:         "Secret key of the RGW admin user. Falls back to HOMELAB_S3_ADMIN_SECRET_KEY.",
				MarkdownDescription: "Secret key of the RGW admin user. Falls back to `HOMELAB_S3_ADMIN_SECRET_KEY`.",
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
	if cfg.DNSURL.IsUnknown() || cfg.DNSToken.IsUnknown() || cfg.BackupURL.IsUnknown() || cfg.BackupToken.IsUnknown() ||
		cfg.CephMonHost.IsUnknown() || cfg.CephUser.IsUnknown() || cfg.CephKey.IsUnknown() || cfg.CephPool.IsUnknown() ||
		cfg.S3Endpoint.IsUnknown() || cfg.S3AccessKey.IsUnknown() || cfg.S3SecretKey.IsUnknown() {
		return
	}

	dnsURL := resolveStringConfig(cfg.DNSURL, envDNSURL)
	dnsToken := resolveStringConfig(cfg.DNSToken, envDNSToken)
	backupURL := resolveStringConfig(cfg.BackupURL, envBackupURL)
	backupToken := resolveStringConfig(cfg.BackupToken, envBackupToken)
	cephMonHost := resolveStringConfig(cfg.CephMonHost, envCephMonHost)
	cephUser := resolveStringConfig(cfg.CephUser, envCephUser)
	cephKey := resolveStringConfig(cfg.CephKey, envCephKey)
	cephPool := resolveStringConfig(cfg.CephPool, envCephPool)
	s3Endpoint := resolveStringConfig(cfg.S3Endpoint, envS3Endpoint)
	s3AccessKey := resolveStringConfig(cfg.S3AccessKey, envS3AccessKey)
	s3SecretKey := resolveStringConfig(cfg.S3SecretKey, envS3SecretKey)

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
	// Ceph and S3 are all-or-nothing groups: a partially-set group is a
	// misconfiguration, an entirely-unset group means "I don't use it".
	cephSet := validateGroup(&resp.Diagnostics, []groupField{
		{path: "ceph_mon_host", env: envCephMonHost, value: cephMonHost},
		{path: "ceph_user", env: envCephUser, value: cephUser},
		{path: "ceph_key", env: envCephKey, value: cephKey},
		{path: "ceph_pool", env: envCephPool, value: cephPool},
	})
	s3Set := validateGroup(&resp.Diagnostics, []groupField{
		{path: "s3_endpoint", env: envS3Endpoint, value: s3Endpoint},
		{path: "s3_admin_access_key", env: envS3AccessKey, value: s3AccessKey},
		{path: "s3_admin_secret_key", env: envS3SecretKey, value: s3SecretKey},
	})
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
	if cephSet {
		conn, err := cephconn.New(cephMonHost, cephUser, cephKey, cephPool)
		if err != nil {
			resp.Diagnostics.AddError("Failed to connect to Ceph", err.Error())
			return
		}
		clients.ceph = conn
	}
	if s3Set {
		s3c, err := s3storage.NewClient(s3Endpoint, s3AccessKey, s3SecretKey, p.version)
		if err != nil {
			resp.Diagnostics.AddError("Failed to initialize S3 client", err.Error())
			return
		}
		clients.s3 = s3c
	}

	resp.ResourceData = clients
	resp.DataSourceData = clients
}

// groupField is one member of an all-or-nothing provider config group.
type groupField struct {
	path  string
	env   string
	value string
}

// validateGroup reports whether the group is fully set. If a non-empty proper
// subset is set, it raises an attribute error on each missing member and
// returns false.
func validateGroup(diags *diag.Diagnostics, fields []groupField) bool {
	setCount := 0
	for _, f := range fields {
		if f.value != "" {
			setCount++
		}
	}
	if setCount == 0 {
		return false
	}
	if setCount == len(fields) {
		return true
	}
	for _, f := range fields {
		if f.value == "" {
			diags.AddAttributeError(
				path.Root(f.path),
				"Incomplete provider configuration group",
				"This attribute group must be set together. "+f.path+" is empty while others in its group are set. "+
					"Set it via the provider attribute or "+f.env+", or unset the whole group.",
			)
		}
	}
	return false
}

func (p *HomelabProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		dnsreservation.NewResource,
		backupcredential.NewResource,
		rbdimage.NewResource,
		cephfssubvolume.NewResource,
		s3storage.NewResource,
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
