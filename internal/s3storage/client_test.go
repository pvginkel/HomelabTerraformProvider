package s3storage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
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
	return &Client{api: api, s3: fake, endpoint: srv.URL, reader: "backup-reader"}, fake
}

// policyBucket reports the bucket of an S3 bucket-policy request with the given
// method.
func policyBucket(r *http.Request, method string) (string, bool) {
	if _, ok := r.URL.Query()["policy"]; !ok || r.Method != method {
		return "", false
	}
	return strings.TrimPrefix(r.URL.Path, "/"), true
}

// signingKey is the access key id in a SigV4 Authorization header.
func signingKey(r *http.Request) string {
	_, cred, _ := strings.Cut(r.Header.Get("Authorization"), "Credential=")
	key, _, _ := strings.Cut(cred, "/")
	return key
}

func writeS3Error(w http.ResponseWriter, status int, code string) {
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<Error><Code>%s</Code></Error>", code)
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

func TestClientGrantReader(t *testing.T) {
	type statement struct {
		Effect    string
		Principal map[string][]string
		Action    []string
		Resource  []string
	}
	policies := map[string][]statement{}
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		bucket, ok := policyBucket(r, http.MethodPut)
		if !ok {
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			return
		}
		if got := signingKey(r); got != "OWNER" {
			t.Errorf("policy on %q signed with %q, want the owner's key", bucket, got)
		}
		var doc struct {
			Version   string
			Statement []statement
		}
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&doc); err != nil {
			t.Errorf("decode policy on %q: %v", bucket, err)
		}
		policies[bucket] = doc.Statement
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.GrantReader(context.Background(), "OWNER", "owner-secret", []string{"b1", "b2"}); err != nil {
		t.Fatalf("GrantReader: %v", err)
	}
	if len(policies) != 2 {
		t.Fatalf("policies written on %d buckets, want 2", len(policies))
	}
	for bucket, statements := range policies {
		want := map[string]string{
			"s3:ListBucket": "arn:aws:s3:::" + bucket,
			"s3:GetObject":  "arn:aws:s3:::" + bucket + "/*",
		}
		for _, s := range statements {
			principal := s.Principal["AWS"]
			if s.Effect != "Allow" || len(s.Principal) != 1 || len(principal) != 1 || principal[0] != "arn:aws:iam:::user/backup-reader" {
				t.Errorf("bucket %q: statement %+v does not allow exactly the reader", bucket, s)
			}
			if len(s.Action) != 1 || len(s.Resource) != 1 || want[s.Action[0]] != s.Resource[0] {
				t.Errorf("bucket %q: statement grants %v on %v", bucket, s.Action, s.Resource)
				continue
			}
			delete(want, s.Action[0])
		}
		if len(want) != 0 {
			t.Errorf("bucket %q: policy does not grant %v", bucket, want)
		}
	}
}

func TestClientRevokeReader(t *testing.T) {
	var revoked []string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		bucket, ok := policyBucket(r, http.MethodDelete)
		if !ok {
			t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
			return
		}
		if got := signingKey(r); got != "OWNER" {
			t.Errorf("policy delete on %q signed with %q, want the owner's key", bucket, got)
		}
		revoked = append(revoked, bucket)
		if bucket == "no-policy" {
			writeS3Error(w, http.StatusNotFound, "NoSuchBucketPolicy")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.RevokeReader(context.Background(), "OWNER", "owner-secret", []string{"b1", "no-policy"}); err != nil {
		t.Fatalf("RevokeReader: %v", err)
	}
	sort.Strings(revoked)
	if len(revoked) != 2 || revoked[0] != "b1" || revoked[1] != "no-policy" {
		t.Errorf("revoked buckets = %v", revoked)
	}
}

func TestClientReaderGranted(t *testing.T) {
	// The reader policy re-laid out, as a store that does not keep the written
	// bytes would return it.
	stored := func(reader, bucket string) string {
		var doc any
		if err := json.Unmarshal([]byte(readerPolicy(reader, bucket)), &doc); err != nil {
			t.Fatalf("unmarshal reader policy: %v", err)
		}
		out, err := json.MarshalIndent(doc, "", "  ")
		if err != nil {
			t.Fatalf("marshal reader policy: %v", err)
		}
		return string(out)
	}

	cases := []struct {
		name    string
		b2      func(w http.ResponseWriter)
		want    bool
		wantErr bool
	}{
		{
			name: "every bucket granted",
			b2:   func(w http.ResponseWriter) { _, _ = io.WriteString(w, stored("backup-reader", "b2")) },
			want: true,
		},
		{
			name: "a bucket without a policy",
			b2:   func(w http.ResponseWriter) { writeS3Error(w, http.StatusNotFound, "NoSuchBucketPolicy") },
		},
		{
			name: "a policy naming another user",
			b2:   func(w http.ResponseWriter) { _, _ = io.WriteString(w, stored("someone-else", "b2")) },
		},
		{
			name: "another bucket's policy",
			b2:   func(w http.ResponseWriter) { _, _ = io.WriteString(w, stored("backup-reader", "b1")) },
		},
		{
			name:    "policy read refused",
			b2:      func(w http.ResponseWriter) { writeS3Error(w, http.StatusForbidden, "AccessDenied") },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				bucket, ok := policyBucket(r, http.MethodGet)
				if !ok {
					t.Errorf("unexpected request %s %s?%s", r.Method, r.URL.Path, r.URL.RawQuery)
					return
				}
				if got := signingKey(r); got != "OWNER" {
					t.Errorf("policy read on %q signed with %q, want the owner's key", bucket, got)
				}
				switch bucket {
				case "b1":
					_, _ = io.WriteString(w, stored("backup-reader", "b1"))
				case "b2":
					tc.b2(w)
				default:
					t.Errorf("unexpected bucket %q", bucket)
				}
			})

			granted, err := client.ReaderGranted(context.Background(), "OWNER", "owner-secret", []string{"b1", "b2"})
			if (err != nil) != tc.wantErr {
				t.Fatalf("ReaderGranted error = %v, wantErr %v", err, tc.wantErr)
			}
			if granted != tc.want {
				t.Errorf("ReaderGranted = %v, want %v", granted, tc.want)
			}
		})
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
