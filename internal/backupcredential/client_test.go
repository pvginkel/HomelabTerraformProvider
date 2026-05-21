package backupcredential

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "test-token"

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, testToken, "test"), srv
}

func assertCommonHeaders(t *testing.T, r *http.Request, expectBody bool) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer "+testToken {
		t.Errorf("Authorization = %q, want Bearer %s", got, testToken)
	}
	if got := r.Header.Get("User-Agent"); got != "terraform-provider-homelab/test" {
		t.Errorf("User-Agent = %q, want terraform-provider-homelab/test", got)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("Accept = %q, want application/json", got)
	}
	if expectBody {
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", got)
		}
	}
}

func TestClientPut_Created(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/credentials/electronics-inventory" {
			t.Errorf("path = %s", r.URL.Path)
		}
		assertCommonHeaders(t, r, true)

		var body putRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Retention != 10 {
			t.Errorf("body.Retention = %d", body.Retention)
		}
		if body.Kind != "upload" {
			t.Errorf("body.Kind = %q, want upload", body.Kind)
		}

		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Credential{
			Scope: "electronics-inventory", Token: "tok-aaa", Retention: body.Retention, Kind: body.Kind,
		})
	})

	res, err := client.Put(context.Background(), "electronics-inventory", 10)
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	if res.Scope != "electronics-inventory" || res.Token != "tok-aaa" || res.Retention != 10 || res.Kind != "upload" {
		t.Errorf("unexpected credential: %+v", res)
	}
}

func TestClientPut_Updated(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(Credential{
			Scope: "electronics-inventory", Token: "tok-aaa", Retention: 20, Kind: "upload",
		})
	})

	res, err := client.Put(context.Background(), "electronics-inventory", 20)
	if err != nil {
		t.Fatalf("Put returned error: %v", err)
	}
	if res.Token != "tok-aaa" {
		t.Errorf("expected token preserved, got %q", res.Token)
	}
	if res.Retention != 20 {
		t.Errorf("retention = %d, want 20", res.Retention)
	}
}

func TestClientPut_InvalidRetention(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "invalid_retention", Message: "retention must be between 1 and 100."})
	})

	_, err := client.Put(context.Background(), "electronics-inventory", 0)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Status != http.StatusBadRequest || apiErr.Code != "invalid_retention" {
		t.Errorf("unexpected api error: %+v", apiErr)
	}
	if !strings.Contains(err.Error(), "invalid_retention: retention") {
		t.Errorf("Error() = %q, want prefix invalid_retention:", err.Error())
	}
}

func TestClientPut_Unauthorized(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "unauthorized", Message: "invalid token"})
	})

	_, err := client.Put(context.Background(), "electronics-inventory", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if IsNotFound(err) {
		t.Errorf("IsNotFound = true on 401")
	}
}

func TestClientPut_BadRequestNoEnvelope(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "not json")
	})

	_, err := client.Put(context.Background(), "electronics-inventory", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected *APIError, got %T", err)
	}
	if apiErr.Code != "" {
		t.Errorf("Code = %q, want empty fallback", apiErr.Code)
	}
	if apiErr.Message != http.StatusText(http.StatusBadRequest) {
		t.Errorf("Message = %q, want %q", apiErr.Message, http.StatusText(http.StatusBadRequest))
	}
}

func TestClientGet_OK(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s", r.Method)
		}
		if r.URL.Path != "/credentials/electronics-inventory" {
			t.Errorf("path = %s", r.URL.Path)
		}
		assertCommonHeaders(t, r, false)

		_ = json.NewEncoder(w).Encode(Credential{
			Scope: "electronics-inventory", Token: "tok-aaa", Retention: 10, Kind: "upload",
		})
	})

	res, err := client.Get(context.Background(), "electronics-inventory")
	if err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if res.Scope != "electronics-inventory" {
		t.Errorf("scope = %q", res.Scope)
	}
	if res.Token != "tok-aaa" {
		t.Errorf("token = %q", res.Token)
	}
}

func TestClientGet_NotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "not_found", Message: "no such credential"})
	})

	_, err := client.Get(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false, want true")
	}
}

func TestClientDelete_NoContent(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s", r.Method)
		}
		assertCommonHeaders(t, r, false)
		w.WriteHeader(http.StatusNoContent)
	})

	if err := client.Delete(context.Background(), "electronics-inventory"); err != nil {
		t.Fatalf("Delete returned error: %v", err)
	}
}

func TestClientDelete_NotFound(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(errorEnvelope{Error: "not_found", Message: "gone"})
	})

	err := client.Delete(context.Background(), "electronics-inventory")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false, want true")
	}
}

func TestClientInternalError_NoEnvelope(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := client.Put(context.Background(), "electronics-inventory", 10)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "Internal Server Error") {
		t.Errorf("Error() = %q, want it to contain Internal Server Error", err.Error())
	}
}

func TestClientTrailingSlashBaseURL(t *testing.T) {
	var seenPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(Credential{
			Scope: "electronics-inventory", Token: "tok-aaa", Retention: 10, Kind: "upload",
		})
	}))
	t.Cleanup(srv.Close)

	client := NewClient(srv.URL+"/", testToken, "test")
	if _, err := client.Get(context.Background(), "electronics-inventory"); err != nil {
		t.Fatalf("Get returned error: %v", err)
	}
	if seenPath != "/credentials/electronics-inventory" {
		t.Errorf("path = %q, want /credentials/electronics-inventory", seenPath)
	}
}
