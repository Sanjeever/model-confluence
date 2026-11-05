package admin

import (
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/protocol"
	"github.com/Sanjeever/model-confluence/internal/store"
	"net/http"
)

func (h *Handler) explainRoutes(w http.ResponseWriter, r *http.Request) {
	var input store.RoutingRequirements
	if !httpx.DecodeJSON(w, r, &input) {
		return
	}
	if input.VirtualModel == "" || (input.Protocol != protocol.Chat && input.Protocol != protocol.Responses && input.Protocol != protocol.Messages) {
		httpx.WriteJSON(w, 400, map[string]string{"error": "请提供虚拟模型与有效入站协议"})
		return
	}
	tester, ok := h.tester.(interface {
		ExplainRoutes(store.RoutingRequirements) ([]store.RouteDecision, error)
	})
	if !ok {
		httpx.WriteJSON(w, 503, map[string]string{"error": "路由诊断不可用"})
		return
	}
	result, err := tester.ExplainRoutes(input)
	if err != nil {
		writeStoreError(w, err, "虚拟模型不存在")
		return
	}
	httpx.WriteJSON(w, 200, result)
}
