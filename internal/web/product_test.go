package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/GeneJie199/ai-dev-cycle-manager/internal/app"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeDiagnosticsRealHTTPNumericOnlyAndOriginHostGates(t *testing.T) {
	s := newTestServer(t)
	resp, e := http.Get(s.URL + "/api/team/diagnostics/runtime")
	if e != nil {
		t.Fatal(e)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	var values map[string]float64
	if e = json.NewDecoder(resp.Body).Decode(&values); e != nil {
		t.Fatal(e)
	}
	if len(values) != 7 || values["goroutines"] < 1 || values["heapAllocBytes"] < 1 || values["uptimeSeconds"] < 0 {
		t.Fatalf("invalid real metrics %+v", values)
	}
	for _, test := range []struct{ host, origin, fetch string }{{"attacker.example", "", ""}, {"", "https://attacker.example", ""}, {"", "", "cross-site"}} {
		req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/team/diagnostics/runtime", nil)
		if test.host != "" {
			req.Host = test.host
		}
		req.Header.Set("Origin", test.origin)
		req.Header.Set("Sec-Fetch-Site", test.fetch)
		response, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		response.Body.Close()
		if response.StatusCode != 403 {
			t.Fatalf("diagnostics gate bypass: %+v %s", test, response.Status)
		}
	}
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/api/team/diagnostics/runtime", nil)
	req.Header.Set("Origin", s.URL)
	response, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("same-origin metrics refused")
	}
}
func TestOnboardingWithoutModelsIsUsableAndSimpleHasNoJSONEditor(t *testing.T) {
	s := newTestServer(t)
	response, e := http.Get(s.URL + "/api/product/onboarding")
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	var status struct {
		State     string `json:"state"`
		Providers []struct {
			State string `json:"state"`
		} `json:"providers"`
	}
	if e = json.NewDecoder(response.Body).Decode(&status); e != nil {
		t.Fatal(e)
	}
	if status.State != "configuration_required" && status.State != "unavailable" {
		t.Fatal("empty installation falsely ready")
	}
	for _, p := range status.Providers {
		if p.State == "ready" {
			t.Fatal("unconfigured model marked ready")
		}
	}
	page, e := http.Get(s.URL + "/team.html")
	if e != nil {
		t.Fatal(e)
	}
	defer page.Body.Close()
	raw, _ := io.ReadAll(page.Body)
	html := string(raw)
	for _, old := range []string{`id="models"`, `id="expert"`, `Capability Registry（JSON）`} {
		if strings.Contains(html, old) {
			t.Fatal("Simple still requires JSON editor")
		}
	}
	for _, needed := range []string{`id="provider-form"`, `id="project-form"`, `id="check-preset"`, `id="delivery-content"`} {
		if !strings.Contains(html, needed) {
			t.Fatal("missing actual product form", needed)
		}
	}
}
func TestProductModelTestRequiresExplicitAuthorization(t *testing.T) {
	s := newTestServer(t)
	r, e := http.Post(s.URL+"/api/product/providers/codex/test", "application/json", strings.NewReader(`{"authorize":false}`))
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatal("model call without explicit consent accepted", r.Status)
	}
}

type credentialFixture struct {
	value string
	fail  bool
}

func (s *credentialFixture) Set(_ context.Context, id, value string) (string, error) {
	if s.fail {
		return "", errors.New("backend error " + value)
	}
	s.value = value
	return "keyring:" + id, nil
}
func (s *credentialFixture) Get(context.Context, string) (string, error) {
	if s.value == "" {
		return "", errors.New("missing fixture credential")
	}
	return s.value, nil
}
func (s *credentialFixture) Delete(context.Context, string) error { s.value = ""; return nil }
func TestProviderWebCredentialNeverEchoesAndSecureStorageFailureIsSafe(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "saved", true: "backend-failure"}[fail], func(t *testing.T) {
			a, e := app.New(filepath.Join(t.TempDir(), "db"))
			if e != nil {
				t.Fatal(e)
			}
			defer a.Close()
			secret := &credentialFixture{fail: fail}
			a.AI.Secrets = secret
			handler, e := NewServer(a, nil)
			if e != nil {
				t.Fatal(e)
			}
			server := httptest.NewServer(handler)
			defer server.Close()
			value := "synthetic-opaque-credential-not-for-response"
			body := `{"transport":"api","provider":{"id":"fixture-api","name":"Fixture API","kind":"openai_compatible","baseUrl":"https://example.invalid/v1","secret":"` + value + `"},"model":"fixture-model","contextLimit":16000,"tier":2,"roles":["lead","explorer","qa","review"],"capabilities":["coding","exploration","testing","review"]}`
			response, e := http.Post(server.URL+"/api/product/providers", "application/json", strings.NewReader(body))
			if e != nil {
				t.Fatal(e)
			}
			raw, _ := io.ReadAll(response.Body)
			response.Body.Close()
			if strings.Contains(string(raw), value) || strings.Contains(string(raw), "keyring:") {
				t.Fatal("credential or secret ref echoed")
			}
			if fail {
				if response.StatusCode < 400 {
					t.Fatal("secure storage failure accepted")
				}
				return
			}
			if response.StatusCode != 200 || secret.value != value {
				t.Fatalf("credential not safely configured: %s %s", response.Status, raw)
			}
			onboarding, e := http.Get(server.URL + "/api/product/onboarding")
			if e != nil {
				t.Fatal(e)
			}
			raw, _ = io.ReadAll(onboarding.Body)
			onboarding.Body.Close()
			if strings.Contains(string(raw), value) || strings.Contains(string(raw), "keyring:") {
				t.Fatal("onboarding exposed credential")
			}
			request, _ := http.NewRequest(http.MethodDelete, server.URL+"/api/product/providers/fixture-api", nil)
			revoked, e := http.DefaultClient.Do(request)
			if e != nil {
				t.Fatal(e)
			}
			revoked.Body.Close()
			if revoked.StatusCode != 200 || secret.value != "" {
				t.Fatal("credential revoke failed")
			}
		})
	}
}

func TestUnifiedShellAndSuiteStaticResourcesRealHTTP(t *testing.T) {
	s := newTestServer(t)
	for _, path := range []string{"/", "/team.html", "/suite.html"} {
		response, e := http.Get(s.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 {
			t.Fatal(path, response.Status)
		}
		text := string(raw)
		for _, required := range []string{`data-suite-product=`, `/suite-design.css`, `/suite-ui.js`, `data-ui-main`} {
			if !strings.Contains(text, required) {
				t.Fatal(path, "missing shared shell", required)
			}
		}
	}
	for path, want := range map[string]string{"/suite-design.css": "d6e8bd267739cf445a78c58294f3e06383892588fad109f9ee1b3c9934ed7ffc", "/suite-ui.js": "759c0361723dca54b7ac1234c698d4d19b7f70b1602fed906a88a57afd431633"} {
		response, e := http.Get(s.URL + path)
		if e != nil {
			t.Fatal(e)
		}
		raw, _ := io.ReadAll(response.Body)
		response.Body.Close()
		sum := sha256.Sum256(raw)
		if response.StatusCode != 200 || hex.EncodeToString(sum[:]) != want {
			t.Fatal("shared asset bytes changed", path)
		}
	}
}
