package repo

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/coxpanel/backend/internal/generator"
	"github.com/coxpanel/backend/internal/models"
)

var (
	ErrRevisionConflict               = errors.New("revision conflict")
	ErrFormatUnsupported              = errors.New("format unsupported")
	ErrTemplateVersionWithoutTemplate = errors.New("template version requires a template")
	ErrTemplateNotFound               = errors.New("template not found")
	ErrTemplateVersionNotFound        = errors.New("template version not found")
	ErrTemplateFormatMismatch         = errors.New("template format mismatch")
	ErrTemplateNotPublished           = errors.New("template is not published")
	ErrTemplateArchived               = errors.New("template is archived")
	ErrTemplateInUse                  = errors.New("template is in use")
	ErrTemplateImmutable              = errors.New("builtin template is immutable")
	ErrOverrideFieldForbidden         = errors.New("override field forbidden")
	ErrInvalidOverride                = errors.New("invalid override")
)

// ValidateSubscriptionSelection validates the format/template relationship
// before a subscription is created or changed. A nil template means the
// immutable built-in renderer, and therefore must not carry a version.
func ValidateSubscriptionSelection(format string, templateID *int64, templateVersion *int) error {
	switch format {
	case "mihomo", "sing-box", "base64":
	default:
		return fmt.Errorf("%w: %s", ErrFormatUnsupported, format)
	}
	if templateID == nil && templateVersion != nil {
		return ErrTemplateVersionWithoutTemplate
	}
	if templateID != nil && *templateID <= 0 {
		return fmt.Errorf("%w: invalid template id", ErrTemplateNotFound)
	}
	if templateVersion != nil && *templateVersion <= 0 {
		return fmt.Errorf("%w: invalid template version", ErrTemplateVersionNotFound)
	}
	return nil
}

// OverridePatch is a full replacement for one override row. A nil scalar is
// an inherited value; an explicit pointer to zero remains an explicit value.
// Params is a JSON object containing only safe client-side fields.
type OverridePatch struct {
	DisplayName *string         `json:"displayName"`
	SortOrder   *int            `json:"sortOrder"`
	Icon        *string         `json:"icon"`
	Params      json.RawMessage `json:"params"`
	ProxyGroup  *string         `json:"proxyGroup"`
}

func (p OverridePatch) SortOrderValue() *int {
	return p.SortOrder
}

// ValidateSubscriptionOverride validates the common, protocol-independent
// part of an override. Protocol-specific capability checks belong to the
// generator registry before this repository method is called.
func ValidateSubscriptionOverride(p OverridePatch) error {
	if err := generator.ValidateOverrideMetadata(p.DisplayName, p.SortOrder, p.Icon, p.ProxyGroup); err != nil {
		return err
	}
	for field, value := range map[string]*string{
		"displayName": p.DisplayName,
		"icon":        p.Icon,
		"proxyGroup":  p.ProxyGroup,
	} {
		if value != nil && (len([]byte(*value)) > 128 || strings.ContainsAny(*value, "\r\n\x00")) {
			return fmt.Errorf("%w: %s", ErrInvalidOverride, field)
		}
	}
	if p.Params == nil || string(p.Params) == "" || string(p.Params) == "null" {
		return nil
	}
	var params map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(p.Params)))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil || params == nil {
		return fmt.Errorf("%w: params must be an object", ErrInvalidOverride)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%w: params must contain one JSON value", ErrInvalidOverride)
	}
	for key, value := range params {
		if err := validateOverrideField(key, value); err != nil {
			return err
		}
	}
	return nil
}

func validateOverrideField(key string, value any) error {
	if err := generator.ValidateOverrideParams("mihomo", "", map[string]any{key: value}); err != nil {
		if errors.Is(err, generator.ErrOverrideFieldForbidden) {
			return ErrOverrideFieldForbidden
		}
		return ErrInvalidOverride
	}
	return nil
}

// Template is the editable template definition and its publication state.
// It contains no node connection material or user credentials.
type Template struct {
	ID               int64           `json:"id"`
	Name             string          `json:"name"`
	Description      string          `json:"description"`
	Format           string          `json:"format"`
	Definition       json.RawMessage `json:"definition"`
	Status           string          `json:"status"`
	IsBuiltin        bool            `json:"isBuiltin"`
	PublishedVersion *int            `json:"publishedVersion,omitempty"`
	Revision         int64           `json:"revision"`
	CreatedBy        *int64          `json:"createdBy,omitempty"`
	CreatedAt        time.Time       `json:"createdAt"`
	UpdatedAt        time.Time       `json:"updatedAt"`
	ArchivedAt       *time.Time      `json:"archivedAt,omitempty"`
}

// TemplateVersion is immutable after publication.
type TemplateVersion struct {
	Format        string          `json:"format,omitempty"`
	TemplateID    int64           `json:"templateId"`
	Version       int             `json:"version"`
	SchemaVersion int             `json:"schemaVersion"`
	Definition    json.RawMessage `json:"definition"`
	Checksum      string          `json:"checksum"`
	PublishedBy   *int64          `json:"publishedBy,omitempty"`
	PublishedAt   time.Time       `json:"publishedAt"`
}

// TemplateRepo persists editable templates and immutable published versions.
type TemplateRepo struct{ db *sql.DB }

func NewTemplateRepo(db *sql.DB) *TemplateRepo { return &TemplateRepo{db: db} }

func (r *TemplateRepo) Create(ctx context.Context, template *Template) (int64, error) {
	if r == nil || r.db == nil || template == nil || strings.TrimSpace(template.Name) == "" {
		return 0, fmt.Errorf("%w: invalid template", ErrInvalidOverride)
	}
	if len([]byte(template.Name)) > 128 || strings.ContainsAny(template.Name, "\r\n\x00") || len([]byte(template.Description)) > 1024 || strings.ContainsAny(template.Description, "\r\n\x00") {
		return 0, fmt.Errorf("%w: invalid template metadata", ErrInvalidOverride)
	}
	if err := ValidateSubscriptionSelection(template.Format, nil, nil); err != nil {
		return 0, err
	}
	definition := template.Definition
	if len(definition) == 0 || string(definition) == "null" {
		definition = json.RawMessage(`{"schemaVersion":1}`)
	}
	if len(definition) > 256*1024 {
		return 0, fmt.Errorf("%w: definition too large", ErrInvalidOverride)
	}
	if err := generator.ValidateTemplateDefinition(template.Format, definition); err != nil {
		return 0, err
	}
	var id int64
	err := r.db.QueryRowContext(ctx, `
		INSERT INTO templates (name, description, format, definition, status, is_builtin, revision, created_by)
		VALUES ($1,$2,$3,$4,'draft',FALSE,1,$5) RETURNING id`,
		template.Name, template.Description, template.Format, definition, nullableID(template.CreatedBy)).Scan(&id)
	return id, err
}

func (r *TemplateRepo) CreateTemplate(ctx context.Context, name, format, description string, definition json.RawMessage, createdBy int64) (int64, error) {
	var actor *int64
	if createdBy > 0 {
		actor = &createdBy
	}
	return r.Create(ctx, &Template{Name: name, Format: format, Description: description, Definition: definition, CreatedBy: actor})
}

func (r *TemplateRepo) Get(ctx context.Context, id int64) (*Template, error) {
	return r.get(ctx, `WHERE id=$1`, id)
}

func (r *TemplateRepo) GetPublished(ctx context.Context, id int64) (*Template, error) {
	return r.get(ctx, `WHERE id=$1 AND archived_at IS NULL AND status='published' AND published_version IS NOT NULL`, id)
}

func (r *TemplateRepo) List(ctx context.Context, includeDrafts bool) ([]Template, error) {
	where := "WHERE archived_at IS NULL AND status='published' AND published_version IS NOT NULL"
	if includeDrafts {
		where = "WHERE archived_at IS NULL"
	}
	rows, err := r.db.QueryContext(ctx, `
		SELECT id,name,description,format,definition,status,is_builtin,published_version,revision,created_by,created_at,updated_at,archived_at
		FROM templates `+where+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Template, 0)
	for rows.Next() {
		template, err := scanTemplate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *template)
	}
	return out, rows.Err()
}

func (r *TemplateRepo) Update(ctx context.Context, id, expectedRevision int64, template *Template) (*Template, error) {
	if template == nil {
		return nil, fmt.Errorf("%w: template is nil", ErrInvalidOverride)
	}
	if err := ValidateSubscriptionSelection(template.Format, nil, nil); err != nil {
		return nil, err
	}
	if len(template.Definition) > 256*1024 {
		return nil, fmt.Errorf("%w: definition too large", ErrInvalidOverride)
	}
	if len([]byte(template.Name)) == 0 || len([]byte(template.Name)) > 128 || strings.ContainsAny(template.Name, "\r\n\x00") || len([]byte(template.Description)) > 1024 || strings.ContainsAny(template.Description, "\r\n\x00") {
		return nil, fmt.Errorf("%w: invalid template metadata", ErrInvalidOverride)
	}
	definition := template.Definition
	if len(definition) == 0 || string(definition) == "null" {
		definition = json.RawMessage(`{"schemaVersion":1}`)
	}
	if err := generator.ValidateTemplateDefinition(template.Format, definition); err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return nil, err
	}
	var revision int64
	var builtin bool
	var archivedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT revision,is_builtin,archived_at FROM templates WHERE id=$1 FOR UPDATE`, id).Scan(&revision, &builtin, &archivedAt); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotFound
	} else if err != nil {
		return nil, err
	}
	if revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if builtin {
		return nil, ErrTemplateImmutable
	}
	if archivedAt.Valid {
		return nil, ErrTemplateArchived
	}
	var immutableFormat string
	var hasVersions bool
	if err := tx.QueryRowContext(ctx, `SELECT format,EXISTS(SELECT 1 FROM template_versions WHERE template_id=$1) FROM templates WHERE id=$1`, id).Scan(&immutableFormat, &hasVersions); err != nil {
		return nil, err
	}
	if hasVersions && immutableFormat != template.Format {
		return nil, ErrTemplateImmutable
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE templates SET name=$2,description=$3,format=$4,definition=$5,revision=revision+1,updated_at=now()
		WHERE id=$1 AND revision=$6`, id, template.Name, template.Description, template.Format, definition, expectedRevision)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.Get(ctx, id)
}

func (r *TemplateRepo) UpdateDraft(ctx context.Context, id, expectedRevision int64, name, description, format string, definition json.RawMessage) (*Template, error) {
	return r.Update(ctx, id, expectedRevision, &Template{Name: name, Description: description, Format: format, Definition: definition})
}

func (r *TemplateRepo) ListVersions(ctx context.Context, templateID int64) ([]TemplateVersion, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT template_id,version,schema_version,definition,checksum,published_by,published_at
		FROM template_versions WHERE template_id=$1 ORDER BY version DESC`, templateID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TemplateVersion, 0)
	for rows.Next() {
		version, err := scanTemplateVersion(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *version)
	}
	return out, rows.Err()
}

func (r *TemplateRepo) GetVersion(ctx context.Context, templateID int64, version int) (*TemplateVersion, error) {
	var item TemplateVersion
	var definition []byte
	var publishedBy sql.NullInt64
	err := r.db.QueryRowContext(ctx, `
		SELECT template_id,version,schema_version,definition,checksum,published_by,published_at
		FROM template_versions WHERE template_id=$1 AND version=$2`, templateID, version).
		Scan(&item.TemplateID, &item.Version, &item.SchemaVersion, &definition, &item.Checksum, &publishedBy, &item.PublishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateVersionNotFound
	}
	if err != nil {
		return nil, err
	}
	item.Definition = json.RawMessage(definition)
	if publishedBy.Valid {
		item.PublishedBy = &publishedBy.Int64
	}
	return &item, nil
}

func (r *TemplateRepo) Publish(ctx context.Context, id, expectedRevision, publishedBy int64) (*TemplateVersion, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return nil, err
	}
	var template Template
	var definition []byte
	var publishedVersion sql.NullInt64
	var createdBy sql.NullInt64
	var archivedAt sql.NullTime
	err = tx.QueryRowContext(ctx, `
		SELECT id,name,description,format,definition,status,is_builtin,published_version,revision,created_by,created_at,updated_at,archived_at
		FROM templates WHERE id=$1 FOR UPDATE`, id).
		Scan(&template.ID, &template.Name, &template.Description, &template.Format, &definition, &template.Status, &template.IsBuiltin, &publishedVersion, &template.Revision, &createdBy, &template.CreatedAt, &template.UpdatedAt, &archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	if template.Revision != expectedRevision {
		return nil, ErrRevisionConflict
	}
	if template.IsBuiltin {
		return nil, ErrTemplateImmutable
	}
	if archivedAt.Valid {
		return nil, ErrTemplateArchived
	}
	if err := ValidateSubscriptionSelection(template.Format, nil, nil); err != nil {
		return nil, err
	}
	if err := generator.ValidateTemplateDefinition(template.Format, definition); err != nil {
		return nil, err
	}
	bound, err := tx.QueryContext(ctx, `SELECT id FROM subscriptions WHERE template_id=$1 AND template_version IS NULL ORDER BY id`, id)
	if err != nil {
		return nil, err
	}
	var subscriptionIDs []int64
	for bound.Next() {
		var subID int64
		if err := bound.Scan(&subID); err != nil {
			bound.Close()
			return nil, err
		}
		subscriptionIDs = append(subscriptionIDs, subID)
	}
	err = bound.Err()
	bound.Close()
	if err != nil {
		return nil, err
	}
	conflicts := 0
	for _, subID := range subscriptionIDs {
		if err := validateBoundOverridesTx(ctx, tx, subID, template.Format, definition); err != nil {
			if errors.Is(err, ErrTemplateInUse) {
				conflicts++
			} else {
				return nil, err
			}
		}
	}
	if conflicts > 0 {
		return nil, &TemplateReferenceError{Count: conflicts}
	}
	if len(definition) == 0 {
		definition = []byte(`{}`)
	}
	var nextVersion int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0)+1 FROM template_versions WHERE template_id=$1`, id).Scan(&nextVersion); err != nil {
		return nil, err
	}
	checksumBytes := sha256.Sum256(definition)
	checksum := hex.EncodeToString(checksumBytes[:])
	var version TemplateVersion
	var publishedByValue any
	if publishedBy > 0 {
		publishedByValue = publishedBy
	}
	var storedDefinition []byte
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO template_versions (template_id,version,schema_version,definition,checksum,published_by)
		VALUES ($1,$2,1,$3,$4,$5)
		RETURNING template_id,version,schema_version,definition,checksum,published_by,published_at`,
		id, nextVersion, definition, checksum, publishedByValue).
		Scan(&version.TemplateID, &version.Version, &version.SchemaVersion, &storedDefinition, &version.Checksum, &publishedByValue, &version.PublishedAt); err != nil {
		return nil, err
	}
	version.Definition = json.RawMessage(storedDefinition)
	if publishedBy > 0 {
		version.PublishedBy = &publishedBy
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE templates SET status='published',published_version=$2,revision=revision+1,updated_at=now()
		WHERE id=$1 AND revision=$3`, id, nextVersion, expectedRevision)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &version, nil
}

func (r *TemplateRepo) PublishVersion(ctx context.Context, id, expectedRevision, publishedBy int64) (*TemplateVersion, error) {
	return r.Publish(ctx, id, expectedRevision, publishedBy)
}

func (r *TemplateRepo) Archive(ctx context.Context, id, expectedRevision int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return err
	}
	var revision int64
	var builtin bool
	var archivedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT revision,is_builtin,archived_at FROM templates WHERE id=$1 FOR UPDATE`, id).Scan(&revision, &builtin, &archivedAt); errors.Is(err, sql.ErrNoRows) {
		return ErrTemplateNotFound
	} else if err != nil {
		return err
	}
	if revision != expectedRevision {
		return ErrRevisionConflict
	}
	if builtin {
		return ErrTemplateImmutable
	}
	if archivedAt.Valid {
		return nil
	}
	var inUse bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM subscriptions WHERE template_id=$1)`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse {
		return ErrTemplateInUse
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE templates SET status='archived',archived_at=now(),revision=revision+1,updated_at=now()
		WHERE id=$1 AND revision=$2`, id, expectedRevision)
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return ErrRevisionConflict
	}
	return tx.Commit()
}

func (r *TemplateRepo) Delete(ctx context.Context, id, expectedRevision int64) error {
	return r.Archive(ctx, id, expectedRevision)
}

func (r *TemplateRepo) Copy(ctx context.Context, sourceTemplateID int64, sourceVersion *int, name, description string, createdBy int64) (int64, error) {
	var definition []byte
	var format string
	if sourceVersion == nil {
		err := r.db.QueryRowContext(ctx, `SELECT format,definition FROM templates WHERE id=$1 AND archived_at IS NULL`, sourceTemplateID).Scan(&format, &definition)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrTemplateNotFound
		}
		if err != nil {
			return 0, err
		}
	} else {
		err := r.db.QueryRowContext(ctx, `
			SELECT t.format,v.definition FROM templates t JOIN template_versions v ON v.template_id=t.id
			WHERE t.id=$1 AND v.version=$2 AND t.archived_at IS NULL`, sourceTemplateID, *sourceVersion).
			Scan(&format, &definition)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrTemplateVersionNotFound
		}
		if err != nil {
			return 0, err
		}
	}
	return r.Create(ctx, &Template{Name: name, Description: description, Format: format, Definition: json.RawMessage(definition), CreatedBy: func() *int64 {
		if createdBy <= 0 {
			return nil
		}
		return &createdBy
	}()})
}

func (r *TemplateRepo) CopyVersion(ctx context.Context, sourceTemplateID int64, sourceVersion int, name, description string, createdBy int64) (int64, error) {
	return r.Copy(ctx, sourceTemplateID, &sourceVersion, name, description, createdBy)
}

// ResolveVersion returns the explicitly selected immutable version, or the
// latest published version when requestedVersion is nil. A nil template ID
// represents the historical built-in renderer and returns (nil, nil).
func (r *TemplateRepo) ResolveVersion(ctx context.Context, templateID *int64, requestedVersion *int) (*TemplateVersion, error) {
	if templateID == nil {
		return nil, nil
	}
	if *templateID <= 0 {
		return nil, ErrTemplateNotFound
	}
	if requestedVersion != nil {
		item, err := r.GetVersion(ctx, *templateID, *requestedVersion)
		if err != nil {
			return nil, err
		}
		err = r.db.QueryRowContext(ctx, `SELECT format FROM templates WHERE id=$1`, *templateID).Scan(&item.Format)
		return item, err
	}
	var version int
	if err := r.db.QueryRowContext(ctx, `
		SELECT published_version FROM templates
		WHERE id=$1 AND archived_at IS NULL AND published_version IS NOT NULL`, *templateID).Scan(&version); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotPublished
	} else if err != nil {
		return nil, err
	}
	item, err := r.GetVersion(ctx, *templateID, version)
	if err != nil {
		return nil, err
	}
	err = r.db.QueryRowContext(ctx, `SELECT format FROM templates WHERE id=$1`, *templateID).Scan(&item.Format)
	return item, err
}

func (r *TemplateRepo) get(ctx context.Context, predicate string, args ...any) (*Template, error) {
	var template Template
	var definition []byte
	var publishedVersion, createdBy sql.NullInt64
	var archivedAt sql.NullTime
	err := r.db.QueryRowContext(ctx, `
		SELECT id,name,description,format,definition,status,is_builtin,published_version,revision,created_by,created_at,updated_at,archived_at
		FROM templates `+predicate, args...).
		Scan(&template.ID, &template.Name, &template.Description, &template.Format, &definition, &template.Status, &template.IsBuiltin, &publishedVersion, &template.Revision, &createdBy, &template.CreatedAt, &template.UpdatedAt, &archivedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTemplateNotFound
	}
	if err != nil {
		return nil, err
	}
	template.Definition = json.RawMessage(definition)
	if publishedVersion.Valid {
		version := int(publishedVersion.Int64)
		template.PublishedVersion = &version
	}
	if createdBy.Valid {
		template.CreatedBy = &createdBy.Int64
	}
	if archivedAt.Valid {
		template.ArchivedAt = &archivedAt.Time
	}
	if template.Revision <= 0 {
		template.Revision = 1
	}
	return &template, nil
}

func scanTemplate(scanner interface{ Scan(...any) error }) (*Template, error) {
	var template Template
	var definition []byte
	var publishedVersion, createdBy sql.NullInt64
	var archivedAt sql.NullTime
	if err := scanner.Scan(&template.ID, &template.Name, &template.Description, &template.Format, &definition, &template.Status, &template.IsBuiltin, &publishedVersion, &template.Revision, &createdBy, &template.CreatedAt, &template.UpdatedAt, &archivedAt); err != nil {
		return nil, err
	}
	template.Definition = json.RawMessage(definition)
	if publishedVersion.Valid {
		version := int(publishedVersion.Int64)
		template.PublishedVersion = &version
	}
	if createdBy.Valid {
		template.CreatedBy = &createdBy.Int64
	}
	if archivedAt.Valid {
		template.ArchivedAt = &archivedAt.Time
	}
	if template.Revision <= 0 {
		template.Revision = 1
	}
	return &template, nil
}

func scanTemplateVersion(scanner interface{ Scan(...any) error }) (*TemplateVersion, error) {
	var version TemplateVersion
	var definition []byte
	var publishedBy sql.NullInt64
	if err := scanner.Scan(&version.TemplateID, &version.Version, &version.SchemaVersion, &definition, &version.Checksum, &publishedBy, &version.PublishedAt); err != nil {
		return nil, err
	}
	version.Definition = json.RawMessage(definition)
	if publishedBy.Valid {
		version.PublishedBy = &publishedBy.Int64
	}
	return &version, nil
}

func nullableID(value *int64) any {
	if value == nil || *value <= 0 {
		return nil
	}
	return *value
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

const subscriptionSelectP2 = `id, user_id, name, token, format, node_group_id, template_id, template_version, revision, created_at, updated_at`

const subscriptionSelectLegacy = `id, user_id, name, token, format, node_group_id, template_id, created_at, updated_at`

func getSubscriptionCompat(ctx context.Context, db *sql.DB, predicate string, args ...any) (*models.Subscription, error) {
	query := `SELECT ` + subscriptionSelectP2 + ` FROM subscriptions ` + predicate
	subscription, err := scanSubscription(db.QueryRowContext(ctx, query, args...))
	if err == nil || errors.Is(err, sql.ErrNoRows) || !isP2CompatibilityError(err) {
		return subscription, err
	}
	return scanSubscriptionLegacy(db.QueryRowContext(ctx, `SELECT `+subscriptionSelectLegacy+` FROM subscriptions `+predicate, args...))
}

func scanSubscription(row interface{ Scan(...any) error }) (*models.Subscription, error) {
	var subscription models.Subscription
	var templateVersion, revision sql.NullInt64
	if err := row.Scan(&subscription.ID, &subscription.UserID, &subscription.Name, &subscription.Token, &subscription.Format, &subscription.NodeGroupID, &subscription.TemplateID, &templateVersion, &revision, &subscription.CreatedAt, &subscription.UpdatedAt); err != nil {
		return nil, err
	}
	if templateVersion.Valid {
		version := int(templateVersion.Int64)
		subscription.TemplateVersion = &version
	}
	subscription.Revision = revision.Int64
	if subscription.Revision <= 0 {
		subscription.Revision = 1
	}
	return &subscription, nil
}

func scanSubscriptionLegacy(row interface{ Scan(...any) error }) (*models.Subscription, error) {
	var subscription models.Subscription
	if err := row.Scan(&subscription.ID, &subscription.UserID, &subscription.Name, &subscription.Token, &subscription.Format, &subscription.NodeGroupID, &subscription.TemplateID, &subscription.CreatedAt, &subscription.UpdatedAt); err != nil {
		return nil, err
	}
	subscription.Revision = 1
	return &subscription, nil
}

func listSubscriptionsCompat(ctx context.Context, db *sql.DB, predicate string, args ...any) ([]models.Subscription, error) {
	rows, err := db.QueryContext(ctx, `SELECT `+subscriptionSelectP2+` FROM subscriptions `+predicate, args...)
	if err != nil && !isP2CompatibilityError(err) {
		return nil, err
	}
	if err == nil {
		out := make([]models.Subscription, 0)
		compatibilityErr := error(nil)
		for rows.Next() {
			item, scanErr := scanSubscription(rows)
			if scanErr != nil {
				compatibilityErr = scanErr
				break
			}
			out = append(out, *item)
		}
		if compatibilityErr == nil {
			compatibilityErr = rows.Err()
		}
		rows.Close()
		if compatibilityErr == nil {
			return out, nil
		}
		if !isP2CompatibilityError(compatibilityErr) {
			return nil, compatibilityErr
		}
	}
	rows, err = db.QueryContext(ctx, `SELECT `+subscriptionSelectLegacy+` FROM subscriptions `+predicate, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.Subscription, 0)
	for rows.Next() {
		item, scanErr := scanSubscriptionLegacy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func isP2CompatibilityError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "does not exist") ||
		strings.Contains(message, "undefined column") ||
		strings.Contains(message, "destination arguments in scan")
}

func createSubscriptionP2(ctx context.Context, db *sql.DB, subscription *models.Subscription, authorized bool) (int64, error) {
	if subscription == nil {
		return 0, ErrSubscriptionNotFound
	}
	if err := ValidateSubscriptionSelection(subscription.Format, subscription.TemplateID, subscription.TemplateVersion); err != nil {
		return 0, err
	}
	var id int64
	query := `INSERT INTO subscriptions (user_id,name,token,format,node_group_id,template_id,template_version) VALUES ($1,$2,$3,$4,$5,$6,$7)`
	args := []any{subscription.UserID, subscription.Name, subscription.Token, subscription.Format, subscription.NodeGroupID, subscription.TemplateID, subscription.TemplateVersion}
	if authorized {
		query = `INSERT INTO subscriptions (user_id,name,token,format,node_group_id,template_id,template_version)
			SELECT $1,$2,$3,$4,$5,$6,$7 WHERE EXISTS (SELECT 1 FROM users WHERE id=$1 AND is_active=TRUE AND (expire_at IS NULL OR expire_at > now()))
			AND EXISTS (SELECT 1 FROM user_node_groups WHERE user_id=$1 AND group_id=$5)`
	}
	query += ` RETURNING id`
	err := db.QueryRowContext(ctx, query, args[:7]...).Scan(&id)
	if authorized && errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNodeNotAuthorized
	}
	return id, err
}

func createSubscriptionForUserP2(ctx context.Context, db *sql.DB, subscription *models.Subscription) (int64, error) {
	if subscription == nil || subscription.NodeGroupID == nil {
		return 0, ErrNodeNotAuthorized
	}
	if err := ValidateSubscriptionSelection(subscription.Format, subscription.TemplateID, subscription.TemplateVersion); err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return 0, err
	}
	var authorized bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM users u
			JOIN user_node_groups ug ON ug.user_id=u.id
			WHERE u.id=$1 AND u.is_active=TRUE
			  AND (u.expire_at IS NULL OR u.expire_at > now())
			  AND ug.group_id=$2
		)`, subscription.UserID, *subscription.NodeGroupID).Scan(&authorized); err != nil {
		return 0, err
	}
	if !authorized {
		return 0, ErrNodeNotAuthorized
	}
	if err := validateTemplateSelectionTx(ctx, tx, subscription.Format, subscription.TemplateID, subscription.TemplateVersion); err != nil {
		return 0, err
	}
	var id int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO subscriptions (user_id,name,token,format,node_group_id,template_id,template_version)
		VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING id`,
		subscription.UserID, subscription.Name, subscription.Token, subscription.Format,
		subscription.NodeGroupID, subscription.TemplateID, subscription.TemplateVersion).Scan(&id); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func listOverridesCompat(ctx context.Context, db *sql.DB, subID int64) (map[int64]OverrideRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT node_id,display_name,sort_order,icon,params,proxy_group,revision
		FROM subscription_node_overrides WHERE subscription_id=$1`, subID)
	if err == nil {
		out := make(map[int64]OverrideRow)
		for rows.Next() {
			row, scanErr := scanOverrideWithRevision(rows, false)
			if scanErr != nil {
				rows.Close()
				if !isP2CompatibilityError(scanErr) {
					return nil, scanErr
				}
				return listOverridesLegacy(ctx, db, subID)
			}
			out[row.NodeID] = *row
		}
		if scanErr := rows.Err(); scanErr != nil {
			rows.Close()
			if !isP2CompatibilityError(scanErr) {
				return nil, scanErr
			}
			return listOverridesLegacy(ctx, db, subID)
		}
		rows.Close()
		return out, nil
	}
	if !isP2CompatibilityError(err) {
		return nil, err
	}
	return listOverridesLegacy(ctx, db, subID)
}

func listOverridesLegacy(ctx context.Context, db *sql.DB, subID int64) (map[int64]OverrideRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT node_id,COALESCE(display_name,''),sort_order,COALESCE(icon,''),params,COALESCE(proxy_group,'')
		FROM subscription_node_overrides WHERE subscription_id=$1`, subID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[int64]OverrideRow)
	for rows.Next() {
		var row OverrideRow
		var params []byte
		if err := rows.Scan(&row.NodeID, &row.DisplayName, &row.SortOrder, &row.Icon, &params, &row.ProxyGroup); err != nil {
			return nil, err
		}
		row.Params = json.RawMessage(params)
		row.SortOrderSet = true
		row.Revision = 1
		out[row.NodeID] = row
	}
	return out, rows.Err()
}

func listInboundOverridesCompat(ctx context.Context, db *sql.DB, subID int64) (map[OverrideKey]OverrideRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT node_id,inbound_id,display_name,sort_order,icon,params,proxy_group,revision
		FROM subscription_inbound_overrides WHERE subscription_id=$1`, subID)
	if err == nil {
		out := make(map[OverrideKey]OverrideRow)
		for rows.Next() {
			row, scanErr := scanOverrideWithRevision(rows, true)
			if scanErr != nil {
				rows.Close()
				if !isP2CompatibilityError(scanErr) {
					return nil, scanErr
				}
				return listInboundOverridesLegacy(ctx, db, subID)
			}
			out[OverrideKey{NodeID: row.NodeID, InboundID: row.InboundID}] = *row
		}
		if scanErr := rows.Err(); scanErr != nil {
			rows.Close()
			if !isP2CompatibilityError(scanErr) {
				return nil, scanErr
			}
			return listInboundOverridesLegacy(ctx, db, subID)
		}
		rows.Close()
		return out, nil
	}
	if !isP2CompatibilityError(err) {
		return nil, err
	}
	return listInboundOverridesLegacy(ctx, db, subID)
}

func listInboundOverridesLegacy(ctx context.Context, db *sql.DB, subID int64) (map[OverrideKey]OverrideRow, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT node_id,inbound_id,COALESCE(display_name,''),sort_order,COALESCE(icon,''),params,COALESCE(proxy_group,'')
		FROM subscription_inbound_overrides WHERE subscription_id=$1`, subID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[OverrideKey]OverrideRow)
	for rows.Next() {
		var row OverrideRow
		var params []byte
		if err := rows.Scan(&row.NodeID, &row.InboundID, &row.DisplayName, &row.SortOrder, &row.Icon, &params, &row.ProxyGroup); err != nil {
			return nil, err
		}
		row.Params = json.RawMessage(params)
		row.SortOrderSet = true
		row.Revision = 1
		out[OverrideKey{NodeID: row.NodeID, InboundID: row.InboundID}] = row
	}
	return out, rows.Err()
}

func scanOverrideWithRevision(scanner interface{ Scan(...any) error }, inbound bool) (*OverrideRow, error) {
	var row OverrideRow
	var displayName, icon, proxyGroup sql.NullString
	var sortOrder sql.NullInt64
	var params []byte
	if inbound {
		if err := scanner.Scan(&row.NodeID, &row.InboundID, &displayName, &sortOrder, &icon, &params, &proxyGroup, &row.Revision); err != nil {
			return nil, err
		}
	} else if err := scanner.Scan(&row.NodeID, &displayName, &sortOrder, &icon, &params, &proxyGroup, &row.Revision); err != nil {
		return nil, err
	}
	row.DisplayName = displayName.String
	row.Icon = icon.String
	row.ProxyGroup = proxyGroup.String
	row.Params = json.RawMessage(params)
	row.SortOrderSet = sortOrder.Valid
	if sortOrder.Valid {
		row.SortOrder = int(sortOrder.Int64)
	}
	if row.Revision <= 0 {
		row.Revision = 1
	}
	return &row, nil
}

func saveOverrideP2(ctx context.Context, db *sql.DB, subID, nodeID int64, patch OverridePatch) error {
	if err := ValidateSubscriptionOverride(patch); err != nil {
		return err
	}
	params, err := normalizedOverrideParams(patch.Params)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO subscription_node_overrides (subscription_id,node_id,display_name,sort_order,icon,params,proxy_group)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		ON CONFLICT (subscription_id,node_id) DO UPDATE SET
			display_name=EXCLUDED.display_name,sort_order=EXCLUDED.sort_order,
			icon=EXCLUDED.icon,params=EXCLUDED.params,proxy_group=EXCLUDED.proxy_group,
			revision=subscription_node_overrides.revision+1,updated_at=now()`,
		subID, nodeID, nullStringPtr(patch.DisplayName), patch.SortOrder,
		nullStringPtr(patch.Icon), params, nullStringPtr(patch.ProxyGroup))
	return err
}

func saveInboundOverrideP2(ctx context.Context, db *sql.DB, subID, nodeID, inboundID int64, patch OverridePatch) error {
	if err := ValidateSubscriptionOverride(patch); err != nil {
		return err
	}
	params, err := normalizedOverrideParams(patch.Params)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `
		INSERT INTO subscription_inbound_overrides (subscription_id,node_id,inbound_id,display_name,sort_order,icon,params,proxy_group)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		ON CONFLICT (subscription_id,node_id,inbound_id) DO UPDATE SET
			display_name=EXCLUDED.display_name,sort_order=EXCLUDED.sort_order,
			icon=EXCLUDED.icon,params=EXCLUDED.params,proxy_group=EXCLUDED.proxy_group,
			revision=subscription_inbound_overrides.revision+1,updated_at=now()`,
		subID, nodeID, inboundID, nullStringPtr(patch.DisplayName), patch.SortOrder,
		nullStringPtr(patch.Icon), params, nullStringPtr(patch.ProxyGroup))
	return err
}

func validateTemplateSelectionTx(ctx context.Context, tx *sql.Tx, format string, templateID *int64, templateVersion *int) error {
	if err := ValidateSubscriptionSelection(format, templateID, templateVersion); err != nil {
		return err
	}
	if templateID == nil {
		return nil
	}
	var templateFormat, status string
	var publishedVersion sql.NullInt64
	var archivedAt sql.NullTime
	if err := tx.QueryRowContext(ctx, `SELECT format,status,published_version,archived_at FROM templates WHERE id=$1 FOR SHARE`, *templateID).Scan(&templateFormat, &status, &publishedVersion, &archivedAt); errors.Is(err, sql.ErrNoRows) {
		return ErrTemplateNotFound
	} else if err != nil {
		return err
	}
	if archivedAt.Valid || status == "archived" {
		return ErrTemplateArchived
	}
	if templateFormat != format {
		return ErrTemplateFormatMismatch
	}
	if templateVersion == nil {
		if !publishedVersion.Valid {
			return ErrTemplateNotPublished
		}
		return nil
	}
	var versionFormat string
	if err := tx.QueryRowContext(ctx, `SELECT t.format FROM template_versions v JOIN templates t ON t.id=v.template_id WHERE v.template_id=$1 AND v.version=$2`, *templateID, *templateVersion).Scan(&versionFormat); errors.Is(err, sql.ErrNoRows) {
		return ErrTemplateVersionNotFound
	} else if err != nil {
		return err
	}
	if versionFormat != format {
		return ErrTemplateFormatMismatch
	}
	return nil
}

// UpdateForUser changes only subscription metadata and template selection.
// The token remains unchanged, while the row revision advances atomically.
func (r *SubscriptionRepo) UpdateForUser(ctx context.Context, userID, id, expectedRevision int64, name string, groupID, templateID *int64, templateVersion *int, format string) (*models.Subscription, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("subscription repository unavailable")
	}
	if err := ValidateSubscriptionSelection(format, templateID, templateVersion); err != nil {
		return nil, err
	}
	if groupID == nil || *groupID <= 0 {
		return nil, ErrNodeNotAuthorized
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return nil, err
	}
	var currentRevision int64
	var currentGroupID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT node_group_id,revision FROM subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, id, userID).Scan(&currentGroupID, &currentRevision); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	} else if err != nil {
		return nil, err
	}
	if currentRevision <= 0 {
		currentRevision = 1
	}
	if expectedRevision != currentRevision {
		return nil, ErrRevisionConflict
	}
	var authorized bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_node_groups WHERE user_id=$1 AND group_id=$2)`, userID, *groupID).Scan(&authorized); err != nil {
		return nil, err
	}
	if !authorized {
		return nil, ErrNodeNotAuthorized
	}
	if err := validateTemplateSelectionTx(ctx, tx, format, templateID, templateVersion); err != nil {
		return nil, err
	}
	definition, err := templateDefinitionTx(ctx, tx, templateID, templateVersion)
	if err != nil {
		return nil, err
	}
	if err := validateBoundOverridesTx(ctx, tx, id, format, definition); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
		UPDATE subscriptions SET name=$3,node_group_id=$4,template_id=$5,template_version=$6,format=$7,revision=revision+1,updated_at=now()
		WHERE id=$1 AND user_id=$2 AND revision=$8`, id, userID, name, *groupID, templateID, templateVersion, format, expectedRevision)
	if err != nil {
		return nil, err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return nil, err
	} else if affected != 1 {
		return nil, ErrRevisionConflict
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return getSubscriptionCompat(ctx, r.db, `WHERE id=$1 AND user_id=$2`, id, userID)
}

func (r *SubscriptionRepo) UpdateSubscriptionForUser(ctx context.Context, userID, id, expectedRevision int64, name string, groupID, templateID *int64, templateVersion *int, format string) (*models.Subscription, error) {
	return r.UpdateForUser(ctx, userID, id, expectedRevision, name, groupID, templateID, templateVersion, format)
}

func (r *SubscriptionRepo) SetTemplateForUser(ctx context.Context, userID, id, expectedRevision int64, templateID *int64, templateVersion *int, format string) (*models.Subscription, error) {
	current, err := getSubscriptionCompat(ctx, r.db, `WHERE id=$1 AND user_id=$2`, id, userID)
	if err != nil {
		return nil, ErrSubscriptionNotFound
	}
	return r.UpdateForUser(ctx, userID, id, expectedRevision, current.Name, current.NodeGroupID, templateID, templateVersion, format)
}

func (r *SubscriptionRepo) validateTemplateForCreate(ctx context.Context, subscription *models.Subscription) error {
	if subscription == nil || subscription.TemplateID == nil {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return err
	}
	if err := validateTemplateSelectionTx(ctx, tx, subscription.Format, subscription.TemplateID, subscription.TemplateVersion); err != nil {
		return err
	}
	return tx.Commit()
}

// SaveOverrideForUserAtRevision saves a node-level row and increments the
// owning subscription revision in one transaction.
func (r *SubscriptionRepo) SaveOverrideForUserAtRevision(ctx context.Context, userID, subID, nodeID, expectedRevision int64, patch OverridePatch) (*OverrideRow, error) {
	return r.saveOverrideForUserAtRevision(ctx, userID, subID, nodeID, 0, expectedRevision, patch)
}

// SaveInboundOverrideForUserAtRevision saves an entry-scoped row. Presence of
// this row takes precedence over the legacy node-level row at render time.
func (r *SubscriptionRepo) SaveInboundOverrideForUserAtRevision(ctx context.Context, userID, subID, nodeID, inboundID, expectedRevision int64, patch OverridePatch) (*OverrideRow, error) {
	return r.saveOverrideForUserAtRevision(ctx, userID, subID, nodeID, inboundID, expectedRevision, patch)
}

// saveOverrideForUserP2 is the compatibility-preserving legacy write path.
// It has no caller-supplied revision because the original P1 endpoint did not
// expose one; the subscription row is locked and advanced atomically.
func (r *SubscriptionRepo) saveOverrideForUserP2(ctx context.Context, userID, subID, nodeID, inboundID int64, patch OverridePatch) error {
	if err := ValidateSubscriptionOverride(patch); err != nil {
		return err
	}
	params, err := normalizedOverrideParams(patch.Params)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return err
	}
	var groupID sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT node_group_id FROM subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, subID, userID).Scan(&groupID); errors.Is(err, sql.ErrNoRows) {
		return ErrSubscriptionNotFound
	} else if err != nil {
		return err
	}
	if !groupID.Valid {
		return ErrNodeNotAuthorized
	}
	var authorized bool
	if inboundID == 0 {
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM user_node_groups ug
				JOIN node_group_members ngm ON ngm.group_id=ug.group_id
				WHERE ug.user_id=$1 AND ug.group_id=$2 AND ngm.node_id=$3
			)`, userID, groupID.Int64, nodeID).Scan(&authorized)
	} else {
		err = tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM inbounds i
				JOIN nodes n ON n.id=i.node_id
				JOIN node_group_members ngm ON ngm.node_id=i.node_id
				JOIN user_node_groups ug ON ug.group_id=ngm.group_id
				JOIN users u ON u.id=ug.user_id
				WHERE i.id=$1 AND i.node_id=$2 AND i.role='entry' AND n.type='managed'
				  AND ug.user_id=$3 AND ug.group_id=$4 AND u.is_active=TRUE
				  AND (u.expire_at IS NULL OR u.expire_at > now())
			)`, inboundID, nodeID, userID, groupID.Int64).Scan(&authorized)
	}
	if err != nil {
		return err
	}
	if !authorized {
		return ErrNodeNotAuthorized
	}
	if inboundID == 0 {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO subscription_node_overrides (subscription_id,node_id,display_name,sort_order,icon,params,proxy_group,revision)
			VALUES ($1,$2,$3,$4,$5,$6,$7,1)
			ON CONFLICT (subscription_id,node_id) DO UPDATE SET
				display_name=EXCLUDED.display_name,sort_order=EXCLUDED.sort_order,
				icon=EXCLUDED.icon,params=EXCLUDED.params,proxy_group=EXCLUDED.proxy_group,
				revision=subscription_node_overrides.revision+1,updated_at=now()`,
			subID, nodeID, nullStringPtr(patch.DisplayName), patch.SortOrder, nullStringPtr(patch.Icon), params, nullStringPtr(patch.ProxyGroup))
	} else {
		_, err = tx.ExecContext(ctx, `
			INSERT INTO subscription_inbound_overrides (subscription_id,node_id,inbound_id,display_name,sort_order,icon,params,proxy_group,revision)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1)
			ON CONFLICT (subscription_id,node_id,inbound_id) DO UPDATE SET
				display_name=EXCLUDED.display_name,sort_order=EXCLUDED.sort_order,
				icon=EXCLUDED.icon,params=EXCLUDED.params,proxy_group=EXCLUDED.proxy_group,
				revision=subscription_inbound_overrides.revision+1,updated_at=now()`,
			subID, nodeID, inboundID, nullStringPtr(patch.DisplayName), patch.SortOrder, nullStringPtr(patch.Icon), params, nullStringPtr(patch.ProxyGroup))
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1,updated_at=now() WHERE id=$1 AND user_id=$2`, subID, userID)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *SubscriptionRepo) saveOverrideForUserAtRevision(ctx context.Context, userID, subID, nodeID, inboundID, expectedRevision int64, patch OverridePatch) (*OverrideRow, error) {
	if err := ValidateSubscriptionOverride(patch); err != nil {
		return nil, err
	}
	params, err := normalizedOverrideParams(patch.Params)
	if err != nil {
		return nil, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return nil, err
	}
	var groupID sql.NullInt64
	var subscriptionRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT node_group_id,revision FROM subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, subID, userID).Scan(&groupID, &subscriptionRevision); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSubscriptionNotFound
	} else if err != nil {
		return nil, err
	}
	if !groupID.Valid {
		return nil, ErrNodeNotAuthorized
	}
	var authorized bool
	if inboundID == 0 {
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_node_groups ug JOIN node_group_members ngm ON ngm.group_id=ug.group_id WHERE ug.user_id=$1 AND ug.group_id=$2 AND ngm.node_id=$3)`, userID, groupID.Int64, nodeID).Scan(&authorized); err != nil {
			return nil, err
		}
	} else {
		if err := tx.QueryRowContext(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM inbounds i JOIN nodes n ON n.id=i.node_id
				JOIN node_group_members ngm ON ngm.node_id=i.node_id
				JOIN user_node_groups ug ON ug.group_id=ngm.group_id
				JOIN users u ON u.id=ug.user_id
				WHERE i.id=$1 AND i.node_id=$2 AND i.role='entry' AND n.type='managed'
				AND ug.user_id=$3 AND ug.group_id=$4 AND u.is_active=TRUE
				AND (u.expire_at IS NULL OR u.expire_at > now())
			)`, inboundID, nodeID, userID, groupID.Int64).Scan(&authorized); err != nil {
			return nil, err
		}
	}
	if !authorized {
		return nil, ErrNodeNotAuthorized
	}
	currentRevision, exists, err := overrideRevisionForUpdate(ctx, tx, subID, nodeID, inboundID)
	if err != nil {
		return nil, err
	}
	if subscriptionRevision <= 0 {
		subscriptionRevision = 1
	}
	if expectedRevision != currentRevision {
		return nil, ErrRevisionConflict
	}
	if err := validateOverrideTargetTx(ctx, tx, subID, nodeID, inboundID, patch); err != nil {
		return nil, err
	}
	args := []any{nullStringPtr(patch.DisplayName), patch.SortOrder, nullStringPtr(patch.Icon), params, nullStringPtr(patch.ProxyGroup)}
	if inboundID == 0 {
		if exists {
			_, err = tx.ExecContext(ctx, `UPDATE subscription_node_overrides SET display_name=$1,sort_order=$2,icon=$3,params=$4,proxy_group=$5,revision=revision+1,updated_at=now() WHERE subscription_id=$6 AND node_id=$7`, args[0], args[1], args[2], args[3], args[4], subID, nodeID)
		} else {
			_, err = tx.ExecContext(ctx, `INSERT INTO subscription_node_overrides (subscription_id,node_id,display_name,sort_order,icon,params,proxy_group,revision) VALUES ($1,$2,$3,$4,$5,$6,$7,1)`, subID, nodeID, args[0], args[1], args[2], args[3], args[4])
		}
	} else if exists {
		_, err = tx.ExecContext(ctx, `UPDATE subscription_inbound_overrides SET display_name=$1,sort_order=$2,icon=$3,params=$4,proxy_group=$5,revision=revision+1,updated_at=now() WHERE subscription_id=$6 AND node_id=$7 AND inbound_id=$8`, args[0], args[1], args[2], args[3], args[4], subID, nodeID, inboundID)
	} else {
		_, err = tx.ExecContext(ctx, `INSERT INTO subscription_inbound_overrides (subscription_id,node_id,inbound_id,display_name,sort_order,icon,params,proxy_group,revision) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1)`, subID, nodeID, inboundID, args[0], args[1], args[2], args[3], args[4])
	}
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1,updated_at=now() WHERE id=$1 AND user_id=$2`, subID, userID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if inboundID == 0 {
		return getOverrideP2(ctx, r.db, false, subID, nodeID, 0)
	}
	return getOverrideP2(ctx, r.db, true, subID, nodeID, inboundID)
}

func (r *SubscriptionRepo) DeleteOverrideForUser(ctx context.Context, userID, subID, nodeID, expectedRevision int64) error {
	return r.deleteOverrideForUser(ctx, userID, subID, nodeID, 0, expectedRevision)
}

func (r *SubscriptionRepo) DeleteInboundOverrideForUser(ctx context.Context, userID, subID, nodeID, inboundID, expectedRevision int64) error {
	return r.deleteOverrideForUser(ctx, userID, subID, nodeID, inboundID, expectedRevision)
}

func (r *SubscriptionRepo) deleteOverrideForUser(ctx context.Context, userID, subID, nodeID, inboundID, expectedRevision int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := lockSubscriptionTemplates(ctx, tx); err != nil {
		return err
	}
	var groupID sql.NullInt64
	var subscriptionRevision int64
	if err := tx.QueryRowContext(ctx, `SELECT node_group_id,revision FROM subscriptions WHERE id=$1 AND user_id=$2 FOR UPDATE`, subID, userID).Scan(&groupID, &subscriptionRevision); errors.Is(err, sql.ErrNoRows) {
		return ErrSubscriptionNotFound
	} else if err != nil {
		return err
	}
	if subscriptionRevision <= 0 {
		subscriptionRevision = 1
	}
	if !groupID.Valid {
		return ErrNodeNotAuthorized
	}
	var authorized bool
	if inboundID == 0 {
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM user_node_groups ug JOIN node_group_members ngm ON ngm.group_id=ug.group_id WHERE ug.user_id=$1 AND ug.group_id=$2 AND ngm.node_id=$3)`, userID, groupID.Int64, nodeID).Scan(&authorized)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM inbounds i JOIN node_group_members ngm ON ngm.node_id=i.node_id JOIN user_node_groups ug ON ug.group_id=ngm.group_id WHERE i.id=$1 AND i.node_id=$2 AND i.role='entry' AND ug.user_id=$3 AND ug.group_id=$4)`, inboundID, nodeID, userID, groupID.Int64).Scan(&authorized)
	}
	if err != nil {
		return err
	}
	if !authorized {
		return ErrNodeNotAuthorized
	}
	currentRevision, exists, err := overrideRevisionForUpdate(ctx, tx, subID, nodeID, inboundID)
	if err != nil {
		return err
	}
	if !exists {
		return sql.ErrNoRows
	}
	if expectedRevision != currentRevision {
		return ErrRevisionConflict
	}
	var result sql.Result
	if inboundID == 0 {
		result, err = tx.ExecContext(ctx, `DELETE FROM subscription_node_overrides WHERE subscription_id=$1 AND node_id=$2`, subID, nodeID)
	} else {
		result, err = tx.ExecContext(ctx, `DELETE FROM subscription_inbound_overrides WHERE subscription_id=$1 AND node_id=$2 AND inbound_id=$3`, subID, nodeID, inboundID)
	}
	if err != nil {
		return err
	}
	if affected, err := result.RowsAffected(); err != nil {
		return err
	} else if affected != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `UPDATE subscriptions SET revision=revision+1,updated_at=now() WHERE id=$1 AND user_id=$2`, subID, userID); err != nil {
		return err
	}
	return tx.Commit()
}

func normalizedOverrideParams(raw json.RawMessage) ([]byte, error) {
	if raw == nil || len(raw) == 0 || string(raw) == "null" {
		return []byte(`{}`), nil
	}
	if err := ValidateSubscriptionOverride(OverridePatch{Params: raw}); err != nil {
		return nil, err
	}
	var params map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.UseNumber()
	if err := decoder.Decode(&params); err != nil {
		return nil, fmt.Errorf("%w: params", ErrInvalidOverride)
	}
	return json.Marshal(params)
}

func nullStringPtr(value *string) any {
	if value == nil || *value == "" {
		return nil
	}
	return *value
}

func overrideRevisionForUpdate(ctx context.Context, tx *sql.Tx, subID, nodeID, inboundID int64) (int64, bool, error) {
	var revision int64
	if inboundID == 0 {
		err := tx.QueryRowContext(ctx, `SELECT revision FROM subscription_node_overrides WHERE subscription_id=$1 AND node_id=$2 FOR UPDATE`, subID, nodeID).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		return revision, true, err
	}
	err := tx.QueryRowContext(ctx, `SELECT revision FROM subscription_inbound_overrides WHERE subscription_id=$1 AND node_id=$2 AND inbound_id=$3 FOR UPDATE`, subID, nodeID, inboundID).Scan(&revision)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	return revision, true, err
}

func getOverrideP2(ctx context.Context, db *sql.DB, inbound bool, subID, nodeID, inboundID int64) (*OverrideRow, error) {
	var row OverrideRow
	var displayName, icon, proxyGroup sql.NullString
	var sortOrder sql.NullInt64
	var params []byte
	var revision int64
	var err error
	if inbound {
		err = db.QueryRowContext(ctx, `SELECT node_id,inbound_id,display_name,sort_order,icon,params,proxy_group,revision FROM subscription_inbound_overrides WHERE subscription_id=$1 AND node_id=$2 AND inbound_id=$3`, subID, nodeID, inboundID).Scan(&row.NodeID, &row.InboundID, &displayName, &sortOrder, &icon, &params, &proxyGroup, &revision)
	} else {
		err = db.QueryRowContext(ctx, `SELECT node_id,display_name,sort_order,icon,params,proxy_group,revision FROM subscription_node_overrides WHERE subscription_id=$1 AND node_id=$2`, subID, nodeID).Scan(&row.NodeID, &displayName, &sortOrder, &icon, &params, &proxyGroup, &revision)
	}
	if err != nil {
		return nil, err
	}
	row.DisplayName = displayName.String
	row.Icon = icon.String
	row.ProxyGroup = proxyGroup.String
	row.Params = json.RawMessage(params)
	row.SortOrderSet = sortOrder.Valid
	if sortOrder.Valid {
		row.SortOrder = int(sortOrder.Int64)
	}
	row.Revision = revision
	return &row, nil
}

// ListOverridesForUser and ListInboundOverridesForUser verify ownership
// before returning rows, so callers cannot turn this repository into a
// cross-user configuration oracle.
func (r *SubscriptionRepo) ListOverridesForUser(ctx context.Context, userID, subID int64) (map[int64]OverrideRow, error) {
	if _, err := getSubscriptionCompat(ctx, r.db, `WHERE id=$1 AND user_id=$2`, subID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return r.ListOverrides(ctx, subID)
}

func (r *SubscriptionRepo) ListInboundOverridesForUser(ctx context.Context, userID, subID int64) (map[OverrideKey]OverrideRow, error) {
	if _, err := getSubscriptionCompat(ctx, r.db, `WHERE id=$1 AND user_id=$2`, subID, userID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrSubscriptionNotFound
		}
		return nil, err
	}
	return r.ListInboundOverrides(ctx, subID)
}

// EffectiveOverrideRows expresses the P2 inheritance rule without merging
// values: an existing inbound row wins as a whole, otherwise the node row is
// the legacy fallback.
func (r *SubscriptionRepo) EffectiveOverrideRows(ctx context.Context, userID, subID int64) (map[OverrideKey]OverrideRow, error) {
	nodeRows, err := r.ListOverridesForUser(ctx, userID, subID)
	if err != nil {
		return nil, err
	}
	inboundRows, err := r.ListInboundOverridesForUser(ctx, userID, subID)
	if err != nil {
		return nil, err
	}
	out := make(map[OverrideKey]OverrideRow, len(nodeRows)+len(inboundRows))
	for key, row := range inboundRows {
		out[key] = row
	}
	for nodeID, row := range nodeRows {
		key := OverrideKey{NodeID: nodeID}
		if _, exists := out[key]; !exists {
			out[key] = row
		}
	}
	return out, nil
}

// Keep the compiler honest when P2 is built without a caller yet.
var _ = hex.EncodeToString
