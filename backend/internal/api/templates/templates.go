// Package templates 提供订阅模板管理端点。
package templates

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/coxpanel/backend/internal/api/middleware"
	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
	"github.com/coxpanel/backend/internal/repo"
)

type Store interface {
	CreateTemplate(context.Context, string, string, string, json.RawMessage, int64) (int64, error)
	Copy(context.Context, int64, *int, string, string, int64) (int64, error)
	Get(context.Context, int64) (*repo.Template, error)
	GetPublished(context.Context, int64) (*repo.Template, error)
	List(context.Context, bool) ([]repo.Template, error)
	UpdateDraft(context.Context, int64, int64, string, string, string, json.RawMessage) (*repo.Template, error)
	ListVersions(context.Context, int64) ([]repo.TemplateVersion, error)
	Publish(context.Context, int64, int64, int64) (*repo.TemplateVersion, error)
	Archive(context.Context, int64, int64) error
}

type Handler struct {
	Templates Store
}

type templateRequest struct {
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Format           string          `json:"format"`
	Definition       json.RawMessage `json:"definition"`
	ExpectedRevision *int64          `json:"expectedRevision"`
	SourceTemplateID *int64          `json:"sourceTemplateId"`
	SourceVersion    *int            `json:"sourceVersion"`
}

type validationResponse struct {
	Valid        bool                         `json:"valid"`
	FieldErrors  []map[string]string          `json:"fieldErrors"`
	Warnings     []string                     `json:"warnings"`
	Capabilities generator.FormatCapabilities `json:"capabilities"`
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	page, err := middleware.ParsePage(r)
	if err != nil {
		middleware.Err(w, 422, "invalid_pagination", "分页参数无效")
		return
	}
	if h == nil || h.Templates == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "模板服务不可用")
		return
	}
	includeDrafts := r.URL.Query().Get("includeDrafts") == "true" && ownerFromRequest(r)
	items, err := h.Templates.List(r.Context(), includeDrafts)
	if err != nil {
		writeError(w, err)
		return
	}
	if items == nil {
		items = make([]repo.Template, 0)
	}
	selected := middleware.PageItems(items, page, func(item repo.Template) int64 { return item.ID }, false)
	result := middleware.PageResponse[map[string]any]{Items: []map[string]any{}, NextCursor: selected.NextCursor}
	for _, item := range selected.Items {
		result.Items = append(result.Items, templateView(item, ownerFromRequest(r)))
	}
	middleware.JSON(w, http.StatusOK, result)
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !requireOwner(w, r) {
		return
	}
	request, err := decodeTemplateRequest(w, r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	if strings.TrimSpace(request.Name) == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板名称必填")
		return
	}
	actor := actorID(r)
	var id int64
	if request.SourceTemplateID != nil {
		id, err = h.Templates.Copy(r.Context(), *request.SourceTemplateID, request.SourceVersion, request.Name, request.Description, actor)
	} else {
		if request.Format == "" {
			request.Format = "mihomo"
		}
		id, err = h.Templates.CreateTemplate(r.Context(), request.Name, request.Format, request.Description, request.Definition, actor)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	template, err := h.Templates.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	middleware.JSON(w, http.StatusCreated, templateView(*template, true))
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板 id 无效")
		return
	}
	var (
		template *repo.Template
		err      error
	)
	if ownerFromRequest(r) {
		template, err = h.Templates.Get(r.Context(), id)
	} else {
		template, err = h.Templates.GetPublished(r.Context(), id)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, templateView(*template, ownerFromRequest(r)))
}

func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !requireOwner(w, r) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板 id 无效")
		return
	}
	request, err := decodeTemplateRequest(w, r)
	if err != nil || request.ExpectedRevision == nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "expectedRevision 必填且请求体有效")
		return
	}
	if strings.TrimSpace(request.Name) == "" || request.Format == "" {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板名称和格式必填")
		return
	}
	template, err := h.Templates.UpdateDraft(r.Context(), id, *request.ExpectedRevision, request.Name, request.Description, request.Format, request.Definition)
	if err != nil {
		writeError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, templateView(*template, true))
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !requireOwner(w, r) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板 id 无效")
		return
	}
	request, err := decodeTemplateRequest(w, r)
	if err != nil || request.ExpectedRevision == nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "expectedRevision 必填且请求体有效")
		return
	}
	if err := h.Templates.Archive(r.Context(), id, *request.ExpectedRevision); err != nil {
		writeError(w, err)
		return
	}
	middleware.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) Versions(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板 id 无效")
		return
	}
	var err error
	if ownerFromRequest(r) {
		_, err = h.Templates.Get(r.Context(), id)
	} else {
		_, err = h.Templates.GetPublished(r.Context(), id)
	}
	if err != nil {
		writeError(w, err)
		return
	}
	versions, err := h.Templates.ListVersions(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	if versions == nil {
		versions = make([]repo.TemplateVersion, 0)
	}
	page, err := middleware.ParsePage(r)
	if err != nil {
		middleware.Err(w, 422, "invalid_pagination", "分页参数无效")
		return
	}
	middleware.JSON(w, http.StatusOK, middleware.PageItems(versions, page, func(item repo.TemplateVersion) int64 { return int64(item.Version) }, true))
}

func (h *Handler) Validate(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !requireOwner(w, r) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板 id 无效")
		return
	}
	template, err := h.Templates.Get(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	request, err := decodeTemplateRequest(w, r)
	if err != nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "请求体无效")
		return
	}
	definition := request.Definition
	if len(definition) == 0 || string(definition) == "null" {
		definition = template.Definition
	}
	response := validationResponse{FieldErrors: make([]map[string]string, 0), Warnings: make([]string, 0)}
	capability, supported := generator.Capabilities(template.Format)
	if supported {
		response.Capabilities = capability
	}
	if err := validateDefinitionAndRender(template.Format, definition); err != nil {
		response.FieldErrors = append(response.FieldErrors, map[string]string{"field": "definition", "reason": validationReason(err)})
	} else {
		response.Valid = true
	}
	middleware.JSON(w, http.StatusOK, response)
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	if !h.available(w) || !requireOwner(w, r) {
		return
	}
	id, ok := pathID(r, "id")
	if !ok {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "模板 id 无效")
		return
	}
	request, err := decodeTemplateRequest(w, r)
	if err != nil || request.ExpectedRevision == nil {
		middleware.Err(w, http.StatusBadRequest, "bad_request", "expectedRevision 必填且请求体有效")
		return
	}
	version, err := h.Templates.Publish(r.Context(), id, *request.ExpectedRevision, actorID(r))
	if err != nil {
		writeError(w, err)
		return
	}
	middleware.JSON(w, http.StatusCreated, version)
}

func (h *Handler) available(w http.ResponseWriter) bool {
	if h == nil || h.Templates == nil {
		middleware.Err(w, http.StatusServiceUnavailable, "unavailable", "模板服务不可用")
		return false
	}
	return true
}

func decodeTemplateRequest(w http.ResponseWriter, r *http.Request) (templateRequest, error) {
	var request templateRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 512*1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(&request)
	if err == nil {
		var trailing any
		if decoder.Decode(&trailing) != io.EOF {
			err = errors.New("one JSON value required")
		}
	}
	return request, err
}

func templateView(template repo.Template, includeDefinition bool) map[string]any {
	view := map[string]any{
		"id":               template.ID,
		"name":             template.Name,
		"description":      template.Description,
		"format":           template.Format,
		"status":           template.Status,
		"isBuiltin":        template.IsBuiltin,
		"publishedVersion": template.PublishedVersion,
		"revision":         template.Revision,
		"createdAt":        template.CreatedAt,
		"updatedAt":        template.UpdatedAt,
	}
	if includeDefinition {
		view["definition"] = template.Definition
	}
	if capability, ok := generator.Capabilities(template.Format); ok {
		view["capabilities"] = capability
	}
	return view
}

func validateDefinitionAndRender(format string, definition json.RawMessage) error {
	if err := generator.ValidateTemplateDefinition(format, definition); err != nil {
		return err
	}
	proxies := []generator.Proxy{}
	for index, protocol := range []string{"vless-reality", "shadowsocks", "hysteria2"} {
		id := int64(index + 1)
		proxies = append(proxies, generator.Proxy{Name: protocol, Node: &models.Node{ID: id, Type: "managed", PublicIP: "127.0.0.1"}, Inbound: &models.Inbound{ID: id, Role: "entry", Protocol: protocol, ListenPort: 8443, Config: json.RawMessage(`{"sni":"example.test","publicKey":"synthetic-public","shortId":"0123456789abcdef","method":"2022-blake3-aes-128-gcm","password":"QUFBQUFBQUFBQUFBQUFBQQ=="}`)}, Credential: "QkJCQkJCQkJCQkJCQkJCQg=="})
	}
	_, err := generator.GenerateSubscriptionPreview(format, proxies, "validation", definition)
	return err
}

func validationReason(err error) string {
	switch {
	case errors.Is(err, generator.ErrTemplateFieldForbidden), errors.Is(err, generator.ErrOverrideFieldForbidden):
		return "field_forbidden"
	case errors.Is(err, repo.ErrFormatUnsupported):
		return "format_unsupported"
	default:
		return "invalid_definition"
	}
}

func requireOwner(w http.ResponseWriter, r *http.Request) bool {
	if !ownerFromRequest(r) {
		middleware.Err(w, http.StatusForbidden, "forbidden", "需要 owner 权限")
		return false
	}
	return true
}

func ownerFromRequest(r *http.Request) bool {
	if user := middleware.UserFrom(r.Context()); user != nil {
		return user.Role == "owner"
	}
	claims := middleware.ClaimsFrom(r.Context())
	return claims != nil && claims.Role == "owner"
}

func actorID(r *http.Request) int64 {
	if claims := middleware.ClaimsFrom(r.Context()); claims != nil {
		return claims.UserID
	}
	if user := middleware.UserFrom(r.Context()); user != nil {
		return user.ID
	}
	return 0
}

func pathID(r *http.Request, name string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	return id, err == nil && id > 0
}

func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, repo.ErrRevisionConflict):
		middleware.Err(w, http.StatusConflict, "revision_conflict", "资源已被修改，请重新加载")
	case errors.Is(err, repo.ErrTemplateInUse):
		count := 1
		var references *repo.TemplateReferenceError
		if errors.As(err, &references) {
			count = references.Count
		}
		middleware.JSON(w, 409, map[string]any{"error": map[string]any{"code": "template_in_use", "message": "模板或分组仍被引用", "details": []map[string]any{{"referenceCount": count}}}})
	case errors.Is(err, repo.ErrTemplateArchived):
		middleware.Err(w, http.StatusConflict, "template_archived", "模板已归档")
	case errors.Is(err, repo.ErrTemplateImmutable):
		middleware.Err(w, http.StatusUnprocessableEntity, "template_immutable", "内置模板不可修改")
	case errors.Is(err, repo.ErrTemplateNotFound), errors.Is(err, repo.ErrTemplateVersionNotFound):
		middleware.Err(w, http.StatusNotFound, "not_found", "模板不存在")
	case errors.Is(err, repo.ErrTemplateNotPublished):
		middleware.Err(w, http.StatusUnprocessableEntity, "template_not_published", "模板尚未发布")
	case errors.Is(err, generator.ErrOverrideFieldForbidden):
		middleware.Err(w, 422, "override_field_forbidden", "默认值包含不允许的字段")
	case errors.Is(err, repo.ErrInvalidOverride), errors.Is(err, generator.ErrInvalidOverride), errors.Is(err, repo.ErrTemplateFormatMismatch), errors.Is(err, repo.ErrTemplateVersionWithoutTemplate), errors.Is(err, repo.ErrFormatUnsupported), errors.Is(err, generator.ErrTemplateInvalid), errors.Is(err, generator.ErrTemplateFieldForbidden):
		middleware.Err(w, http.StatusUnprocessableEntity, "template_invalid", "模板定义或格式无效")
	default:
		middleware.Err(w, http.StatusInternalServerError, "internal", "模板处理失败")
	}
}
