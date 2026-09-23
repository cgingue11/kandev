package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/db/dialect"
	"github.com/kandev/kandev/internal/task/models"
)

const conversationForkStateDraft = "draft"

type conversationForkDestinationTx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (r *Repository) attachConversationForkToDestinationTx(ctx context.Context, tx conversationForkDestinationTx, admission models.ConversationForkAdmission) error {
	if admission.OwnerID == "" || admission.WorkspaceID == "" || admission.ForkID == "" ||
		admission.DestinationRequestID == "" || admission.RequestFingerprint == "" || admission.DestinationTaskID == "" {
		return fmt.Errorf("%w: incomplete destination request", models.ErrConversationForkConflict)
	}
	handled, err := r.attachExistingConversationForkReceipt(ctx, tx, admission)
	if handled || err != nil {
		return err
	}
	return r.attachNewConversationForkDraft(ctx, tx, admission)
}

func (r *Repository) attachExistingConversationForkReceipt(
	ctx context.Context,
	tx conversationForkDestinationTx,
	admission models.ConversationForkAdmission,
) (bool, error) {
	existing, err := scanConversationForkDraft(tx.QueryRowContext(ctx, r.db.Rebind(`
		SELECT `+conversationForkDraftColumns+` FROM task_conversation_forks
		WHERE owner_id = ? AND destination_request_id = ?
	`), admission.OwnerID, admission.DestinationRequestID))
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return true, fmt.Errorf("check conversation fork destination receipt: %w", err)
	}
	if err := validateExistingConversationForkReceipt(existing, admission); err != nil {
		return true, err
	}
	if admission.DestinationSessionID == "" || existing.Descriptor.DestinationSessionID == admission.DestinationSessionID {
		return true, nil
	}
	if existing.Descriptor.DestinationSessionID != "" {
		return true, fmt.Errorf("%w: destination session already bound", models.ErrConversationForkConflict)
	}
	result, err := tx.ExecContext(ctx, r.db.Rebind(`
			UPDATE task_conversation_forks SET destination_session_id = ?
			WHERE owner_id = ? AND id = ? AND state = 'attached' AND destination_session_id = ''
	`), admission.DestinationSessionID, admission.OwnerID, admission.ForkID)
	if err != nil {
		return true, fmt.Errorf("bind conversation fork to session: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return true, fmt.Errorf("count conversation fork session binding: %w", err)
	}
	if changed != 1 {
		return true, fmt.Errorf("%w: destination session binding changed", models.ErrConversationForkConflict)
	}
	return true, nil
}

func validateExistingConversationForkReceipt(existing models.ConversationForkDraft, admission models.ConversationForkAdmission) error {
	if existing.Descriptor.ID != admission.ForkID || existing.DestinationFingerprint != admission.RequestFingerprint ||
		existing.Descriptor.DestinationTaskID != admission.DestinationTaskID || existing.WorkspaceID != admission.WorkspaceID {
		return fmt.Errorf("%w: destination receipt does not match", models.ErrConversationForkConflict)
	}
	if admission.DestinationKind != "" && existing.Descriptor.DestinationKind != admission.DestinationKind {
		return fmt.Errorf("%w: destination kind does not match", models.ErrConversationForkConflict)
	}
	return nil
}

func (r *Repository) attachNewConversationForkDraft(ctx context.Context, tx conversationForkDestinationTx, admission models.ConversationForkAdmission) error {
	query := `SELECT ` + conversationForkDraftColumns + ` FROM task_conversation_forks WHERE owner_id = ? AND id = ?`
	if dialect.IsPostgres(r.db.DriverName()) {
		query += forUpdateClause
	}
	draft, err := scanConversationForkDraft(tx.QueryRowContext(ctx, r.db.Rebind(query), admission.OwnerID, admission.ForkID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.ErrConversationForkNotFound
		}
		return fmt.Errorf("load conversation fork for destination: %w", err)
	}
	now := time.Now().UTC()
	if draft.WorkspaceID != admission.WorkspaceID {
		return fmt.Errorf("%w: destination workspace mismatch", models.ErrConversationForkConflict)
	}
	if draft.Descriptor.State != conversationForkStateDraft {
		return fmt.Errorf("%w: snapshot is not an unattached draft", models.ErrConversationForkConflict)
	}
	if !now.Before(draft.Descriptor.ExpiresAt) {
		return models.ErrConversationForkExpired
	}
	if err := r.bindConversationForkAttachmentsTx(ctx, tx, admission, draft); err != nil {
		return err
	}
	var destinationRequestID any = admission.DestinationRequestID
	result, err := tx.ExecContext(ctx, r.db.Rebind(`
		UPDATE task_conversation_forks
		SET state = 'attached', destination_kind = ?, destination_task_id = ?, destination_session_id = ?,
			destination_request_id = ?, destination_request_fingerprint = ?
		WHERE owner_id = ? AND id = ? AND state = 'draft' AND expires_at > ?
	`), admission.DestinationKind, admission.DestinationTaskID, admission.DestinationSessionID, destinationRequestID,
		admission.RequestFingerprint, admission.OwnerID, admission.ForkID, now)
	if err != nil {
		if isConversationForkUniqueViolation(err) {
			return models.ErrConversationForkConflict
		}
		return fmt.Errorf("attach conversation fork: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return fmt.Errorf("count attached conversation fork rows: %w", err)
		}
		return fmt.Errorf("%w: snapshot attachment update matched %d rows", models.ErrConversationForkConflict, changed)
	}
	return nil
}

func (r *Repository) bindPendingConversationForkSessionTx(ctx context.Context, tx conversationForkDestinationTx, taskID, sessionID string) (string, bool, error) {
	if taskID == "" || sessionID == "" {
		return "", false, fmt.Errorf("conversation fork pending session binding requires task and session IDs")
	}
	query := `SELECT ` + conversationForkDraftColumns + ` FROM task_conversation_forks
		WHERE destination_task_id = ? AND destination_kind IN ('task', 'child_task') AND state = 'attached' AND destination_session_id = ''
		ORDER BY created_at ASC, id ASC LIMIT 1`
	if dialect.IsPostgres(r.db.DriverName()) {
		query += forUpdateClause
	}
	draft, err := scanConversationForkDraft(tx.QueryRowContext(ctx, r.db.Rebind(query), taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("load pending task conversation fork: %w", err)
	}
	result, err := tx.ExecContext(ctx, r.db.Rebind(`
		UPDATE task_conversation_forks SET destination_session_id = ?
		WHERE id = ? AND destination_task_id = ? AND state = 'attached' AND destination_session_id = ''
	`), sessionID, draft.Descriptor.ID, taskID)
	if err != nil {
		return "", false, fmt.Errorf("bind pending task conversation fork to session: %w", err)
	}
	if changed, err := result.RowsAffected(); err != nil || changed != 1 {
		if err != nil {
			return "", false, fmt.Errorf("count pending task conversation fork session binding: %w", err)
		}
		return "", false, fmt.Errorf("%w: pending task fork session binding changed", models.ErrConversationForkConflict)
	}
	return draft.Descriptor.ID, true, nil
}

func (r *Repository) bindConversationForkAttachmentsTx(ctx context.Context, tx conversationForkDestinationTx, admission models.ConversationForkAdmission, draft models.ConversationForkDraft) error {
	attachments := draft.Descriptor.AttachmentDescriptors
	if len(attachments) > models.MaxMessageAttachmentCount {
		return models.ErrConversationForkLimitExceeded
	}
	var total int64
	for _, attachment := range attachments {
		if attachment.ID == "" || attachment.Size < 0 || attachment.Size > models.MaxMessageAttachmentBytes {
			return models.ErrConversationForkAttachmentMissing
		}
		total += attachment.Size
	}
	if total > models.MaxMessageAttachmentBytes {
		return models.ErrConversationForkLimitExceeded
	}
	now := time.Now().UTC()
	for _, attachment := range attachments {
		result, err := tx.ExecContext(ctx, r.db.Rebind(`
			UPDATE task_message_attachments
			SET task_id = ?, session_id = '', state = ?, updated_at = ?
			WHERE id = ? AND owner_id = ? AND workspace_id = ? AND state = ? AND expires_at > ?
		`), admission.DestinationTaskID, models.AttachmentStateClaimed, now, attachment.ID,
			admission.OwnerID, draft.WorkspaceID, models.AttachmentStateStaged, now)
		if err != nil {
			return fmt.Errorf("bind conversation fork attachment: %w", err)
		}
		if changed, err := result.RowsAffected(); err != nil || changed != 1 {
			if err != nil {
				return fmt.Errorf("count bound conversation fork attachment: %w", err)
			}
			return models.ErrConversationForkAttachmentMissing
		}
	}
	return nil
}

func (r *Repository) GetConversationForkByDestinationRequest(ctx context.Context, ownerID, requestID string) (models.ConversationForkDraft, error) {
	if ownerID == "" || requestID == "" {
		return models.ConversationForkDraft{}, models.ErrConversationForkNotFound
	}
	draft, err := scanConversationForkDraft(r.ro.QueryRowxContext(ctx, r.ro.Rebind(`
		SELECT `+conversationForkDraftColumns+` FROM task_conversation_forks
		WHERE owner_id = ? AND destination_request_id = ?
	`), ownerID, requestID))
	if errors.Is(err, sql.ErrNoRows) {
		return models.ConversationForkDraft{}, models.ErrConversationForkNotFound
	}
	return draft, err
}

func (r *Repository) GetPendingConversationForkForTask(ctx context.Context, ownerID, taskID string) (models.ConversationForkDraft, error) {
	if ownerID == "" || taskID == "" {
		return models.ConversationForkDraft{}, models.ErrConversationForkNotFound
	}
	draft, err := scanConversationForkDraft(r.ro.QueryRowxContext(ctx, r.ro.Rebind(`
		SELECT `+conversationForkDraftColumns+` FROM task_conversation_forks
		WHERE owner_id = ? AND destination_task_id = ? AND destination_session_id = '' AND state = 'attached'
		ORDER BY created_at ASC, id ASC LIMIT 1
	`), ownerID, taskID))
	if errors.Is(err, sql.ErrNoRows) {
		return models.ConversationForkDraft{}, models.ErrConversationForkNotFound
	}
	return draft, err
}

func (r *Repository) GetConversationForkByDestinationSession(ctx context.Context, ownerID, taskID, sessionID string) (models.ConversationForkDraft, error) {
	if ownerID == "" || taskID == "" || sessionID == "" {
		return models.ConversationForkDraft{}, models.ErrConversationForkNotFound
	}
	draft, err := scanConversationForkDraft(r.ro.QueryRowxContext(ctx, r.ro.Rebind(`
		SELECT `+conversationForkDraftColumns+` FROM task_conversation_forks
		WHERE owner_id = ? AND destination_task_id = ? AND destination_session_id = ? AND state = 'attached'
	`), ownerID, taskID, sessionID))
	if errors.Is(err, sql.ErrNoRows) {
		return models.ConversationForkDraft{}, models.ErrConversationForkNotFound
	}
	return draft, err
}

func (r *Repository) BindConversationForkToSession(ctx context.Context, admission models.ConversationForkAdmission) (models.ConversationForkDraft, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return models.ConversationForkDraft{}, fmt.Errorf("begin conversation fork session binding: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := r.attachConversationForkToDestinationTx(ctx, tx, admission); err != nil {
		return models.ConversationForkDraft{}, err
	}
	if err := tx.Commit(); err != nil {
		return models.ConversationForkDraft{}, fmt.Errorf("commit conversation fork session binding: %w", err)
	}
	return r.GetConversationForkByDestinationRequest(ctx, admission.OwnerID, admission.DestinationRequestID)
}

func (r *Repository) DeleteConversationForksByDestinationTask(ctx context.Context, taskID string) error {
	if taskID == "" {
		return nil
	}
	if _, err := r.db.ExecContext(ctx, r.db.Rebind(`
		DELETE FROM task_conversation_forks WHERE destination_task_id = ? AND state = 'attached'
	`), taskID); err != nil {
		return fmt.Errorf("delete conversation forks for destination task: %w", err)
	}
	return nil
}

func isConversationForkUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "unique constraint") || strings.Contains(message, "duplicate key") || strings.Contains(message, "23505")
}
