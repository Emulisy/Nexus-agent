// Knowledge-base endpoints cover ownership-scoped metadata, document upload,
// chunk inspection, and retrieval results. Uploads are size-bounded while reading;
// the service coordinates object storage and asynchronous document ingestion.
package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/chengpeng-cp/nexus-agent/internal/model"
	"github.com/chengpeng-cp/nexus-agent/internal/pkg/errcode"
	"github.com/chengpeng-cp/nexus-agent/internal/service"
)

// ListKBs returns the current user's knowledge bases with pagination metadata.
func (h *Handler) ListKBs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, total, err := h.svcs.KB.ListKBs(ctx, userID(r), parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// GetKB returns a knowledge base through the service's ownership-filtered lookup.
func (h *Handler) GetKB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	kb, err := h.svcs.KB.GetKB(ctx, userID(r), id)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, kb)
}

// CreateKB binds metadata and delegates owner assignment and vector-collection creation.
func (h *Handler) CreateKB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	kb := &model.KnowledgeBase{}
	if err := decodeJSON(w, r, kb); err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.KB.CreateKB(ctx, userID(r), kb); err != nil {
		Fail(w, err)
		return
	}
	OK(w, kb)
}

// DeleteKB delegates document and vector cleanup after ownership validation.
func (h *Handler) DeleteKB(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.KB.DeleteKB(ctx, userID(r), id); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// UploadDoc reads the multipart file using a MaxDocSize+1 limit.
// The extra byte lets the service distinguish oversized content from a file
// exactly at the limit without loading an unbounded upload into memory.
func (h *Handler) UploadDoc(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, service.MaxDocSize+(1<<20))
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	f, fh, err := r.FormFile("file")
	if err != nil {
		var limitError *http.MaxBytesError
		if errors.As(err, &limitError) {
			Fail(w, errcode.ErrFileTooLarge.WithCause(err))
		} else {
			Fail(w, errcode.ErrInvalidParam.WithMsg("A multipart file upload is required").WithCause(err))
		}
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, service.MaxDocSize+1))
	if err != nil {
		Fail(w, err)
		return
	}
	doc, err := h.svcs.KB.UploadDoc(ctx, userID(r), id, fh.Filename, data)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, doc)
}

// ListDocs lists documents only after the service verifies knowledge-base ownership.
func (h *Handler) ListDocs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	list, total, err := h.svcs.KB.ListDocs(ctx, userID(r), id, parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// DeleteDoc supplies both route IDs so the service can verify the document belongs to that KB.
func (h *Handler) DeleteDoc(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	docID, err := pathIDNamed(r, "docID")
	if err != nil {
		Fail(w, err)
		return
	}
	if err := h.svcs.KB.DeleteDoc(ctx, userID(r), id, docID); err != nil {
		Fail(w, err)
		return
	}
	OK(w, nil)
}

// ListChunks resolves document ownership through its knowledge base before listing chunks.
func (h *Handler) ListChunks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	docID, err := pathIDNamed(r, "docID")
	if err != nil {
		Fail(w, err)
		return
	}
	list, total, err := h.svcs.KB.ListChunks(ctx, userID(r), docID, parsePagination(r))
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, map[string]any{"list": list, "total": total})
}

// TestRetrieve returns references without generating a model answer.
// The query is bound from the request object; both GET and POST routes currently
// point to this handler.
func (h *Handler) TestRetrieve(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id, err := pathID(r)
	if err != nil {
		Fail(w, err)
		return
	}
	var req struct {
		Query string `json:"query"`
	}
	if r.Method == http.MethodGet {
		req.Query = r.URL.Query().Get("query")
	} else if err := decodeJSON(w, r, &req); err != nil {
		Fail(w, err)
		return
	}
	refs, err := h.svcs.KB.TestRetrieve(ctx, userID(r), id, req.Query)
	if err != nil {
		Fail(w, err)
		return
	}
	OK(w, refs)
}

// pathIDNamed parses a nonstandard ID parameter such as docID with the shared int64 parser.
func pathIDNamed(r *http.Request, name string) (int64, error) {
	return parseID(r.PathValue(name))
}
