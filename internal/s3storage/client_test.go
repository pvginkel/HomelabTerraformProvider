package s3storage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"

	"github.com/ceph/go-ceph/rgw/admin"
)

// fakeBucketCreator stands in for the aws-sdk PutBucket path so the rgw-only
// logic is testable without a real RGW. It records the buckets it was asked to
// create.
type fakeBucketCreator struct {
	created []string
	err     error
}

func (f *fakeBucketCreator) CreateBucket(_ context.Context, bucket string) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, bucket)
	return nil
}

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *fakeBucketCreator) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	api, err := admin.New(srv.URL, "admin-ak", "admin-sk", srv.Client())
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	fake := &fakeBucketCreator{}
	return &Client{api: api, s3: fake}, fake
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func writeStatusError(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"Code": code})
}

func hasKeyQuery(r *http.Request) bool {
	_, ok := r.URL.Query()["key"]
	return ok
}

func TestClientCreate(t *testing.T) {
	var linked []string
	client, fake := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/admin/user":
			q := r.URL.Query()
			if q.Get("uid") != "release-a" {
				t.Errorf("uid = %q", q.Get("uid"))
			}
			if q.Get("max-buckets") != "-1" {
				t.Errorf("max-buckets = %q, want -1", q.Get("max-buckets"))
			}
			writeJSON(t, w, admin.User{
				ID:   "release-a",
				Keys: []admin.UserKeySpec{{User: "release-a", AccessKey: "AKIA", SecretKey: "secret"}},
			})
		case r.Method == http.MethodPut && r.URL.Path == "/admin/bucket":
			linked = append(linked, r.URL.Query().Get("bucket"))
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	})

	st, err := client.Create(context.Background(), "release-a", []string{"b1", "b2"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if st.AccessKeyID != "AKIA" || st.SecretAccessKey != "secret" {
		t.Errorf("unexpected key: %+v", st)
	}
	sort.Strings(fake.created)
	if len(fake.created) != 2 || fake.created[0] != "b1" || fake.created[1] != "b2" {
		t.Errorf("created buckets = %v", fake.created)
	}
	sort.Strings(linked)
	if len(linked) != 2 || linked[0] != "b1" || linked[1] != "b2" {
		t.Errorf("linked buckets = %v", linked)
	}
}

func TestClientReadFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/admin/user":
			writeJSON(t, w, admin.User{
				ID: "release-a",
				Keys: []admin.UserKeySpec{
					{User: "release-a", AccessKey: "OLD", SecretKey: "olds"},
					{User: "release-a", AccessKey: "AKIA", SecretKey: "secret"},
				},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/admin/bucket":
			b := r.URL.Query().Get("bucket")
			switch b {
			case "mine":
				writeJSON(t, w, admin.Bucket{Bucket: "mine", Owner: "release-a"})
			case "theirs":
				writeJSON(t, w, admin.Bucket{Bucket: "theirs", Owner: "someone-else"})
			case "gone":
				writeStatusError(w, http.StatusNotFound, "NoSuchBucket")
			default:
				t.Errorf("unexpected bucket %q", b)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	st, found, err := client.Read(context.Background(), "release-a", "AKIA", []string{"mine", "theirs", "gone"})
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	if st.AccessKeyID != "AKIA" || st.SecretAccessKey != "secret" {
		t.Errorf("selected wrong key: %+v", st)
	}
	if len(st.Buckets) != 1 || st.Buckets[0] != "mine" {
		t.Errorf("buckets = %v, want [mine]", st.Buckets)
	}
}

func TestClientReadNotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		writeStatusError(w, http.StatusNotFound, "NoSuchUser")
	})

	_, found, err := client.Read(context.Background(), "missing", "AKIA", nil)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
}

func TestClientRotateKey(t *testing.T) {
	var removedOld bool
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/admin/user" && hasKeyQuery(r):
			writeJSON(t, w, []admin.UserKeySpec{
				{User: "release-a", AccessKey: "OLD", SecretKey: "olds"},
				{User: "release-a", AccessKey: "NEW", SecretKey: "news"},
			})
		case r.Method == http.MethodDelete && r.URL.Path == "/admin/user" && hasKeyQuery(r):
			if got := r.URL.Query().Get("access-key"); got != "OLD" {
				t.Errorf("removed access-key = %q, want OLD", got)
			}
			removedOld = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
		}
	})

	access, secret, err := client.RotateKey(context.Background(), "release-a", "OLD")
	if err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	if access != "NEW" || secret != "news" {
		t.Errorf("rotated to %q/%q, want NEW/news", access, secret)
	}
	if !removedOld {
		t.Error("old key was not removed")
	}
}

func TestClientDelete(t *testing.T) {
	var removedBuckets []string
	var removedUser bool
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodDelete && r.URL.Path == "/admin/bucket":
			if r.URL.Query().Get("purge-objects") != "true" {
				t.Errorf("purge-objects = %q, want true", r.URL.Query().Get("purge-objects"))
			}
			removedBuckets = append(removedBuckets, r.URL.Query().Get("bucket"))
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/admin/user":
			removedUser = true
			w.WriteHeader(http.StatusOK)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	})

	if err := client.Delete(context.Background(), "release-a", []string{"b1", "b2"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	sort.Strings(removedBuckets)
	if len(removedBuckets) != 2 || removedBuckets[0] != "b1" || removedBuckets[1] != "b2" {
		t.Errorf("removed buckets = %v", removedBuckets)
	}
	if !removedUser {
		t.Error("user was not removed")
	}
}

func TestDiffBuckets(t *testing.T) {
	added, removed := diffBuckets([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	sort.Strings(added)
	sort.Strings(removed)
	if len(added) != 1 || added[0] != "d" {
		t.Errorf("added = %v, want [d]", added)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Errorf("removed = %v, want [a]", removed)
	}
}
