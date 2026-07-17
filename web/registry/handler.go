package registry

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// Handler 提供 registry 的 HTTP 路由处理。
type Handler struct {
	store *Store
}

// NewHandler 构造 registry HTTP handler。
func NewHandler(store *Store) *Handler {
	return &Handler{store: store}
}

// RegisterRoutes 在指定的 ServeMux 上注册 registry 路由。
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/registry/heartbeat", h.handleHeartbeat)
	mux.HandleFunc("GET /api/v1/registry/agents", h.handleListAgents)
}

// POST /api/v1/registry/heartbeat
// Body: AgentInfo JSON → 204 No Content
func (h *Handler) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	var info AgentInfo
	if err := json.NewDecoder(r.Body).Decode(&info); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if info.Hostname == "" {
		http.Error(w, "hostname required", http.StatusBadRequest)
		return
	}

	if err := h.store.Heartbeat(r.Context(), info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/registry/agents?online=true
// 返回 AgentInfo[]，online 参数可选。
func (h *Handler) handleListAgents(w http.ResponseWriter, r *http.Request) {
	onlineOnly, _ := strconv.ParseBool(r.URL.Query().Get("online"))

	agents, err := h.store.ListAgents(r.Context(), onlineOnly)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	writeJSON(w, agents)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
