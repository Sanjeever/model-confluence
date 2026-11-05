package admin

import (
	"context"
	"github.com/Sanjeever/model-confluence/internal/gateway"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"net/http"
)

func (h *Handler) replay(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	var input gateway.ReplayInput
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	tester, ok := h.tester.(interface {
		ReplayRequest(context.Context, string, gateway.ReplayInput) (gateway.ReplayResult, error)
	})
	if !ok {
		httpx.WriteJSON(w, 503, map[string]string{"error": "调试重放不可用"})
		return
	}
	result, err := tester.ReplayRequest(r.Context(), r.PathValue("id"), input)
	if err != nil {
		writeStoreError(w, err, "请求不存在或无法重放")
		return
	}
	httpx.WriteJSON(w, 200, result)
}
