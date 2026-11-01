package web

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestModuleLinksUseActualSuitePorts(t *testing.T) {
	t.Setenv("AI_DEVOPS_INFRASCOUT_PORT", "23456")
	t.Setenv("AI_DEVOPS_FLEETSCOPE_PORT", "23457")
	t.Setenv("AI_DEVOPS_RELEASEGUARD_PORT", "23458")
	srv := newTestServer(t)
	res, err := http.Get(srv.URL + "/api/team/module-links")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var ports map[string]int
	if err = json.NewDecoder(res.Body).Decode(&ports); err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || ports["8765"] != 23456 || ports["8770"] != 23457 || ports["8771"] != 23458 {
		t.Fatalf("incorrect Suite links: %d %v", res.StatusCode, ports)
	}
	t.Setenv("AI_DEVOPS_INFRASCOUT_PORT", "invalid")
	bad, err := http.Get(srv.URL + "/api/team/module-links")
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusInternalServerError {
		t.Fatalf("invalid port was accepted: %d", bad.StatusCode)
	}
}
