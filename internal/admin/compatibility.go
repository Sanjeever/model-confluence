package admin

import (
	"context"
	"github.com/Sanjeever/model-confluence/internal/gateway"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"net/http"
	"strconv"
)

func (h *Handler) compatibility(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteJSON(w, 400, map[string]string{"error": "无效模型 ID"})
		return
	}
	model, err := h.store.VirtualModelName(id)
	if err != nil {
		writeStoreError(w, err, "模型不存在")
		return
	}
	if r.Method == http.MethodGet {
		records, err := h.store.CompatibilityHistory(id)
		if err != nil {
			writeStoreError(w, err, "读取测试记录失败")
			return
		}
		httpx.WriteJSON(w, 200, records)
		return
	}
	var input gateway.CompatibilityInput
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	tester, ok := h.tester.(interface {
		CheckCompatibility(context.Context, string, gateway.CompatibilityInput) gateway.CompatibilityResult
	})
	if !ok {
		httpx.WriteJSON(w, 503, map[string]string{"error": "兼容性测试不可用"})
		return
	}
	result := tester.CheckCompatibility(r.Context(), model, input)
	if err := h.store.SaveCompatibilityCheck(id, result); err != nil {
		writeStoreError(w, err, "保存测试记录失败")
		return
	}
	httpx.WriteJSON(w, 200, result)
}
