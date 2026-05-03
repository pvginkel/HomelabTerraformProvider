package dnsreservation

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var macRegex = regexp.MustCompile(`^[0-9A-F]{2}(:[0-9A-F]{2}){5}$`)

type macValidator struct{}

func (macValidator) Description(context.Context) string {
	return "must be an uppercase, colon-separated MAC address (e.g. 02:A7:F3:03:84:00)"
}

func (m macValidator) MarkdownDescription(ctx context.Context) string {
	return m.Description(ctx)
}

func (macValidator) ValidateString(_ context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	if !macRegex.MatchString(req.ConfigValue.ValueString()) {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid MAC address",
			"MAC must match the pattern AA:BB:CC:DD:EE:FF with uppercase hex bytes; got "+req.ConfigValue.ValueString(),
		)
	}
}
