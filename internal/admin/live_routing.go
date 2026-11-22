package admin

import (
	"net/http"
	"slices"

	"github.com/Sanjeever/model-confluence/internal/gateway"
	"github.com/Sanjeever/model-confluence/internal/httpx"
)

func (h *Handler) liveRouting(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	source, ok := h.tester.(interface {
		LiveRouting() gateway.LiveRoutingSnapshot
	})
	if !ok {
		httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "实时路由不可用"})
		return
	}
	snapshot := source.LiveRouting()
	if len(snapshot.Recent) != 0 {
		ids := make([]string, 0, len(snapshot.Recent))
		for _, request := range snapshot.Recent {
			ids = append(ids, request.ID)
		}
		existing, err := h.store.ExistingRequestIDs(r.Context(), ids)
		if err != nil {
			writeStoreError(w, err, "读取最近结束的请求失败")
			return
		}
		snapshot.Recent = slices.DeleteFunc(snapshot.Recent, func(request gateway.LiveRequest) bool {
			return !existing[request.ID]
		})
	}
	httpx.WriteJSON(w, http.StatusOK, snapshot)
}

func (h *Handler) liveRoutingCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	nodes, err := h.store.LiveRoutingCatalog(r.Context())
	if err != nil {
		writeStoreError(w, err, "读取实时路由节点失败")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, nodes)
}
