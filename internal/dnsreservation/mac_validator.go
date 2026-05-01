package dnsreservation

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
)

var macRegex = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)

type macValidator struct{}

func (macValidator) Description(context.Context) string {
	return "must be a lowercase, colon-separated MAC address (e.g. 02:a7:f3:03:84:00)"
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
			"MAC must match the pattern aa:bb:cc:dd:ee:ff with lowercase hex bytes; got "+req.ConfigValue.ValueString(),
		)
	}
}
