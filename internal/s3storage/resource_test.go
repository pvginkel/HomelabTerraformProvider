package s3storage_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

// These tests drive homelab_s3_storage through the provider's protocol server
// the way Terraform does — upgrade, refresh, plan, apply — against a fake RGW,
// so they run without TF_ACC.

const (
	storageType = "homelab_s3_storage"
	testOwner   = "release-prd"
	testBucket  = "release-prd-data"
)

// preGrantStateJSON is the state of a release deployed before
// grant_backup_reader existed.
const preGrantStateJSON = `{"id":"release-prd","name":"release-prd","buckets":["release-prd-data"],"key_rotation":null,"access_key_id":"OWNER","secret_access_key":"owner-secret"}`

var (
	grantAsked    = tftypes.NewValue(tftypes.Bool, true)
	grantUnset    = tftypes.NewValue(tftypes.Bool, nil)
	grantUnmet    = tftypes.NewValue(tftypes.Bool, false)
	bucketSetType = tftypes.Set{ElementType: tftypes.String}
)

// fakeRGW answers the Admin Ops reads a refresh makes for testOwner and keeps
// bucket policies, recording every policy request.
type fakeRGW struct {
	*httptest.Server
	mu          sync.Mutex
	policies    map[string]string
	policyCalls []string
}

func newFakeRGW(t *testing.T) *fakeRGW {
	t.Helper()
	f := &fakeRGW{policies: map[string]string{}}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.URL.Query()["policy"]; ok {
			f.servePolicy(t, w, r)
			return
		}
		var body any
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/user":
			body = admin.User{ID: testOwner, Keys: []admin.UserKeySpec{{User: testOwner, AccessKey: "OWNER", SecretKey: "owner-secret"}}}
		case r.Method == http.MethodGet && r.URL.Path == "/admin/bucket":
			body = admin.Bucket{Bucket: r.URL.Query().Get("bucket"), Owner: testOwner}
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

func (f *fakeRGW) servePolicy(t *testing.T, w http.ResponseWriter, r *http.Request) {
	bucket := strings.TrimPrefix(r.URL.Path, "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.policyCalls = append(f.policyCalls, r.Method+" "+bucket)
	switch r.Method {
	case http.MethodGet:
		policy, ok := f.policies[bucket]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, "<Error><Code>NoSuchBucketPolicy</Code></Error>")
			return
		}
		_, _ = io.WriteString(w, policy)
	case http.MethodPut:
		policy, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read policy body: %v", err)
		}
		f.policies[bucket] = string(policy)
		w.WriteHeader(http.StatusNoContent)
	case http.MethodDelete:
		delete(f.policies, bucket)
		w.WriteHeader(http.StatusNoContent)
	default:
		t.Errorf("unexpected policy request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusBadRequest)
	}
}

func (f *fakeRGW) hasPolicy(bucket string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.policies[bucket]
	return ok
}

func (f *fakeRGW) removePolicy(bucket string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.policies, bucket)
}

func (f *fakeRGW) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.policyCalls...)
}

// harness is a configured provider server with homelab_s3_storage's type.
type harness struct {
	t      *testing.T
	ctx    context.Context
	server tfprotov6.ProviderServer
	typ    tftypes.Object
}

// newHarness configures the provider's s3 group against endpoint, naming reader
// as s3_backup_reader unless it is empty.
func newHarness(t *testing.T, endpoint, reader string) *harness {
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
	h.typ = schemas.ResourceSchemas[storageType].ValueType().(tftypes.Object)

	attrs := map[string]tftypes.Value{
		"s3_endpoint":         tftypes.NewValue(tftypes.String, endpoint),
		"s3_admin_access_key": tftypes.NewValue(tftypes.String, "admin-ak"),
		"s3_admin_secret_key": tftypes.NewValue(tftypes.String, "admin-sk"),
	}
	if reader != "" {
		attrs["s3_backup_reader"] = tftypes.NewValue(tftypes.String, reader)
	}
	providerTyp := schemas.Provider.ValueType().(tftypes.Object)
	resp, err := h.server.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{
		TerraformVersion: "1.9.0",
		Config:           h.dynamic(objectValue(providerTyp, attrs)),
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

// state is testOwner's resource state with the given grant_backup_reader. With
// every computed attribute already known it is also the proposed new state for
// a config carrying that grant.
func (h *harness) state(grant tftypes.Value) tftypes.Value {
	return objectValue(h.typ, map[string]tftypes.Value{
		"id":                  tftypes.NewValue(tftypes.String, testOwner),
		"name":                tftypes.NewValue(tftypes.String, testOwner),
		"buckets":             tftypes.NewValue(bucketSetType, []tftypes.Value{tftypes.NewValue(tftypes.String, testBucket)}),
		"access_key_id":       tftypes.NewValue(tftypes.String, "OWNER"),
		"secret_access_key":   tftypes.NewValue(tftypes.String, "owner-secret"),
		"grant_backup_reader": grant,
	})
}

// config is testOwner's resource as written in HCL.
func (h *harness) config(grant tftypes.Value) tftypes.Value {
	return objectValue(h.typ, map[string]tftypes.Value{
		"name":                tftypes.NewValue(tftypes.String, testOwner),
		"buckets":             tftypes.NewValue(bucketSetType, []tftypes.Value{tftypes.NewValue(tftypes.String, testBucket)}),
		"grant_backup_reader": grant,
	})
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
		TypeName: storageType,
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
		TypeName:     storageType,
		CurrentState: h.dynamic(current),
	})
	if err != nil {
		h.t.Fatalf("ReadResource: %v", err)
	}
	h.requireNoErrors("ReadResource", resp.Diagnostics)
	return h.value("ReadResource", resp.NewState)
}

// plan plans prior towards a config carrying grant and fails on a replacement.
func (h *harness) plan(prior, grant tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         storageType,
		PriorState:       h.dynamic(prior),
		ProposedNewState: h.dynamic(h.state(grant)),
		Config:           h.dynamic(h.config(grant)),
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

func (h *harness) apply(prior, planned, grant tftypes.Value) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     storageType,
		PriorState:   h.dynamic(prior),
		PlannedState: h.dynamic(planned),
		Config:       h.dynamic(h.config(grant)),
	})
	if err != nil {
		h.t.Fatalf("ApplyResourceChange: %v", err)
	}
	h.requireNoErrors("ApplyResourceChange", resp.Diagnostics)
	return h.value("ApplyResourceChange", resp.NewState)
}

// A release that does not ask for the grant — every release before the grant
// existed — refreshes and plans no change, whether or not the provider names a
// reader, and no bucket policy is touched.
func TestStorageWithoutAskPlansNoChange(t *testing.T) {
	for _, tc := range []struct{ name, reader string }{
		{name: "no reader", reader: ""},
		{name: "reader configured", reader: "backup-reader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rgw := newFakeRGW(t)
			h := newHarness(t, rgw.URL, tc.reader)

			prior := h.upgrade(preGrantStateJSON)
			if want := h.state(grantUnset); !prior.Equal(want) {
				t.Fatalf("upgraded state = %s, want %s", prior, want)
			}
			refreshed := h.read(prior)
			if !refreshed.Equal(prior) {
				t.Errorf("refresh changed state to %s", refreshed)
			}
			if planned := h.plan(refreshed, grantUnset); !planned.Equal(refreshed) {
				t.Errorf("plan is not empty: %s", planned)
			}
			if calls := rgw.calls(); len(calls) != 0 {
				t.Errorf("bucket policy requests: %v", calls)
			}
		})
	}
}

// A release asking for the grant on a provider with no reader — the dev
// cluster — plans only the ask being added; the apply and every later refresh
// write, delete and check no bucket policy, and the next plan is empty.
func TestStorageGrantWithoutReaderIsInert(t *testing.T) {
	rgw := newFakeRGW(t)
	h := newHarness(t, rgw.URL, "")

	prior := h.upgrade(preGrantStateJSON)
	planned := h.plan(prior, grantAsked)
	if want := h.state(grantAsked); !planned.Equal(want) {
		t.Fatalf("planned %s, want only the ask added: %s", planned, want)
	}
	applied := h.apply(prior, planned, grantAsked)
	if !applied.Equal(planned) {
		t.Fatalf("applied state %s differs from the plan %s", applied, planned)
	}

	refreshed := h.read(applied)
	if !refreshed.Equal(applied) {
		t.Errorf("refresh changed state to %s", refreshed)
	}
	if replanned := h.plan(refreshed, grantAsked); !replanned.Equal(refreshed) {
		t.Errorf("plan after apply is not empty: %s", replanned)
	}
	if calls := rgw.calls(); len(calls) != 0 {
		t.Errorf("bucket policy requests: %v", calls)
	}
}

// With a reader configured, the ask updates an existing release in place and
// writes the policy; a policy removed out of band refreshes as drift and the
// next apply restores it; dropping the ask deletes it.
func TestStorageGrantWithReader(t *testing.T) {
	rgw := newFakeRGW(t)
	h := newHarness(t, rgw.URL, "backup-reader")

	prior := h.upgrade(preGrantStateJSON)
	state := h.apply(prior, h.plan(prior, grantAsked), grantAsked)
	if !rgw.hasPolicy(testBucket) {
		t.Fatal("apply wrote no policy on the bucket")
	}
	if refreshed := h.read(state); !refreshed.Equal(state) {
		t.Errorf("refresh of a granted bucket changed state to %s", refreshed)
	}

	rgw.removePolicy(testBucket)
	drifted := h.read(state)
	if want := h.state(grantUnmet); !drifted.Equal(want) {
		t.Fatalf("refresh after the policy was removed = %s, want %s", drifted, want)
	}
	state = h.apply(drifted, h.plan(drifted, grantAsked), grantAsked)
	if !rgw.hasPolicy(testBucket) {
		t.Fatal("apply did not restore the removed policy")
	}
	if refreshed := h.read(state); !refreshed.Equal(state) {
		t.Errorf("refresh after restore changed state to %s", refreshed)
	}

	state = h.apply(state, h.plan(state, grantUnset), grantUnset)
	if rgw.hasPolicy(testBucket) {
		t.Error("dropping the ask left the policy in place")
	}
	if refreshed := h.read(state); !refreshed.Equal(state) {
		t.Errorf("refresh after revoke changed state to %s", refreshed)
	}
}
