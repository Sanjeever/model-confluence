package admin

import (
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/store"
	"net/http"
)

func (h *Handler) SetLogRetentionDays(days int) { h.retentionDays = days }

func (h *Handler) storageStatus(w http.ResponseWriter, r *http.Request) {
	stats, err := h.store.StorageStats()
	if err != nil {
		writeStoreError(w, err, "读取存储状态失败")
		return
	}
	httpx.WriteJSON(w, 200, struct {
		store.StorageStats
		RetentionDays int `json:"retention_days"`
	}{stats, h.retentionDays})
}

func (h *Handler) cleanupLogs(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("created_from") == "" || r.URL.Query().Get("created_to") == "" {
		httpx.WriteJSON(w, 400, map[string]string{"error": "请明确提供清理时间范围"})
		return
	}
	from, to, err := requestTimeRange(r)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "prune" && mode != "delete" {
		httpx.WriteJSON(w, 400, map[string]string{"error": "清理方式必须为 prune 或 delete"})
		return
	}
	if r.Method == http.MethodGet {
		count, err := h.store.PreviewCleanup(r.Context(), from, to, mode)
		if err != nil {
			writeStoreError(w, err, "预览清理失败")
			return
		}
		httpx.WriteJSON(w, 200, map[string]int64{"count": count})
		return
	}
	result, err := h.store.CleanupLogs(r.Context(), from, to, mode)
	if err != nil {
		writeStoreError(w, err, "清理失败")
		return
	}
	httpx.WriteJSON(w, 200, result)
}
