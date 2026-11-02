// Session persistence stores conversation records, messages, and denormalized statistics.
// Queries here do not infer ownership; callers load/check the session owner where
// needed. Message inserts and counter updates are separate operations rather
// than a transaction spanning a complete streamed response.
package repo

import (
	"context"
	"time"

	"gorm.io/gorm"

	"github.com/chengpeng-cp/nexus-agent/internal/model"
)

// SessionRepo manages session/message tables through the shared GORM handle.
type SessionRepo struct {
	db *gorm.DB
}

// CreateSession inserts metadata and lets database defaults populate UUID, title, status, and timestamps.
func (r *SessionRepo) CreateSession(ctx context.Context, s *model.Session) error {
	return r.db.WithContext(ctx).Create(s).Error
}

// GetSession loads a UUID-keyed session without applying a user filter; services validate its owner.
func (r *SessionRepo) GetSession(ctx context.Context, id string) (*model.Session, error) {
	var s model.Session
	if err := r.db.WithContext(ctx).First(&s, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// ListSessions returns active sessions for one user and an unpaginated total.
// Pinned sessions sort first, then last activity (or creation time when no
// message timestamp exists) in descending order.
func (r *SessionRepo) ListSessions(ctx context.Context, userID int64, offset, limit int) ([]model.Session, int64, error) {
	var (
		list  []model.Session
		total int64
	)
	q := r.db.WithContext(ctx).Model(&model.Session{}).Where("user_id = ? AND status = 'active'", userID)
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	err := q.Order("pinned DESC, COALESCE(last_msg_at, created_at) DESC").
		Offset(offset).Limit(limit).Find(&list).Error
	return list, total, err
}

// UpdateSession saves the complete loaded session, including zero-valued fields.
func (r *SessionRepo) UpdateSession(ctx context.Context, s *model.Session) error {
	return r.db.WithContext(ctx).Save(s).Error
}

// DeleteSession physically deletes the session row; related-row cleanup follows database foreign keys.
func (r *SessionRepo) DeleteSession(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Delete(&model.Session{}, "id = ?", id).Error
}

// CreateMessage persists a message and populates its generated ID for subsequent metadata/usage references.
func (r *SessionRepo) CreateMessage(ctx context.Context, m *model.Message) error {
	return r.db.WithContext(ctx).Create(m).Error
}

// ListMessages limits the query to the newest IDs and reverses them for chronological context.
// The result includes all stored roles; the service chooses which roles/content
// to replay into the next LLM request.
func (r *SessionRepo) ListMessages(ctx context.Context, sessionID string, limit int) ([]model.Message, error) {
	var list []model.Message
	// Fetch newest first to apply the limit, then reverse for conversation order.
	err := r.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("id DESC").Limit(limit).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// UpdateSessionStats atomically increments message_count and token_usage in SQL.
// It also sets last_msg_at. Each call adds one message, so chat invokes it once
// for the user turn and again for the persisted assistant response.
func (r *SessionRepo) UpdateSessionStats(ctx context.Context, sessionID string, addTokens int64) error {
	return r.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ?", sessionID).
		Updates(map[string]any{
			"message_count": gorm.Expr("message_count + 1"),
			"token_usage":   gorm.Expr("token_usage + ?", addTokens),
			"last_msg_at":   time.Now(),
		}).Error
}

// RetitleSession updates a title only while it equals the default new-conversation title.
// It leaves previously renamed sessions unchanged and does not report an error
// when no row matches that title condition.
func (r *SessionRepo) RetitleSession(ctx context.Context, sessionID, title string) error {
	return r.db.WithContext(ctx).Model(&model.Session{}).
		Where("id = ? AND title = ?", sessionID, "New chat").
		Update("title", title).Error
}
