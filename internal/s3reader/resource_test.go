package s3reader_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

// These tests drive homelab_s3_reader through the provider's protocol server
// the way Terraform does — upgrade, refresh, plan, apply — against a fake RGW,
// so they run without TF_ACC.

const (
	readerType = "homelab_s3_reader"
	testReader = "backup-reader"
)

// preRotationStateJSON is the state of a reader created before key_rotation
// existed.
const preRotationStateJSON = `{"id":"backup-reader","name":"backup-reader","access_key_id":"AK1","secret_access_key":"sk1"}`

var (
	rotationUnset = tftypes.NewValue(tftypes.String, nil)
	firstEpoch    = tftypes.NewValue(tftypes.String, "2026-10-10T02:00:00Z")
	secondEpoch   = tftypes.NewValue(tftypes.String, "2026-10-24T02:00:00Z")
	unknownString = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
)

// fakeRGW holds testReader's keys and answers the Admin Ops requests the
// resource makes, recording every key request.
type fakeRGW struct {
	*httptest.Server
	mu        sync.Mutex
	keys      []admin.UserKeySpec
	minted    int
	mutations []string
}

func newFakeRGW(t *testing.T) *fakeRGW {
	t.Helper()
	f := &fakeRGW{keys: []admin.UserKeySpec{{User: testReader, AccessKey: "AK1", SecretKey: "sk1"}}, minted: 1}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		_, keyOp := q["key"]
		if r.URL.Path != "/admin/user" || q.Get("uid") != testReader {
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		var body any
		switch {
		case r.Method == http.MethodGet && !keyOp:
			body = admin.User{ID: testReader, Keys: f.keys}
		case r.Method == http.MethodPut && keyOp:
			f.mutations = append(f.mutations, "create key")
			f.minted++
			f.keys = append(f.keys, admin.UserKeySpec{
				User:      testReader,
				AccessKey: fmt.Sprintf("AK%d", f.minted),
				SecretKey: fmt.Sprintf("sk%d", f.minted),
			})
			body = f.keys
		case r.Method == http.MethodDelete && keyOp:
			access := q.Get("access-key")
			f.mutations = append(f.mutations, "remove key "+access)
			f.keys = slices.DeleteFunc(f.keys, func(k admin.UserKeySpec) bool { return k.AccessKey == access })
			w.WriteHeader(http.StatusOK)
			return
		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if err := json.NewEncoder(w).Encode(body); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// takeMutations returns the key requests since the last call.
func (f *fakeRGW) takeMutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.mutations
	f.mutations = nil
	return m
}

func (f *fakeRGW) accessKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, k := range f.keys {
		out = append(out, k.AccessKey)
	}
	return out
}

// harness is a configured provider server with homelab_s3_reader's type.
type harness struct {
	t      *testing.T
	ctx    context.Context
	server tfprotov6.ProviderServer
	typ    tftypes.Object
}

func newHarness(t *testing.T, endpoint string) *harness {
	t.Helper()
	// Other groups' triggers and the reader fallback come from the environment
	// unless cleared.
	for _, env := range []string{"HOMELAB_DNS_RESERVATION_URL", "HOMELAB_BACKUP_SERVER_URL", "HOMELAB_CEPH_MON_HOST", "HOMELAB_S3_BACKUP_READER"} {
		t.Setenv(env, "")
	}

	h := &harness{t: t, ctx: context.Background(), server: providerserver.NewProtocol6(provider.New("test")())()}
	schemas, err := h.server.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}
	h.requireNoErrors("GetProviderSchema", schemas.Diagnostics)
	h.typ = schemas.ResourceSchemas[readerType].ValueType().(tftypes.Object)

	providerTyp := schemas.Provider.ValueType().(tftypes.Object)
	resp, err := h.server.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{
		TerraformVersion: "1.9.0",
		Config: h.dynamic(objectValue(providerTyp, map[string]tftypes.Value{
			"s3_endpoint":         tftypes.NewValue(tftypes.String, endpoint),
			"s3_admin_access_key": tftypes.NewValue(tftypes.String, "admin-ak"),
			"s3_admin_secret_key": tftypes.NewValue(tftypes.String, "admin-sk"),
		})),
	})
	if err != nil {
		t.Fatalf("ConfigureProvider: %v", err)
	}
	h.requireNoErrors("ConfigureProvider", resp.Diagnostics)
	return h
}

func objectValue(typ tftypes.Object, attrs map[string]tftypes.Value) tftypes.Value {
	vals := make(map[string]tftypes.Value, len(typ.AttributeTypes))
	for name, attrTyp := range typ.AttributeTypes {
		vals[name] = tftypes.NewValue(attrTyp, nil)
	}
	for name, v := range attrs {
		vals[name] = v
	}
	return tftypes.NewValue(typ, vals)
}

// state is testReader's resource state. With the prior key it is also the
// proposed new state for a config carrying rotation.
func (h *harness) state(rotation, access, secret tftypes.Value) tftypes.Value {
	return objectValue(h.typ, map[string]tftypes.Value{
		"id":                tftypes.NewValue(tftypes.String, testReader),
		"name":              tftypes.NewValue(tftypes.String, testReader),
		"key_rotation":      rotation,
		"access_key_id":     access,
		"secret_access_key": secret,
	})
}

func (h *harness) keyState(rotation tftypes.Value, access, secret string) tftypes.Value {
	return h.state(rotation, tftypes.NewValue(tftypes.String, access), tftypes.NewValue(tftypes.String, secret))
}

// config is testReader's resource as written in HCL.
func (h *harness) config(rotation tftypes.Value) tftypes.Value {
	return objectValue(h.typ, map[string]tftypes.Value{
		"name":         tftypes.NewValue(tftypes.String, testReader),
		"key_rotation": rotation,
	})
}

func (h *harness) attr(state tftypes.Value, name string) tftypes.Value {
	h.t.Helper()
	var attrs map[string]tftypes.Value
	if err := state.As(&attrs); err != nil {
		h.t.Fatalf("decode state %s: %v", state, err)
	}
	return attrs[name]
}

func (h *harness) dynamic(v tftypes.Value) *tfprotov6.DynamicValue {
	h.t.Helper()
	dv, err := tfprotov6.NewDynamicValue(v.Type(), v)
	if err != nil {
		h.t.Fatalf("encode %s: %v", v, err)
	}
	return &dv
}

func (h *harness) value(op string, dv *tfprotov6.DynamicValue) tftypes.Value {
	h.t.Helper()
	if dv == nil {
		h.t.Fatalf("%s returned no state", op)
	}
	v, err := dv.Unmarshal(h.typ)
	if err != nil {
		h.t.Fatalf("%s: decode state: %v", op, err)
	}
	return v
}

func (h *harness) requireNoErrors(op string, diags []*tfprotov6.Diagnostic) {
	h.t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			h.t.Fatalf("%s: %s: %s", op, d.Summary, d.Detail)
		}
	}
}

func (h *harness) upgrade(rawJSON string) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.UpgradeResourceState(h.ctx, &tfprotov6.UpgradeResourceStateRequest{
		TypeName: readerType,
		RawState: &tfprotov6.RawState{JSON: []byte(rawJSON)},
	})
	if err != nil {
		h.t.Fatalf("UpgradeResourceState: %v", err)
	}
	h.requireNoErrors("UpgradeResourceState", resp.Diagnostics)
	return h.value("UpgradeResourceState", resp.UpgradedState)
}

func (h *harness) read(current tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ReadResource(h.ctx, &tfprotov6.ReadResourceRequest{
		TypeName:     readerType,
		CurrentState: h.dynamic(current),
	})
	if err != nil {
		h.t.Fatalf("ReadResource: %v", err)
	}
	h.requireNoErrors("ReadResource", resp.Diagnostics)
	return h.value("ReadResource", resp.NewState)
}

// plan plans prior towards a config carrying rotation and fails on a
// replacement.
func (h *harness) plan(prior, rotation tftypes.Value) tftypes.Value {
	h.t.Helper()
	proposed := h.state(rotation, h.attr(prior, "access_key_id"), h.attr(prior, "secret_access_key"))
	resp, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         readerType,
		PriorState:       h.dynamic(prior),
		ProposedNewState: h.dynamic(proposed),
		Config:           h.dynamic(h.config(rotation)),
	})
	if err != nil {
		h.t.Fatalf("PlanResourceChange: %v", err)
	}
	h.requireNoErrors("PlanResourceChange", resp.Diagnostics)
	if len(resp.RequiresReplace) != 0 {
		h.t.Fatalf("plan replaces the resource over %v", resp.RequiresReplace)
	}
	return h.value("PlanResourceChange", resp.PlannedState)
}

func (h *harness) apply(prior, planned, rotation tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     readerType,
		PriorState:   h.dynamic(prior),
		PlannedState: h.dynamic(planned),
		Config:       h.dynamic(h.config(rotation)),
	})
	if err != nil {
		h.t.Fatalf("ApplyResourceChange: %v", err)
	}
	h.requireNoErrors("ApplyResourceChange", resp.Diagnostics)
	return h.value("ApplyResourceChange", resp.NewState)
}

// A reader created before key_rotation existed, its config not setting it,
// refreshes and plans no change, and RGW sees no key request.
func TestReaderWithoutRotationPlansNoChange(t *testing.T) {
	rgw := newFakeRGW(t)
	h := newHarness(t, rgw.URL)

	prior := h.upgrade(preRotationStateJSON)
	if want := h.keyState(rotationUnset, "AK1", "sk1"); !prior.Equal(want) {
		t.Fatalf("upgraded state = %s, want %s", prior, want)
	}
	refreshed := h.read(prior)
	if !refreshed.Equal(prior) {
		t.Errorf("refresh changed state to %s", refreshed)
	}
	if planned := h.plan(refreshed, rotationUnset); !planned.Equal(refreshed) {
		t.Errorf("plan is not empty: %s", planned)
	}
	if m := rgw.takeMutations(); len(m) != 0 {
		t.Errorf("key requests: %v", m)
	}
}

// Setting key_rotation on that reader plans the key unknown in place; the
// apply mints a new key on the same user and removes the old one; the next plan
// is empty, and each later change of the value mints again.
func TestReaderRotation(t *testing.T) {
	rgw := newFakeRGW(t)
	h := newHarness(t, rgw.URL)

	prior := h.upgrade(preRotationStateJSON)
	for i, epoch := range []tftypes.Value{firstEpoch, secondEpoch} {
		old, next := fmt.Sprintf("AK%d", i+1), fmt.Sprintf("AK%d", i+2)

		planned := h.plan(prior, epoch)
		if want := h.state(epoch, unknownString, unknownString); !planned.Equal(want) {
			t.Fatalf("planned %s, want the key unknown: %s", planned, want)
		}
		applied := h.apply(prior, planned, epoch)
		if m := rgw.takeMutations(); !slices.Equal(m, []string{"create key", "remove key " + old}) {
			t.Errorf("rotation sent %v, want a new key, then %s removed", m, old)
		}
		if want := h.keyState(epoch, next, fmt.Sprintf("sk%d", i+2)); !applied.Equal(want) {
			t.Fatalf("applied state = %s, want %s", applied, want)
		}
		if keys := rgw.accessKeys(); !slices.Equal(keys, []string{next}) {
			t.Errorf("user keys after rotation = %v, want only %s", keys, next)
		}

		refreshed := h.read(applied)
		if !refreshed.Equal(applied) {
			t.Errorf("refresh after rotation changed state to %s", refreshed)
		}
		if replanned := h.plan(refreshed, epoch); !replanned.Equal(refreshed) {
			t.Errorf("plan after rotation is not empty: %s", replanned)
		}
		if m := rgw.takeMutations(); len(m) != 0 {
			t.Errorf("refresh and plan sent %v", m)
		}
		prior = refreshed
	}
}
