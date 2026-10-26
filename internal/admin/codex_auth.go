package admin

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Sanjeever/model-confluence/internal/codex"
	"github.com/Sanjeever/model-confluence/internal/gateway"
	"github.com/Sanjeever/model-confluence/internal/httpx"
	"github.com/Sanjeever/model-confluence/internal/store"
)

const maxAuthFileBytes = 1 << 20

func (h *Handler) uploadAuthFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "无效的供应商 ID"})
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthFileBytes)
	if err := r.ParseMultipartForm(maxAuthFileBytes); err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "上传文件无效或超过 1 MiB"})
		return
	}
	defer r.MultipartForm.RemoveAll()
	if len(r.MultipartForm.File) != 1 || len(r.MultipartForm.File["file"]) != 1 || len(r.MultipartForm.Value) != 0 {
		httpx.WriteJSON(w, 400, map[string]string{"error": "每次上传一个 file 文件"})
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "缺少 file 文件"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "读取上传文件失败"})
		return
	}
	provider, err := h.store.ProviderByID(id)
	if err != nil {
		writeStoreError(w, err, "供应商不存在")
		return
	}
	if provider.Kind == store.ProviderAntigravityOAuth {
		importer, ok := h.tester.(interface {
			ImportAntigravityAccounts(context.Context, int64, string, []byte) (gateway.AntigravityImportResult, error)
		})
		if !ok {
			httpx.WriteJSON(w, 503, map[string]string{"error": "Antigravity 账号导入不可用"})
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
		defer cancel()
		result, err := importer.ImportAntigravityAccounts(ctx, id, header.Filename, data)
		if err != nil {
			httpx.WriteJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		httpx.WriteJSON(w, http.StatusOK, result)
		return
	}
	if _, err := codex.ParseAuth(data); err != nil {
		httpx.WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	key, err := h.store.ImportOAuthCredential(id, header.Filename, data)
	if errors.Is(err, store.ErrDuplicateOAuthAccount) {
		httpx.WriteJSON(w, 409, map[string]string{"error": err.Error()})
		return
	}
	if err != nil {
		writeStoreError(w, err, "供应商不存在")
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, key)
}

func (h *Handler) deleteAuthFile(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "无效的供应商 ID"})
		return
	}
	keyID, err := strconv.ParseInt(r.PathValue("key_id"), 10, 64)
	if err != nil {
		httpx.WriteJSON(w, 400, map[string]string{"error": "无效的账号 ID"})
		return
	}
	if err := h.store.DeleteOAuthCredential(id, keyID); err != nil {
		writeStoreError(w, err, "账号不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
