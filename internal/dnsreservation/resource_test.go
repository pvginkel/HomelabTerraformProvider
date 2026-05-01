package dnsreservation

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestMacValidator(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		null    bool
		unknown bool
		wantErr bool
	}{
		{name: "valid lowercase", input: "02:a7:f3:03:84:00"},
		{name: "valid all zeros", input: "00:00:00:00:00:00"},
		{name: "valid digits and letters", input: "0a:1b:2c:3d:4e:5f"},
		{name: "uppercase rejected", input: "02:A7:F3:03:84:00", wantErr: true},
		{name: "mixed case rejected", input: "02:A7:f3:03:84:00", wantErr: true},
		{name: "missing colons", input: "02a7f30384 00", wantErr: true},
		{name: "wrong byte length", input: "02:a7:f3:03:84", wantErr: true},
		{name: "extra byte", input: "02:a7:f3:03:84:00:11", wantErr: true},
		{name: "non-hex char", input: "0g:a7:f3:03:84:00", wantErr: true},
		{name: "empty", input: "", wantErr: true},
		{name: "null skipped", null: true},
		{name: "unknown skipped", unknown: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var cv types.String
			switch {
			case tc.null:
				cv = types.StringNull()
			case tc.unknown:
				cv = types.StringUnknown()
			default:
				cv = types.StringValue(tc.input)
			}
			req := validator.StringRequest{
				Path:        path.Root("mac"),
				ConfigValue: cv,
			}
			resp := &validator.StringResponse{}
			macValidator{}.ValidateString(context.Background(), req, resp)

			if resp.Diagnostics.HasError() != tc.wantErr {
				t.Fatalf("HasError = %v, want %v (diagnostics: %v)", resp.Diagnostics.HasError(), tc.wantErr, resp.Diagnostics)
			}
		})
	}
}
