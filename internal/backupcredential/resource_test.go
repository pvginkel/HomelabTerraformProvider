package backupcredential_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/pvginkel/HomelabTerraformProvider/internal/backupcredential"
	"github.com/pvginkel/HomelabTerraformProvider/internal/provider"
)

// These tests drive homelab_backup_credential through the provider's protocol
// server the way Terraform does — upgrade, refresh, plan, apply — against a
// fake backup server, so they run without TF_ACC.

const (
	credentialType = "homelab_backup_credential"
	testScope      = "youtrack"
	firstToken     = "tok-1"
)

// preRotationStateJSON is the state of a credential created before rotation
// existed.
const preRotationStateJSON = `{"id":"youtrack","scope":"youtrack","retention":10,"token":"tok-1"}`

var (
	rotationUnset = tftypes.NewValue(tftypes.String, nil)
	firstEpoch    = tftypes.NewValue(tftypes.String, "2026-10-10T02:00:00Z")
	secondEpoch   = tftypes.NewValue(tftypes.String, "2026-10-24T02:00:00Z")
	unknownToken  = tftypes.NewValue(tftypes.String, tftypes.UnknownValue)
)

// fakeBackupServer holds testScope's credential with the server's semantics:
// a PUT mints a token only when it creates the scope. It records every
// mutating request.
type fakeBackupServer struct {
	*httptest.Server
	mu        sync.Mutex
	record    *backupcredential.Credential
	minted    int
	mutations []string
}

func newFakeBackupServer(t *testing.T) *fakeBackupServer {
	t.Helper()
	f := &fakeBackupServer{
		record: &backupcredential.Credential{Scope: testScope, Token: firstToken, Retention: 10, Kind: "upload"},
		minted: 1,
	}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/credentials/"+testScope {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != http.MethodGet {
			f.mutations = append(f.mutations, r.Method)
		}
		switch r.Method {
		case http.MethodGet:
			if f.record == nil {
				writeResponse(t, w, http.StatusNotFound, map[string]string{"error": "not_found", "message": "no such credential"})
				return
			}
			writeResponse(t, w, http.StatusOK, f.record)
		case http.MethodDelete:
			if f.record == nil {
				writeResponse(t, w, http.StatusNotFound, map[string]string{"error": "not_found", "message": "no such credential"})
				return
			}
			f.record = nil
			w.WriteHeader(http.StatusNoContent)
		case http.MethodPut:
			var body struct {
				Retention int    `json:"retention"`
				Kind      string `json:"kind"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode PUT body: %v", err)
			}
			if f.record != nil {
				f.record.Retention = body.Retention
				writeResponse(t, w, http.StatusOK, f.record)
				return
			}
			f.minted++
			f.record = &backupcredential.Credential{Scope: testScope, Token: fmt.Sprintf("tok-%d", f.minted), Retention: body.Retention, Kind: body.Kind}
			writeResponse(t, w, http.StatusCreated, f.record)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func writeResponse(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// takeMutations returns the mutating requests since the last call.
func (f *fakeBackupServer) takeMutations() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	m := f.mutations
	f.mutations = nil
	return m
}

// harness is a configured provider server with homelab_backup_credential's type.
type harness struct {
	t      *testing.T
	ctx    context.Context
	server tfprotov6.ProviderServer
	typ    tftypes.Object
}

func newHarness(t *testing.T, url string) *harness {
	t.Helper()
	// Other groups' triggers come from the environment unless cleared.
	for _, env := range []string{"HOMELAB_DNS_RESERVATION_URL", "HOMELAB_CEPH_MON_HOST", "HOMELAB_S3_ENDPOINT"} {
		t.Setenv(env, "")
	}

	h := &harness{t: t, ctx: context.Background(), server: providerserver.NewProtocol6(provider.New("test")())()}
	schemas, err := h.server.GetProviderSchema(h.ctx, &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatalf("GetProviderSchema: %v", err)
	}
	h.requireNoErrors("GetProviderSchema", schemas.Diagnostics)
	h.typ = schemas.ResourceSchemas[credentialType].ValueType().(tftypes.Object)

	providerTyp := schemas.Provider.ValueType().(tftypes.Object)
	resp, err := h.server.ConfigureProvider(h.ctx, &tfprotov6.ConfigureProviderRequest{
		TerraformVersion: "1.9.0",
		Config: h.dynamic(objectValue(providerTyp, map[string]tftypes.Value{
			"backup_server_url":   tftypes.NewValue(tftypes.String, url),
			"backup_server_token": tftypes.NewValue(tftypes.String, "management-token"),
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

// state is testScope's resource state. With the prior token it is also the
// proposed new state for a config carrying rotation and retention.
func (h *harness) state(rotation tftypes.Value, retention int64, token tftypes.Value) tftypes.Value {
	return objectValue(h.typ, map[string]tftypes.Value{
		"id":        tftypes.NewValue(tftypes.String, testScope),
		"scope":     tftypes.NewValue(tftypes.String, testScope),
		"retention": tftypes.NewValue(tftypes.Number, retention),
		"rotation":  rotation,
		"token":     token,
	})
}

// config is testScope's resource as written in HCL.
func (h *harness) config(rotation tftypes.Value, retention int64) tftypes.Value {
	return objectValue(h.typ, map[string]tftypes.Value{
		"scope":     tftypes.NewValue(tftypes.String, testScope),
		"retention": tftypes.NewValue(tftypes.Number, retention),
		"rotation":  rotation,
	})
}

func (h *harness) token(state tftypes.Value) tftypes.Value {
	h.t.Helper()
	var attrs map[string]tftypes.Value
	if err := state.As(&attrs); err != nil {
		h.t.Fatalf("decode state %s: %v", state, err)
	}
	return attrs["token"]
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
		TypeName: credentialType,
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
		TypeName:     credentialType,
		CurrentState: h.dynamic(current),
	})
	if err != nil {
		h.t.Fatalf("ReadResource: %v", err)
	}
	h.requireNoErrors("ReadResource", resp.Diagnostics)
	return h.value("ReadResource", resp.NewState)
}

// plan plans prior towards a config carrying rotation and retention and fails
// on a replacement.
func (h *harness) plan(prior, rotation tftypes.Value, retention int64) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.PlanResourceChange(h.ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         credentialType,
		PriorState:       h.dynamic(prior),
		ProposedNewState: h.dynamic(h.state(rotation, retention, h.token(prior))),
		Config:           h.dynamic(h.config(rotation, retention)),
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

func (h *harness) apply(prior, planned, rotation tftypes.Value, retention int64) tftypes.Value {
	h.t.Helper()
	resp, err := h.server.ApplyResourceChange(h.ctx, &tfprotov6.ApplyResourceChangeRequest{
		TypeName:     credentialType,
		PriorState:   h.dynamic(prior),
		PlannedState: h.dynamic(planned),
		Config:       h.dynamic(h.config(rotation, retention)),
	})
	if err != nil {
		h.t.Fatalf("ApplyResourceChange: %v", err)
	}
	h.requireNoErrors("ApplyResourceChange", resp.Diagnostics)
	return h.value("ApplyResourceChange", resp.NewState)
}

func tokenValue(token string) tftypes.Value {
	return tftypes.NewValue(tftypes.String, token)
}

// A credential created before rotation existed, its config not setting it,
// refreshes and plans no change, and the server sees no write.
func TestCredentialWithoutRotationPlansNoChange(t *testing.T) {
	srv := newFakeBackupServer(t)
	h := newHarness(t, srv.URL)

	prior := h.upgrade(preRotationStateJSON)
	if want := h.state(rotationUnset, 10, tokenValue(firstToken)); !prior.Equal(want) {
		t.Fatalf("upgraded state = %s, want %s", prior, want)
	}
	refreshed := h.read(prior)
	if !refreshed.Equal(prior) {
		t.Errorf("refresh changed state to %s", refreshed)
	}
	if planned := h.plan(refreshed, rotationUnset, 10); !planned.Equal(refreshed) {
		t.Errorf("plan is not empty: %s", planned)
	}
	if m := srv.takeMutations(); len(m) != 0 {
		t.Errorf("mutating requests: %v", m)
	}
}

// Setting rotation on that credential plans the token unknown in place; the
// apply deletes and re-creates the scope, which mints a new token; the next
// plan is empty, and each later change of the value mints again.
func TestCredentialRotation(t *testing.T) {
	srv := newFakeBackupServer(t)
	h := newHarness(t, srv.URL)

	prior := h.upgrade(preRotationStateJSON)
	tokens := []string{firstToken}
	for _, epoch := range []tftypes.Value{firstEpoch, secondEpoch} {
		planned := h.plan(prior, epoch, 10)
		if want := h.state(epoch, 10, unknownToken); !planned.Equal(want) {
			t.Fatalf("planned %s, want the token unknown: %s", planned, want)
		}
		applied := h.apply(prior, planned, epoch, 10)
		if m := srv.takeMutations(); !slices.Equal(m, []string{http.MethodDelete, http.MethodPut}) {
			t.Errorf("rotation sent %v, want DELETE then PUT", m)
		}
		var token string
		if err := h.token(applied).As(&token); err != nil {
			t.Fatalf("decode token: %v", err)
		}
		if slices.Contains(tokens, token) {
			t.Fatalf("rotation to %s kept a token already held: %q", epoch, token)
		}
		tokens = append(tokens, token)
		if want := h.state(epoch, 10, tokenValue(token)); !applied.Equal(want) {
			t.Fatalf("applied state = %s, want %s", applied, want)
		}

		refreshed := h.read(applied)
		if !refreshed.Equal(applied) {
			t.Errorf("refresh after rotation changed state to %s", refreshed)
		}
		if replanned := h.plan(refreshed, epoch, 10); !replanned.Equal(refreshed) {
			t.Errorf("plan after rotation is not empty: %s", replanned)
		}
		if m := srv.takeMutations(); len(m) != 0 {
			t.Errorf("refresh and plan sent %v", m)
		}
		prior = refreshed
	}
}

// A retention change with rotation unchanged updates the scope in place and
// keeps its token.
func TestCredentialRetentionKeepsToken(t *testing.T) {
	srv := newFakeBackupServer(t)
	h := newHarness(t, srv.URL)

	prior := h.upgrade(preRotationStateJSON)
	planned := h.plan(prior, rotationUnset, 20)
	if want := h.state(rotationUnset, 20, tokenValue(firstToken)); !planned.Equal(want) {
		t.Fatalf("planned %s, want only the retention changed: %s", planned, want)
	}
	applied := h.apply(prior, planned, rotationUnset, 20)
	if !applied.Equal(planned) {
		t.Errorf("applied state %s differs from the plan %s", applied, planned)
	}
	if m := srv.takeMutations(); !slices.Equal(m, []string{http.MethodPut}) {
		t.Errorf("retention change sent %v, want one PUT", m)
	}
}
