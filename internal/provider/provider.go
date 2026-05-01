package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

var _ provider.Provider = (*HomelabProvider)(nil)

type HomelabProvider struct {
	version string
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
		Attributes: map[string]schema.Attribute{},
	}
}

func (p *HomelabProvider) Configure(_ context.Context, _ provider.ConfigureRequest, _ *provider.ConfigureResponse) {
}

func (p *HomelabProvider) Resources(_ context.Context) []func() resource.Resource {
	return nil
}

func (p *HomelabProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return nil
}
