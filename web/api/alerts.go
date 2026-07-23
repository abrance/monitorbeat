package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/web/alerts"
	"github.com/abrance/monitorbeat/web/vm"
)

// alertHandler holds alert-related HTTP handlers. It owns access to the
// alerts store and (for test-query) the VM client.
type alertHandler struct {
	store *alerts.Store
	vm    vmQuerier
}

// vmQuerier is the minimal VM surface needed by the alert test-query
// endpoint. *vm.Client satisfies it.
type vmQuerier interface {
	Query(ctx context.Context, expr string) ([]vm.Vector, error)
}

func (h *alertHandler) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListRules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rules)
}

func (h *alertHandler) createRule(w http.ResponseWriter, r *http.Request) {
	var req alerts.CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if req.Name == "" || req.Expr == "" {
		writeBadRequest(w, "name, expr required")
		return
	}

	rule := &alerts.AlertRule{
		Name:        req.Name,
		Enabled:     true,
		Expr:        req.Expr,
		Duration:    req.Duration,
		Description: req.Description,
	}
	if req.Enabled != nil {
		rule.Enabled = *req.Enabled
	}
	if err := h.store.CreateRule(rule); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			writeBadRequest(w, "rule name already exists")
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rule)
}

func (h *alertHandler) updateRule(w http.ResponseWriter, r *http.Request, id int64) {
	var req alerts.CreateRuleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid JSON")
		return
	}
	if req.Name == "" || req.Expr == "" {
		writeBadRequest(w, "name, expr required")
		return
	}
	rule, err := h.store.UpdateRule(id, req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, rule)
}

func (h *alertHandler) deleteRule(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.store.DeleteRule(id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *alertHandler) acknowledge(w http.ResponseWriter, r *http.Request) {
	var req alerts.AckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid JSON")
		return
	}
	if req.RuleID <= 0 || req.Fingerprint == "" {
		writeBadRequest(w, "rule_id, fingerprint required")
		return
	}
	var sil *time.Time
	if req.SilenceHours > 0 {
		t := time.Now().Add(time.Duration(req.SilenceHours) * time.Hour)
		sil = &t
	}
	if err := h.store.SetAcknowledged(req.RuleID, req.Fingerprint, sil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "acknowledged"})
}

func (h *alertHandler) listHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ruleID, _ := strconv.ParseInt(q.Get("rule_id"), 10, 64)
	fingerprint := q.Get("fingerprint")
	state := q.Get("state")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	items, total, err := h.store.ListHistory(ruleID, fingerprint, state, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"total": total, "items": items})
}

// testQuery runs an ad-hoc PromQL expression and returns the matching
// instances without persisting state. Useful for the UI "test query"
// button. Failures map to:
//   - 400: empty expr or VM-declared query error
//   - 502: VM transport failure
func (h *alertHandler) testQuery(w http.ResponseWriter, r *http.Request) {
	if h.vm == nil {
		writeBadRequest(w, "vm client not available")
		return
	}
	var req alerts.TestQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeBadRequest(w, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.Expr) == "" {
		writeBadRequest(w, "expr is required")
		return
	}
	results, err := h.vm.Query(r.Context(), req.Expr)
	if err != nil {
		// Treat decode / query errors as 502 since they originate from
		// the upstream VM.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
		return
	}
	out := alerts.TestQueryResponse{Result: make([]alerts.TestQueryInstance, 0, len(results))}
	for _, v := range results {
		labels := v.Metric
		if labels == nil {
			labels = map[string]string{}
		}
		out.Result = append(out.Result, alerts.TestQueryInstance{
			Fingerprint: alerts.Fingerprint(labels),
			Labels:      labels,
			Value:       v.Value[1],
		})
	}
	writeJSON(w, out)
}

type alertStatusItem struct {
	RuleID       int64             `json:"rule_id"`
	RuleName     string            `json:"rule_name"`
	Fingerprint  string            `json:"fingerprint"`
	Hostname     string            `json:"hostname"`
	Labels       map[string]string `json:"labels"`
	Status       string            `json:"status"`
	Acknowledged bool              `json:"acknowledged"`
	Since        string            `json:"since"`
}

type alertStatusResponse struct {
	FiringCount       int               `json:"firing_count"`
	AcknowledgedCount int               `json:"acknowledged_count"`
	Items             []alertStatusItem `json:"items"`
}

func (h *alertHandler) status(w http.ResponseWriter, r *http.Request) {
	rules, err := h.store.ListRules()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	resp := alertStatusResponse{Items: []alertStatusItem{}}
	for _, rule := range rules {
		for _, st := range rule.States {
			if st.Status == "firing" {
				resp.FiringCount++
				if st.AcknowledgedAt != nil {
					resp.AcknowledgedCount++
				}
				since := ""
				if st.FiringSince != nil {
					since = st.FiringSince.Format(time.RFC3339)
				}
				resp.Items = append(resp.Items, alertStatusItem{
					RuleID:       rule.ID,
					RuleName:     rule.Name,
					Fingerprint:  st.Fingerprint,
					Hostname:     st.Hostname,
					Labels:       st.Labels,
					Status:       "firing",
					Acknowledged: st.AcknowledgedAt != nil,
					Since:        since,
				})
			}
		}
	}
	writeJSON(w, resp)
}

func writeBadRequest(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
