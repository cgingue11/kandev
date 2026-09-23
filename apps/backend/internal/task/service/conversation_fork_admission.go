package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kandev/kandev/internal/task/models"
	taskrepo "github.com/kandev/kandev/internal/task/repository"
)

func (s *Service) prepareTaskConversationForkAdmission(
	ctx context.Context,
	req *CreateTaskRequest,
) (*models.ConversationForkAdmission, *CreateTaskResult, bool, error) {
	if req.ConversationForkID == "" && req.ConversationForkRequestID == "" {
		return nil, nil, false, nil
	}
	admission, err := s.newTaskConversationForkAdmission(ctx, req)
	if err != nil {
		return nil, nil, true, err
	}
	if result, found, err := s.findConversationForkTaskRetry(ctx, &admission); found || err != nil {
		return &admission, &result, true, err
	}
	draft, err := s.getTaskConversationForkDraft(ctx, admission)
	if err != nil {
		return nil, nil, true, err
	}
	if err := s.validateTaskConversationForkDestination(ctx, req, draft); err != nil {
		return nil, nil, true, err
	}
	return &admission, nil, true, nil
}

func (s *Service) newTaskConversationForkAdmission(ctx context.Context, req *CreateTaskRequest) (models.ConversationForkAdmission, error) {
	if req.ConversationForkID == "" || req.ConversationForkRequestID == "" || len(req.ConversationForkRequestID) > 128 {
		return models.ConversationForkAdmission{}, models.ErrConversationForkConflict
	}
	ownerID, err := s.conversationForkOwnerID(ctx, req.WorkspaceID)
	if err != nil {
		return models.ConversationForkAdmission{}, models.ErrConversationForkNotFound
	}
	fingerprint, err := taskForkDestinationFingerprint(req)
	if err != nil {
		return models.ConversationForkAdmission{}, err
	}
	kind := "task"
	if req.ParentID != "" {
		kind = "child_task"
	}
	return models.ConversationForkAdmission{
		OwnerID: ownerID, WorkspaceID: req.WorkspaceID, ForkID: req.ConversationForkID,
		DestinationKind: kind, DestinationRequestID: req.ConversationForkRequestID, RequestFingerprint: fingerprint,
	}, nil
}

func (s *Service) getTaskConversationForkDraft(ctx context.Context, admission models.ConversationForkAdmission) (models.ConversationForkDraft, error) {
	drafts, ok := s.messages.(taskrepo.ConversationForkDraftRepository)
	if !ok {
		return models.ConversationForkDraft{}, models.ErrConversationForkSourceUnavailable
	}
	draft, err := drafts.GetConversationForkDraft(ctx, admission.OwnerID, admission.ForkID, time.Now().UTC())
	if err != nil {
		return models.ConversationForkDraft{}, err
	}
	return draft, nil
}

func (s *Service) validateTaskConversationForkDestination(ctx context.Context, req *CreateTaskRequest, draft models.ConversationForkDraft) error {
	if draft.Descriptor.State != conversationForkStateDraft {
		return models.ErrConversationForkConflict
	}
	if err := s.AuthorizeSessionAccess(ctx, draft.Descriptor.SourceSessionID); err != nil {
		return err
	}
	if draft.WorkspaceID != req.WorkspaceID {
		return models.ErrConversationForkNotFound
	}
	if req.IsEphemeral || req.ProjectID != "" || req.ParentID != "" && req.ParentID != draft.Descriptor.SourceTaskID {
		return models.ErrConversationForkUnsupportedDestination
	}
	if err := s.prepareWorkspacePolicyForCreation(ctx, req); err != nil {
		return err
	}
	return validateTaskConversationForkWorkspaceMode(s, ctx, req)
}

func validateTaskConversationForkWorkspaceMode(s *Service, ctx context.Context, req *CreateTaskRequest) error {
	mode := workspaceModeForForkRequest(req)
	if req.ParentID == "" && (mode == workspaceModeSharedGroup || mode == workspaceModeInheritParent) {
		return models.ErrConversationForkUnsupportedDestination
	}
	if req.ParentID == "" || mode == "" || mode == workspaceModeNewWorkspace {
		if !s.conversationForkExecutorCanIsolateWorkspace(ctx, req) {
			return models.ErrConversationForkUnsupportedDestination
		}
	}
	return nil
}

func (s *Service) conversationForkExecutorCanIsolateWorkspace(ctx context.Context, req *CreateTaskRequest) bool {
	executorID := strings.TrimSpace(req.ExecutorID)
	if executorID == "" {
		executorID, _ = req.Metadata[models.MetaKeyExecutorID].(string)
		executorID = strings.TrimSpace(executorID)
	}
	if executorID == "" && s.workspaces != nil {
		workspace, err := s.workspaces.GetWorkspace(ctx, req.WorkspaceID)
		if err != nil || workspace == nil {
			return false
		}
		if workspace.DefaultExecutorID != nil {
			executorID = strings.TrimSpace(*workspace.DefaultExecutorID)
		}
	}
	if executorID == "" {
		executorID = models.ExecutorIDLocal
	}
	var executorType models.ExecutorType
	if executorID == models.ExecutorIDLocal {
		executorType = models.ExecutorTypeLocal
	} else {
		if s.executors == nil {
			return false
		}
		executor, err := s.executors.GetExecutor(ctx, executorID)
		if err != nil || executor == nil || executor.Status != models.ExecutorStatusActive {
			return false
		}
		executorType = executor.Type
	}
	switch executorType {
	case models.ExecutorTypeWorktree,
		models.ExecutorTypeLocalDocker,
		models.ExecutorTypeRemoteDocker,
		models.ExecutorTypeSprites,
		models.ExecutorTypeSSH,
		models.ExecutorTypeKubernetes:
		return true
	default:
		return false
	}
}

func (s *Service) findConversationForkTaskRetry(
	ctx context.Context,
	admission *models.ConversationForkAdmission,
) (CreateTaskResult, bool, error) {
	destinations, ok := s.messages.(taskrepo.ConversationForkDestinationRepository)
	if !ok {
		return CreateTaskResult{}, false, models.ErrConversationForkSourceUnavailable
	}
	draft, err := destinations.GetConversationForkByDestinationRequest(ctx, admission.OwnerID, admission.DestinationRequestID)
	if errors.Is(err, models.ErrConversationForkNotFound) {
		return CreateTaskResult{}, false, nil
	}
	if err != nil {
		return CreateTaskResult{}, true, err
	}
	if draft.Descriptor.ID != admission.ForkID || draft.DestinationFingerprint != admission.RequestFingerprint || draft.WorkspaceID != admission.WorkspaceID || draft.Descriptor.DestinationKind != admission.DestinationKind {
		return CreateTaskResult{}, true, models.ErrConversationForkConflict
	}
	task, err := s.tasks.GetTask(ctx, draft.Descriptor.DestinationTaskID)
	if err != nil {
		return CreateTaskResult{}, true, err
	}
	if task == nil {
		return CreateTaskResult{}, true, models.ErrConversationForkConflict
	}
	return CreateTaskResult{Task: task, Outcome: CreateTaskOutcomeFoundSettled}, true, nil
}

func taskForkDestinationFingerprint(req *CreateTaskRequest) (string, error) {
	encoded, err := json.Marshal(struct {
		ForkID    string            `json:"fork_id"`
		Workspace *WorkspacePolicy  `json:"workspace_policy,omitempty"`
		Create    CreateTaskRequest `json:"create"`
	}{req.ConversationForkID, req.WorkspacePolicy, *req})
	if err != nil {
		return "", fmt.Errorf("encode conversation fork destination request: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func workspaceModeForForkRequest(req *CreateTaskRequest) string {
	if req.WorkspacePolicy != nil && req.WorkspacePolicy.Mode != "" {
		return req.WorkspacePolicy.Mode
	}
	workspace, _ := req.Metadata["workspace"].(map[string]interface{})
	mode, _ := workspace["mode"].(string)
	return mode
}
