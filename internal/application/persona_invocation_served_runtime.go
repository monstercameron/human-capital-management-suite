package application

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// composeServedPersonaInvocation installs a complete durable worker in the
// existing chat service. Server-owned post and database ports are bound here,
// after chat and persona discovery have both been composed.
func composeServedPersonaInvocation(served *streamingChatService, personas *personaServeWiring, database composedAgentDatabase, configured *PersonaInvocationProductionConfig, logger personaInvocationLogger, classifiers ...PersonaAcceptedHumanPostClassifier) (*PersonaInvocationProductionRuntime, error) {
	if configured == nil {
		return nil, nil
	}
	if database.store == nil || database.personas == nil || served == nil || personas == nil {
		return nil, errPersonaInvocationProductionComposition
	}
	ports, err := composePersonaInvocationServedPorts(served, personas, logger)
	if err != nil {
		return nil, fmt.Errorf("compose served persona invocation ports: %w", err)
	}
	config := *configured
	config.AgentStore = database.store
	config.TenantUUID = tenantKeyMapper[values.TenantId](pgstore.TenantID)
	config.Chat = ports.chat
	if len(classifiers) > 1 {
		return nil, errPersonaInvocationProductionComposition
	}
	if len(classifiers) == 1 {
		config.Chat, err = ports.ClassifiedHumanWriter(classifiers[0])
		if err != nil {
			return nil, fmt.Errorf("compose served persona source classification: %w", err)
		}
	}
	config.References = ports.references
	config.Failures, err = newPersonaDurableInvocationFailureSink(database.store, ports.failures)
	if err != nil {
		return nil, fmt.Errorf("compose served persona failure status: %w", err)
	}
	config.Run.Reply, err = newPersonaReplyReceiptRecorder(config.Run.Reply, database.store, database.personas)
	if err != nil {
		return nil, fmt.Errorf("%w: compose served persona reply provenance: %v", errPersonaInvocationProductionComposition, err)
	}
	runtime, err := NewDatabasePersonaInvocationProductionRuntime(config)
	if err != nil {
		return nil, fmt.Errorf("compose served persona model worker: %w", err)
	}
	if err := served.bindPersonaInvocation(runtime.Wiring); err != nil {
		return nil, fmt.Errorf("bind served persona invocation: %w", err)
	}
	return runtime, nil
}
