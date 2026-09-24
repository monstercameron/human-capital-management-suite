package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type dataOpsArtifactProbe struct{ puts, gets int }

func (p *dataOpsArtifactProbe) Put(context.Context, values.TenantId, string, string, []byte) error {
	p.puts++
	return nil
}
func (p *dataOpsArtifactProbe) Get(context.Context, values.TenantId, string, string) ([]byte, error) {
	p.gets++
	return nil, errors.New("unexpected artifact read")
}

func TestTodo_REV_030_01(t *testing.T) {
	artifacts := &dataOpsArtifactProbe{}
	if _, err := (&Cell{}).NewDataOpsService(artifacts, time.Now); !errors.Is(err, ErrDataOpsUnavailable) {
		t.Fatalf("factory without governed sources = %v, want ErrDataOpsUnavailable", err)
	}
	if artifacts.puts != 0 || artifacts.gets != 0 {
		t.Fatalf("factory accessed artifact store before source validation: %+v", artifacts)
	}
}

func TestTodo_REV_030_01_Security(t *testing.T) {
	artifacts := &dataOpsArtifactProbe{}
	service := &DataOpsService{artifacts: artifacts}
	_, err := service.DiffRecord(context.Background(), DataOpsDiffRequest{
		Subject: values.EntityRef{Tenant: "tenant-forged", Kind: "worker", Id: "worker-1"},
		Fields:  []dataops.FieldID{"person.name"}, ConnectionID: "connection-forged",
	})
	if !errors.Is(err, ErrAuthorizationDenied) {
		t.Fatalf("request with no verified principal = %v, want ErrAuthorizationDenied", err)
	}
	if artifacts.puts != 0 || artifacts.gets != 0 {
		t.Fatalf("unauthenticated request reached durable artifact store: %+v", artifacts)
	}
}
