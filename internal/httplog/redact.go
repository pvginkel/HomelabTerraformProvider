// Package httplog wires a tflog subsystem with bearer-token redaction so any
// HTTP request/response logged through it cannot leak the auth header.
package httplog

import (
	"context"
	"regexp"

	"github.com/hashicorp/terraform-plugin-log/tflog"
)

const Subsystem = "homelab_http"

var bearerRegex = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]+`)

// Register installs the subsystem and its mask regex on ctx. Callers should
// pass the returned context into the typed HTTP client so its tflog calls run
// through the masking pipeline.
func Register(ctx context.Context) context.Context {
	ctx = tflog.NewSubsystem(ctx, Subsystem,
		tflog.WithLevelFromEnv("TF_LOG_PROVIDER_HOMELAB"))
	ctx = tflog.SubsystemMaskMessageRegexes(ctx, Subsystem, bearerRegex)
	ctx = tflog.SubsystemMaskAllFieldValuesRegexes(ctx, Subsystem, bearerRegex)
	return ctx
}
