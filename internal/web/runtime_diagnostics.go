package web

import (
	"net/http"
	"runtime"
	"time"
)

var runtimeDiagnosticsStarted = time.Now()

func (s *Server) registerRuntimeDiagnostics() {
	s.mux.HandleFunc("GET /api/team/diagnostics/runtime", func(w http.ResponseWriter, r *http.Request) {
		if !teamOrigin(w, r) {
			return
		}
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		writeJSON(w, 200, map[string]any{"goroutines": runtime.NumGoroutine(), "heapAllocBytes": m.HeapAlloc, "heapInuseBytes": m.HeapInuse, "heapSysBytes": m.HeapSys, "systemBytes": m.Sys, "gcCount": m.NumGC, "uptimeSeconds": time.Since(runtimeDiagnosticsStarted).Seconds()})
	})
}
