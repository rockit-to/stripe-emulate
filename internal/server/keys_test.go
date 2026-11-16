package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chelokot/stripe-emulate/internal/config"
	"github.com/chelokot/stripe-emulate/internal/storage"
)

const testKeySecret = "test-key-secret"

func newKeyedServer(t *testing.T) *Server {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "emulator.db")
	store, err := storage.OpenSQLite(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	srv := New(Options{Config: config.Config{Addr: ":0", DatabaseURL: dbPath, StaticDir: "web/dist", Environment: "test", KeySecret: testKeySecret}, Store: store})
	t.Cleanup(func() {
		_ = srv.Close()
		_ = store.Close()
	})
	return srv
}

func keyed(t *testing.T, srv http.Handler, method, path, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+key)
	if body != "" {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func customerEmails(t *testing.T, srv http.Handler, key string) []string {
	t.Helper()
	rec := keyed(t, srv, http.MethodGet, "/v1/customers", key, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list customers with %s: %d %s", key, rec.Code, rec.Body.String())
	}
	var out struct {
		Data []struct {
			Email string `json:"email"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	emails := []string{}
	for _, c := range out.Data {
		emails = append(emails, c.Email)
	}
	return emails
}

func TestRunKeySelectsItsRun(t *testing.T) {
	srv := newKeyedServer(t)
	teamA, teamB := RunKey(testKeySecret, "team-a"), RunKey(testKeySecret, "team-b")

	if rec := keyed(t, srv, http.MethodPost, "/v1/customers", teamA, "email=a@example.test"); rec.Code != http.StatusOK {
		t.Fatalf("create with team a's key: %d %s", rec.Code, rec.Body.String())
	}
	if got := customerEmails(t, srv, teamA); len(got) != 1 || got[0] != "a@example.test" {
		t.Fatalf("team a sees %v", got)
	}
	if got := customerEmails(t, srv, teamB); len(got) != 0 {
		t.Fatalf("team b sees team a's customers: %v", got)
	}
	if got := customerEmails(t, srv, "sk_test_emulator"); len(got) != 0 {
		t.Fatalf("the default run sees team a's customers: %v", got)
	}
	// The same run as its path names it.
	if got := countCustomersPath(t, srv, "/runs/team-a/v1/customers"); got != 1 {
		t.Fatalf("/runs/team-a lists %d customers, want 1", got)
	}
}

func TestRunKeyAsBasicAuth(t *testing.T) {
	srv := newKeyedServer(t)
	key := RunKey(testKeySecret, "team-a")
	keyed(t, srv, http.MethodPost, "/v1/customers", key, "email=a@example.test")

	req := httptest.NewRequest(http.MethodGet, "/v1/customers", nil)
	req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(key+":")))
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "a@example.test") {
		t.Fatalf("basic auth with team a's key: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRunKeyWithAWrongSignatureIsRefused(t *testing.T) {
	srv := newKeyedServer(t)
	forged := "sk_test_team-a_" + strings.Repeat("0", 24)
	rec := keyed(t, srv, http.MethodGet, "/v1/customers", forged, "")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged key: %d, want 401", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Invalid API Key provided: sk_test_") || strings.Contains(rec.Body.String(), "team-a") {
		t.Fatalf("forged key answer shows the key: %s", rec.Body.String())
	}
}

func TestRunKeyCheckoutLinksUnderItsRun(t *testing.T) {
	srv := newKeyedServer(t)
	key := RunKey(testKeySecret, "team-a")
	rec := keyed(t, srv, http.MethodPost, "/v1/customers", key, "id=cus_guest&email=guest@example.test")
	if rec.Code != http.StatusOK {
		t.Fatalf("create customer: %d %s", rec.Code, rec.Body.String())
	}
	body := "customer=cus_guest&mode=payment&success_url=https://shop.example/ok&cancel_url=https://shop.example/no" +
		"&line_items[0][quantity]=1&line_items[0][price_data][currency]=eur" +
		"&line_items[0][price_data][unit_amount]=12000&line_items[0][price_data][product_data][name]=Stay"
	rec = keyed(t, srv, http.MethodPost, "/v1/checkout/sessions", key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("create checkout session: %d %s", rec.Code, rec.Body.String())
	}
	var session struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &session); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !strings.Contains(session.URL, "/runs/team-a/") {
		t.Fatalf("checkout url %q is not under team a's run", session.URL)
	}
	if rec := keyed(t, srv, http.MethodGet, "/v1/checkout/sessions/"+session.ID, RunKey(testKeySecret, "team-b"), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("team b reads team a's session: %d", rec.Code)
	}
}

func TestKeysSelectNothingWithoutASecret(t *testing.T) {
	srv, _ := newRunServer(t)
	key := RunKey(testKeySecret, "team-a")
	keyed(t, srv, http.MethodPost, "/v1/customers", key, "email=a@example.test")
	if got := customerEmails(t, srv, "sk_test_emulator"); len(got) != 1 {
		t.Fatalf("without a secret every key is the default run's: %v", got)
	}
}
