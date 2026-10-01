package project

import (
	"context"
	"errors"
	"strings"
)

var (
	ErrInvalidIntake        = errors.New("project: invalid governed intake request")
	ErrIntakeAccess         = errors.New("project: governed intake access denied")
	ErrIntakeOwnerAuthority = errors.New("project: intake owner authority is required")
)

type IntakeKind string

const (
	IntakeRequestKind   IntakeKind = "REQUEST"
	IntakeIncidentKind  IntakeKind = "INCIDENT"
	IntakeOperationKind IntakeKind = "OPERATION"
)

type IntakeRequest struct {
	ID             string
	TenantID       string
	ProjectID      ProjectID
	SourceSystem   string
	Kind           IntakeKind
	SafeTitle      string
	Priority       string
	QueueOwnerID   string
	Classification string
	Confidential   bool
	Revision       uint64
	SLAReference   string
}

// IntakeGate is evaluated by the owning request/incident system. It is a
// privacy and admission check, not a project-task write permission.
type IntakeGate interface {
	AuthorizeCoordination(context.Context, IntakeRequest, string) error
}

type IntakeGateFunc func(context.Context, IntakeRequest, string) error

func (f IntakeGateFunc) AuthorizeCoordination(ctx context.Context, request IntakeRequest, actor string) error {
	return f(ctx, request, actor)
}

type IntakeLink struct {
	RequestID       string
	SourceSystem    string
	Kind            IntakeKind
	RequestRevision uint64
	SLAReference    string
}

// CoordinationTask is a safe project-owned task link. It contains no source
// body and cannot mutate request, incident, or SLA truth.
type CoordinationTask struct {
	ID               string
	TenantID         string
	ProjectID        ProjectID
	Title            string
	Priority         string
	QueueOwnerID     string
	Link             IntakeLink
	CoordinationOnly bool
	CanMutateSource  bool
	CanMutateSLA     bool
}

func CreateCoordinationTask(ctx context.Context, request IntakeRequest, taskID, actor string, gate IntakeGate) (CoordinationTask, error) {
	if err := validateIntakeRequest(request); err != nil {
		return CoordinationTask{}, err
	}
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(actor) == "" || gate == nil {
		return CoordinationTask{}, ErrIntakeAccess
	}
	if err := gate.AuthorizeCoordination(ctx, request, actor); err != nil {
		return CoordinationTask{}, err
	}
	return CoordinationTask{ID: taskID, TenantID: request.TenantID, ProjectID: request.ProjectID, Title: request.SafeTitle, Priority: request.Priority, QueueOwnerID: request.QueueOwnerID, Link: IntakeLink{RequestID: request.ID, SourceSystem: request.SourceSystem, Kind: request.Kind, RequestRevision: request.Revision, SLAReference: request.SLAReference}, CoordinationOnly: true, CanMutateSource: false, CanMutateSLA: false}, nil
}

func validateIntakeRequest(request IntakeRequest) error {
	if strings.TrimSpace(request.ID) == "" || strings.TrimSpace(request.TenantID) == "" || strings.TrimSpace(string(request.ProjectID)) == "" || strings.TrimSpace(request.SourceSystem) == "" || strings.TrimSpace(request.SafeTitle) == "" || strings.TrimSpace(request.QueueOwnerID) == "" || strings.TrimSpace(request.Classification) == "" || request.Revision == 0 || request.Kind == "" {
		return ErrInvalidIntake
	}
	if request.Kind != IntakeRequestKind && request.Kind != IntakeIncidentKind && request.Kind != IntakeOperationKind {
		return ErrInvalidIntake
	}
	return nil
}
