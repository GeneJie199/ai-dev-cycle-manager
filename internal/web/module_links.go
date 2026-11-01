package web

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
)

func (s *Server) handleModuleLinks(w http.ResponseWriter, r *http.Request) {
	ports := map[string]int{"8765": 8765, "8770": 8770, "8771": 8771}
	for defaultPort, variable := range map[string]string{"8765": "AI_DEVOPS_INFRASCOUT_PORT", "8770": "AI_DEVOPS_FLEETSCOPE_PORT", "8771": "AI_DEVOPS_RELEASEGUARD_PORT"} {
		if value, present := os.LookupEnv(variable); present {
			port, err := strconv.Atoi(value)
			if err != nil || port < 1 || port > 65535 {
				writeError(w, http.StatusInternalServerError, fmt.Sprintf("invalid Suite port configuration: %s", variable))
				return
			}
			ports[defaultPort] = port
		}
	}
	writeJSON(w, http.StatusOK, ports)
}
