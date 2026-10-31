package web

import (
	"net/http"
	"strings"
	"testing"
)

func TestLoopbackHostAndCrossOriginGuards(t *testing.T) {
	ts := newTestServer(t)
	for _, v := range []struct{ method, path, host, origin string }{{"GET", "/api/team/missions", "attacker.invalid:8766", ""}, {"POST", "/api/requirements", "attacker.invalid:8766", "http://attacker.invalid:8766"}, {"POST", "/api/requirements", "", "https://example.invalid"}} {
		request, err := http.NewRequest(v.method, ts.URL+v.path, strings.NewReader(`{"title":"Blocked change"}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Content-Type", "application/json")
		if v.host != "" {
			request.Host = v.host
		}
		if v.origin != "" {
			request.Header.Set("Origin", v.origin)
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("untrusted Host/origin accepted: %+v status %d", v, response.StatusCode)
		}
	}
}
