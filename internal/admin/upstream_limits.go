package admin

import (
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"net/http"
	"strconv"
)

func (h *Handler) upstreamLimits(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		httpx.WriteJSON(w, 400, map[string]string{"error": "无效账号 ID"})
		return
	}
	result, err := h.store.UpstreamLimits(id)
	if err != nil {
		writeStoreError(w, err, "读取额度信息失败")
		return
	}
	httpx.WriteJSON(w, 200, result)
}
