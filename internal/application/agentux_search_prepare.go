package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/internal/application/documentembed"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

const localWorkspaceEmbeddingDirectory = ".artifacts/models/potion-base-8M"

// LocalWorkspaceEmbeddingModel requires local model files and never invokes
// a paid provider or substitutes keyword matching for meaning search.
func LocalWorkspaceEmbeddingModel(getenv func(string) string) (*documentembed.Static, error) {
	dir := getenv(documentembed.EnvDir)
	if dir == "" {
		dir = filepath.FromSlash(localWorkspaceEmbeddingDirectory)
	}
	model, err := documentembed.LoadStatic(dir)
	if err != nil {
		return nil, fmt.Errorf("search by meaning unavailable: model files not installed. To turn it on, install tokenizer.json and model.safetensors (optional config.json) from minishlab/potion-base-8M in %s and set HCMNEXT_EMBEDDING_DIR to that directory. Until then Assistant searches workspace documents by keyword: %w", dir, err)
	}
	return model, nil
}

type WorkspaceIndexPreparation struct {
	Model                                      string
	DocumentsIndexed, SectionsIndexed, Pending int
	Workspace                                  WorkspaceDocumentSearchStatus
	Unavailable                                string
	WorkspaceAccessKnown                       bool
}

func PrepareWorkspaceDocumentIndex(ctx context.Context, store *documenthubstore.Store, tenant string, members WorkspaceDocumentMembers, model documentembed.Embedder) (WorkspaceIndexPreparation, error) {
	var out WorkspaceIndexPreparation
	if store == nil || model == nil || !documentembed.IsLocal(model) || isNilPersonaOutputPort(members) {
		return out, &WorkspaceSearchUnavailable{}
	}
	store.SetIndexModel(model.Model())
	out.Model = model.Model()
	_, beforeSections, err := store.IndexReceipt(ctx, tenant, model.Model())
	if err != nil {
		return out, err
	}
	beforeQueue, err := store.IndexQueueStats(ctx, tenant, model.Model())
	if err != nil {
		return out, err
	}
	if _, err := store.EnqueueMissingIndexJobs(ctx, tenant, model.Model()); err != nil {
		return out, err
	}
	config, err := documentembed.IndexerConfigFromEnv(os.Getenv)
	if err != nil {
		return out, err
	}
	progress, err := documentembed.NewIndexer(store, model, config, tenant).Drain(ctx, tenant, nil)
	if err != nil {
		return out, err
	}
	if progress.Failed > 0 {
		return out, fmt.Errorf("Assistant workspace document indexing failed for %d documents", progress.Failed)
	}
	_, afterSections, err := store.IndexReceipt(ctx, tenant, model.Model())
	if err != nil {
		return out, err
	}
	out.DocumentsIndexed, out.SectionsIndexed = progress.Done-beforeQueue.Done, afterSections-beforeSections
	publicMembers, err := members.WorkspaceDocumentMembers(ctx, tenant)
	if err != nil {
		return out, err
	}
	status, err := store.WorkspaceIndexStatus(ctx, tenant, model.Model(), publicMembers)
	if err != nil {
		return out, err
	}
	out.Workspace = WorkspaceDocumentSearchStatus{WorkspaceDocuments: status.Documents, WorkspaceIndexedAt: status.IndexedAt, WorkspacePending: status.Pending}
	out.WorkspaceAccessKnown = true
	out.Pending = progress.Queued + progress.Running
	return out, nil
}
