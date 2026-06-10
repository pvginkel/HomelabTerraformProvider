package zfsdataset

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

const testToken = "test-token"

// newTestClient stands up an httptest server and a Client whose pools map points
// the given pool at that server's host:port.
func newTestClient(t *testing.T, pool string, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return NewClient(map[string]string{pool: u.Hostname()}, testToken, port, "test"), srv
}

func assertCommonHeaders(t *testing.T, r *http.Request, expectBody bool) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.Header.Get("User-Agent"); got != "terraform-provider-homelab/test" {
		t.Errorf("User-Agent = %q", got)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q", got)
	}
	if expectBody && r.Header.Get("Content-Type") != "application/json" {
		t.Errorf("Content-Type = %q", r.Header.Get("Content-Type"))
	}
}

func TestPut_Created(t *testing.T) {
	client, _ := newTestClient(t, "zpool2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		// The full dataset path is a single URL-encoded segment.
		if r.URL.EscapedPath() != "/zfs/datasets/zpool2%2Fk8s%2Fdata" {
			t.Errorf("escaped path = %s", r.URL.EscapedPath())
		}
		assertCommonHeaders(t, r, true)

		var spec Spec
		if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if spec.Quota != "20G" || spec.Compression != "lz4" {
			t.Errorf("spec = %+v", spec)
		}
		if spec.Properties["atime"] != "off" {
			t.Errorf("properties = %+v", spec.Properties)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Dataset{
			Dataset: "zpool2/k8s/data", Pool: "zpool2", Name: "k8s/data",
			Quota: "20G", Recordsize: "128K", Compression: "lz4",
			Mountpoint: "/zpool2/k8s/data", GUID: "12970251740876153671",
			Available: 21474836480, Mounted: true,
			Properties: map[string]string{"atime": "off"},
		})
	})

	ds, err := client.Put(context.Background(), "zpool2", "k8s/data", Spec{
		Quota: "20G", Recordsize: "128K", Compression: "lz4", Mountpoint: "/zpool2/k8s/data",
		Properties: map[string]string{"atime": "off"},
	})
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if ds.GUID != "12970251740876153671" || ds.Mountpoint != "/zpool2/k8s/data" {
		t.Errorf("dataset = %+v", ds)
	}
}

func TestGet_OK(t *testing.T) {
	client, _ := newTestClient(t, "zpool2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		assertCommonHeaders(t, r, false)
		_ = json.NewEncoder(w).Encode(Dataset{
			Dataset: "zpool2/k8s/data", Pool: "zpool2", Name: "k8s/data", GUID: "1",
		})
	})

	ds, err := client.Get(context.Background(), "zpool2", "k8s/data")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if ds.Name != "k8s/data" {
		t.Errorf("name = %q", ds.Name)
	}
}

func TestGet_NotFound(t *testing.T) {
	client, _ := newTestClient(t, "zpool2", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "not_found", Message: "no such dataset"})
	})

	_, err := client.Get(context.Background(), "zpool2", "k8s/missing")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, got %v", err)
	}
}

func TestDelete_NoContent(t *testing.T) {
	client, _ := newTestClient(t, "zpool2", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.Delete(context.Background(), "zpool2", "k8s/data"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDelete_NotFoundIsError(t *testing.T) {
	client, _ := newTestClient(t, "zpool2", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "not_found", Message: "gone"})
	})

	err := client.Delete(context.Background(), "zpool2", "k8s/data")
	if !IsNotFound(err) {
		t.Fatalf("IsNotFound = false, got %v", err)
	}
}

func TestUnmappedPool(t *testing.T) {
	client := NewClient(map[string]string{"zpool2": "srvk8s2"}, testToken, 9655, "test")

	_, err := client.Get(context.Background(), "rpool", "k8s/data")
	var unmapped *UnmappedPoolError
	if err == nil || !strings.Contains(err.Error(), "no zfs_pools mapping") {
		t.Fatalf("want UnmappedPoolError, got %v", err)
	}
	if e, ok := err.(*UnmappedPoolError); !ok || e.Pool != "rpool" {
		t.Errorf("error = %+v", err)
	}
	_ = unmapped
}

func TestPut_Unauthorized(t *testing.T) {
	client, _ := newTestClient(t, "zpool2", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "unauthorized", Message: "invalid token"})
	})

	_, err := client.Put(context.Background(), "zpool2", "k8s/data", Spec{})
	if err == nil {
		t.Fatal("expected error")
	}
	if IsNotFound(err) {
		t.Errorf("IsNotFound = true on 401")
	}
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != http.StatusUnauthorized {
		t.Errorf("unexpected error: %+v", err)
	}
}
