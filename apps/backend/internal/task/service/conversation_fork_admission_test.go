package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kandev/kandev/internal/task/models"
)

func TestConversationForkTaskAdmissionIsIdempotentAndBindsMetadata(t *testing.T) {
	svc, _, repo := createTestService(t)
	seedConversationForkServiceSource(t, repo)
	ctx := ctxAs("user-a")
	if err := svc.workflows.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-fork-destination", WorkspaceID: "workspace-fork-service", Name: "Destination"}); err != nil {
		t.Fatalf("create destination workflow: %v", err)
	}
	worktree := createTestExecutor(t, svc, "Fork worktree", models.ExecutorTypeWorktree)
	fork, err := svc.CreateConversationForkDraft(ctx, models.ConversationForkCreateRequest{
		Source:         models.ConversationForkSourceRequest{SessionID: "session-fork-service", CutoffMessageID: "message-fork-service"},
		DraftRequestID: "draft-destination-admission",
	})
	if err != nil {
		t.Fatalf("create fork draft: %v", err)
	}
	request := &CreateTaskRequest{
		WorkspaceID: "workspace-fork-service", WorkflowID: "workflow-fork-destination", Title: "Fork task",
		Description: "Continue from this conversation.", WorkspacePolicy: &WorkspacePolicy{Mode: workspaceModeNewWorkspace},
		ExecutorID:         worktree.ID,
		ConversationForkID: fork.Descriptor.ID, ConversationForkRequestID: "task-create-fork-request",
	}
	created, err := svc.CreateTask(ctx, request)
	if err != nil {
		t.Fatalf("create fork task: %v", err)
	}
	if created.Task == nil || created.Task.ID == "" || created.Outcome != CreateTaskOutcomeCreated {
		t.Fatalf("created fork task = %+v", created)
	}
	if created.Task.Metadata[models.MetaKeyConversationForkID] != fork.Descriptor.ID {
		t.Fatalf("destination task metadata = %#v, want server-authored fork reference", created.Task.Metadata)
	}
	stored, err := svc.messages.(interface {
		GetConversationForkByDestinationRequest(context.Context, string, string) (models.ConversationForkDraft, error)
	}).GetConversationForkByDestinationRequest(ctx, "user-a", "task-create-fork-request")
	if err != nil || stored.Descriptor.State != "attached" || stored.Descriptor.DestinationTaskID != created.Task.ID {
		t.Fatalf("stored destination binding = %+v, %v", stored.Descriptor, err)
	}

	retry, err := svc.CreateTask(ctx, &CreateTaskRequest{
		WorkspaceID: request.WorkspaceID, WorkflowID: request.WorkflowID, Title: request.Title,
		Description: request.Description, ExecutorID: worktree.ID, WorkspacePolicy: &WorkspacePolicy{Mode: workspaceModeNewWorkspace},
		ConversationForkID: fork.Descriptor.ID, ConversationForkRequestID: "task-create-fork-request",
	})
	if err != nil || retry.Task == nil || retry.Task.ID != created.Task.ID || retry.Outcome == CreateTaskOutcomeCreated {
		t.Fatalf("idempotent fork task retry = %+v, %v", retry, err)
	}
	if _, err := svc.CreateTask(ctx, &CreateTaskRequest{
		WorkspaceID: request.WorkspaceID, WorkflowID: request.WorkflowID, Title: "Changed payload",
		Description: request.Description, ExecutorID: worktree.ID, WorkspacePolicy: &WorkspacePolicy{Mode: workspaceModeNewWorkspace},
		ConversationForkID: fork.Descriptor.ID, ConversationForkRequestID: "task-create-fork-request",
	}); !errors.Is(err, models.ErrConversationForkConflict) {
		t.Fatalf("changed retry error = %v, want conflict", err)
	}
}

func TestConversationForkTaskAdmissionRejectsWrongWorkspaceModeAndParent(t *testing.T) {
	svc, _, repo := createTestService(t)
	seedConversationForkServiceSource(t, repo)
	ctx := ctxAs("user-a")
	if err := svc.workflows.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-fork-rejected", WorkspaceID: "workspace-fork-service", Name: "Destination"}); err != nil {
		t.Fatalf("create destination workflow: %v", err)
	}
	fork, err := svc.CreateConversationForkDraft(ctx, models.ConversationForkCreateRequest{
		Source:         models.ConversationForkSourceRequest{SessionID: "session-fork-service", CutoffMessageID: "message-fork-service"},
		DraftRequestID: "draft-rejected-admission",
	})
	if err != nil {
		t.Fatalf("create fork draft: %v", err)
	}
	base := CreateTaskRequest{
		WorkspaceID: "workspace-fork-service", WorkflowID: "workflow-fork-rejected", Title: "Fork task",
		Description: "Continue.", ConversationForkID: fork.Descriptor.ID,
	}
	shared := base
	shared.ConversationForkRequestID = "shared-new-task"
	shared.WorkspacePolicy = &WorkspacePolicy{Mode: workspaceModeSharedGroup, GroupID: "group-a"}
	if _, err := svc.CreateTask(ctx, &shared); !errors.Is(err, models.ErrConversationForkUnsupportedDestination) {
		t.Fatalf("shared new-task mode error = %v, want unsupported", err)
	}
	wrongParent := base
	wrongParent.ConversationForkRequestID = "wrong-parent"
	wrongParent.ParentID = "unrelated-task"
	wrongParent.WorkspacePolicy = &WorkspacePolicy{Mode: workspaceModeNewWorkspace}
	if _, err := svc.CreateTask(ctx, &wrongParent); !errors.Is(err, models.ErrConversationForkUnsupportedDestination) {
		t.Fatalf("unrelated parent error = %v, want unsupported", err)
	}
}

func TestConversationForkSeparateWorkspaceRejectsLocalExecutor(t *testing.T) {
	svc, _, repo := createTestService(t)
	seedConversationForkServiceSource(t, repo)
	ctx := ctxAs("user-a")
	if err := svc.workflows.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-fork-local", WorkspaceID: "workspace-fork-service", Name: "Destination"}); err != nil {
		t.Fatalf("create destination workflow: %v", err)
	}
	local := createTestExecutor(t, svc, "Local runner", models.ExecutorTypeLocal)
	fork, err := svc.CreateConversationForkDraft(ctx, models.ConversationForkCreateRequest{
		Source:         models.ConversationForkSourceRequest{SessionID: "session-fork-service", CutoffMessageID: "message-fork-service"},
		DraftRequestID: "draft-local-admission",
	})
	if err != nil {
		t.Fatalf("create fork draft: %v", err)
	}

	requests := []CreateTaskRequest{
		{
			WorkspaceID: "workspace-fork-service", WorkflowID: "workflow-fork-local", Title: "Local fork task",
			Description: "Continue.", ExecutorID: local.ID, WorkspacePolicy: &WorkspacePolicy{Mode: workspaceModeNewWorkspace},
			ConversationForkID: fork.Descriptor.ID, ConversationForkRequestID: "new-task-local",
		},
		{
			WorkspaceID: "workspace-fork-service", WorkflowID: "workflow-fork-local", ParentID: "task-fork-service", Title: "Local fork child",
			Description: "Continue.", ExecutorID: local.ID, WorkspacePolicy: &WorkspacePolicy{Mode: workspaceModeNewWorkspace},
			ConversationForkID: fork.Descriptor.ID, ConversationForkRequestID: "child-task-local",
		},
	}
	for _, request := range requests {
		if _, err := svc.CreateTask(ctx, &request); !errors.Is(err, models.ErrConversationForkUnsupportedDestination) {
			t.Errorf("local separate-workspace admission error = %v, want unsupported", err)
		}
	}
	stored, err := svc.GetConversationForkDraft(ctx, fork.Descriptor.ID)
	if err != nil || stored.Descriptor.State != "draft" {
		t.Fatalf("rejected fork state = %+v, %v; want unchanged draft", stored.Descriptor, err)
	}
}

func TestConversationForkTaskAdmissionRequiresCurrentSourceAccess(t *testing.T) {
	svc, _, repo := createTestService(t)
	seedConversationForkServiceSource(t, repo)
	ctx := ctxAs("user-a")
	if err := svc.workflows.CreateWorkflow(ctx, &models.Workflow{ID: "workflow-fork-access", WorkspaceID: "workspace-fork-service", Name: "Destination"}); err != nil {
		t.Fatalf("create destination workflow: %v", err)
	}
	fork, err := svc.CreateConversationForkDraft(ctx, models.ConversationForkCreateRequest{
		Source:         models.ConversationForkSourceRequest{SessionID: "session-fork-service", CutoffMessageID: "message-fork-service"},
		DraftRequestID: "draft-access-admission",
	})
	if err != nil {
		t.Fatalf("create fork draft: %v", err)
	}
	if _, err := svc.CreateTask(ctxAs("user-b"), &CreateTaskRequest{
		WorkspaceID: "workspace-fork-service", WorkflowID: "workflow-fork-access", Title: "Fork task",
		Description: "Continue.", WorkspacePolicy: &WorkspacePolicy{Mode: workspaceModeNewWorkspace},
		ConversationForkID: fork.Descriptor.ID, ConversationForkRequestID: "inaccessible-source",
	}); err == nil {
		t.Fatal("task creation succeeded without access to the source workspace")
	}
}
