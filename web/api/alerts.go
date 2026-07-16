package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abrance/monitorbeat/web/alerts"
)

// alertHandler holds alert-related HTTP handlers.
type alertHandler struct {
	store *alerts.Store
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
	if req.Name == "" || req.Metric == "" || req.Condition == "" {
		writeBadRequest(w, "name, metric, condition required")
		return
	}
	if req.Condition != "gt" && req.Condition != "lt" {
		writeBadRequest(w, "condition must be 'gt' or 'lt'")
		return
	}
	// Basic PromQL safety — escape quotes
	req.Metric = strings.ReplaceAll(req.Metric, `\`, `\\`)
	req.Metric = strings.ReplaceAll(req.Metric, `"`, `\"`)

	rule := &alerts.AlertRule{
		Name:        req.Name,
		Enabled:     true,
		Metric:      req.Metric,
		Hostname:    req.Hostname,
		Condition:   req.Condition,
		Threshold:   req.Threshold,
		Duration:    req.Duration,
		Description: req.Description,
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
	if req.RuleID <= 0 {
		writeBadRequest(w, "rule_id required")
		return
	}
	var sil *time.Time
	if req.SilenceHours > 0 {
		t := time.Now().Add(time.Duration(req.SilenceHours) * time.Hour)
		sil = &t
	}
	if err := h.store.SetAcknowledged(req.RuleID, req.Hostname, sil); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"status": "acknowledged"})
}

func (h *alertHandler) listHistory(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ruleID, _ := strconv.ParseInt(q.Get("rule_id"), 10, 64)
	hostname := q.Get("hostname")
	state := q.Get("state")
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	items, total, err := h.store.ListHistory(ruleID, hostname, state, limit, offset)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"total": total, "items": items})
}

type alertStatusItem struct {
	RuleID       int64  `json:"rule_id"`
	RuleName     string `json:"rule_name"`
	Hostname     string `json:"hostname"`
	Status       string `json:"status"`
	Acknowledged bool   `json:"acknowledged"`
	Since        string `json:"since"`
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
	resp := alertStatusResponse{}
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
					Hostname:     st.Hostname,
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
