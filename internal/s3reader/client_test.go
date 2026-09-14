package s3reader

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	api, err := admin.New(srv.URL, "admin-ak", "admin-sk", srv.Client())
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	return &Client{api: api}
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func writeStatusError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"Code": code})
}

func TestClientCreate(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/admin/user" {
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			return
		}
		q := r.URL.Query()
		if q.Get("uid") != "backup-reader" {
			t.Errorf("uid = %q", q.Get("uid"))
		}
		if q.Get("max-buckets") != "-1" {
			t.Errorf("max-buckets = %q, want -1", q.Get("max-buckets"))
		}
		if q.Get("user-caps") != "buckets=read" {
			t.Errorf("user-caps = %q, want buckets=read", q.Get("user-caps"))
		}
		writeJSON(t, w, admin.User{
			ID:   "backup-reader",
			Keys: []admin.UserKeySpec{{User: "backup-reader", AccessKey: "AKIA", SecretKey: "secret"}},
			Caps: []admin.UserCapSpec{{Type: "buckets", Perm: "read"}},
		})
	})

	rd, err := client.Create(context.Background(), "backup-reader")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if rd.Name != "backup-reader" || rd.AccessKeyID != "AKIA" || rd.SecretAccessKey != "secret" {
		t.Errorf("unexpected reader: %+v", rd)
	}
}

func TestClientCreateWithoutKey(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, admin.User{ID: "backup-reader"})
	})

	if _, err := client.Create(context.Background(), "backup-reader"); err == nil {
		t.Fatal("Create succeeded for a user returned without a key")
	}
}

func TestClientReadFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/admin/user" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		writeJSON(t, w, admin.User{
			ID: "backup-reader",
			Keys: []admin.UserKeySpec{
				{User: "backup-reader", AccessKey: "OTHER", SecretKey: "others"},
				{User: "backup-reader", AccessKey: "AKIA", SecretKey: "secret"},
			},
		})
	})

	rd, found, err := client.Read(context.Background(), "backup-reader", "AKIA")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if rd.AccessKeyID != "AKIA" || rd.SecretAccessKey != "secret" {
		t.Errorf("selected wrong key: %+v", rd)
	}
}

func TestClientReadNotFound(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeStatusError(w, http.StatusNotFound, "NoSuchUser")
	})

	_, found, err := client.Read(context.Background(), "backup-reader", "AKIA")
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
}

func TestClientDelete(t *testing.T) {
	var removed string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/admin/user" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			return
		}
		removed = r.URL.Query().Get("uid")
		w.WriteHeader(http.StatusOK)
	})

	if err := client.Delete(context.Background(), "backup-reader"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if removed != "backup-reader" {
		t.Errorf("removed uid = %q, want backup-reader", removed)
	}
}

func TestClientDeleteMissing(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeStatusError(w, http.StatusNotFound, "NoSuchUser")
	})

	if err := client.Delete(context.Background(), "backup-reader"); err != nil {
		t.Fatalf("Delete of a missing user: %v", err)
	}
}
