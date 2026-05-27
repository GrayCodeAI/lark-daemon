package api

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"lark-daemon/internal/proto"
)

func (r *Router) handleUploadFile(w http.ResponseWriter, req *http.Request) {
	member := requireWorkspaceAuth(w, req, chi.URLParam(req, "id"))
	if member == nil {
		return
	}
	workspaceID := chi.URLParam(req, "id")
	if !isUUID(workspaceID) {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return
	}
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart form")
		return
	}
	file, header, err := req.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field required")
		return
	}
	defer file.Close()
	// Validate file type
	mimeType := header.Header.Get("Content-Type")
	if mimeType == "" {
		buf := make([]byte, 512)
		n, _ := file.Read(buf)
		mimeType = http.DetectContentType(buf[:n])
		file.Seek(0, 0) // reset reader
	}
	allowedTypes := []string{
		"image/", "text/", "application/pdf", "application/json",
		"application/zip", "application/gzip", "application/x-tar",
	}
	allowed := false
	for _, t := range allowedTypes {
		if strings.HasPrefix(mimeType, t) {
			allowed = true
			break
		}
	}
	if !allowed {
		writeError(w, http.StatusBadRequest, "file type not allowed: "+mimeType)
		return
	}
	fID := proto.NewID()
	ext := filepath.Ext(header.Filename)
	storagePath := filepath.Join(workspaceID, fID+ext)
	if err := r.storage.Save(req.Context(), storagePath, file); err != nil {
		serverError(w, err, "failed to save file")
		return
	}
	// Detect MIME type from header or content — already detected above during validation
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	f := &proto.File{
		ID:          fID,
		WorkspaceID: workspaceID,
		UploaderID:  member.ID,
		Filename:    sanitizeFilename(header.Filename),
		MimeType:    mimeType,
		Size:        header.Size,
		Path:        storagePath,
	}
	if err := r.services.CreateFile(req.Context(), f); err != nil {
		r.storage.Delete(req.Context(), storagePath)
		serverError(w, err, "internal error")
		return
	}
	r.recordUsage(workspaceID, "files", 1)
	writeJSON(w, http.StatusCreated, f)
}

func (r *Router) handleGetFile(w http.ResponseWriter, req *http.Request) {
	caller := memberFromContext(req)
	if caller == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	f, err := r.services.GetFile(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if f == nil || f.WorkspaceID != caller.WorkspaceID {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (r *Router) handleListFiles(w http.ResponseWriter, req *http.Request) {
	if requireWorkspaceAuth(w, req, chi.URLParam(req, "id")) == nil {
		return
	}
	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(req.URL.Query().Get("offset"))
	files, err := r.services.ListFiles(req.Context(), chi.URLParam(req, "id"), limit, offset)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, files)
}

func (r *Router) handleDeleteFile(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	fileID := chi.URLParam(req, "id")
	f, err := r.services.GetFile(req.Context(), fileID)
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if f == nil || f.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	if err := r.storage.Delete(req.Context(), f.Path); err != nil && !os.IsNotExist(err) {
		serverError(w, err, "failed to delete file from storage")
		return
	}
	if err := r.services.DeleteFile(req.Context(), fileID); err != nil {
		serverError(w, err, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (r *Router) handleDownloadFile(w http.ResponseWriter, req *http.Request) {
	member := memberFromContext(req)
	if member == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	f, err := r.services.GetFile(req.Context(), chi.URLParam(req, "id"))
	if err != nil {
		serverError(w, err, "internal error")
		return
	}
	if f == nil || f.WorkspaceID != member.WorkspaceID {
		writeError(w, http.StatusNotFound, "file not found")
		return
	}
	reader, err := r.storage.Open(req.Context(), f.Path)
	if err != nil {
		serverError(w, err, "failed to open file")
		return
	}
	defer reader.Close()
	disposition := "attachment"
	if strings.HasPrefix(f.MimeType, "image/") {
		disposition = "inline"
	}
	w.Header().Set("Content-Disposition", disposition+"; filename=\""+sanitizeFilename(f.Filename)+"\"")
	w.Header().Set("Content-Type", f.MimeType)
	io.Copy(w, reader)
}
