package edge

import (
	"fmt"
	"time"
)

// ValidateServingContract checks the bounded ingress and long-lived-session
// controls used by a serving cell. The probe is local and deterministic; it
// does not admit a request, retain payloads, or create an external session.
func ValidateServingContract() error {
	limits := IngressLimits{
		ContractVersion:      edge008Version,
		MaxBodyBytes:         1024,
		MaxHeaderBytes:       2048,
		MaxHeaderRead:        2 * time.Second,
		MaxBodyRead:          5 * time.Second,
		MaxActiveConnections: 4,
	}
	attempt := IngressAttempt{
		RequestID: "serving-contract", TenantID: "tenant", ContractVersion: edge008Version,
		BodyBytes: 1, HeaderBytes: 1, HeaderRead: time.Millisecond, BodyRead: time.Millisecond,
		ActiveConnections: 1, TrustState: "VERIFIED",
	}
	decision, err := EvaluateIngress(limits, attempt, EffectPlan{})
	if err != nil || !decision.Allowed {
		return fmt.Errorf("edge: serving contract rejected bounded ingress: decision=%+v err=%v", decision, err)
	}
	attempt.BodyBytes = limits.MaxBodyBytes + 1
	decision, err = EvaluateIngress(limits, attempt, EffectPlan{ProviderRequests: 1})
	if err == nil || decision.Allowed || decision.Effects != (EffectPlan{}) {
		return fmt.Errorf("edge: serving contract admitted oversized ingress: decision=%+v err=%v", decision, err)
	}

	gate, err := NewStreamGate(StreamPolicy{
		ContractVersion: edge009Version, MaxStreams: 1, MaxMessageBytes: 1024,
		MaxMessagesPerMinute: 10, MaxReplayMessages: 2, MaxBufferedMessages: 2,
		MaxLifetime: time.Hour, MaxIdle: time.Minute, ReauthInterval: 10 * time.Minute,
	})
	if err != nil {
		return fmt.Errorf("edge: serving contract stream gate: %w", err)
	}
	at := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	if err := gate.RegisterSession(StreamSession{ID: "serving-session", TenantID: "tenant", IssuedAt: at, ExpiresAt: at.Add(time.Hour)}); err != nil {
		return fmt.Errorf("edge: serving contract stream session: %w", err)
	}
	stream, err := gate.Open(StreamRequest{SessionID: "serving-session", TenantID: "tenant", ContractVersion: edge009Version, At: at})
	if err != nil || !stream.Allowed {
		return fmt.Errorf("edge: serving contract did not open bounded stream: decision=%+v err=%v", stream, err)
	}
	if err := gate.Revoke("serving-session"); err != nil {
		return fmt.Errorf("edge: serving contract revoke: %w", err)
	}
	if _, err := gate.Accept(StreamRequest{SessionID: "serving-session", TenantID: "tenant", ContractVersion: edge009Version, At: at.Add(time.Second)}); err == nil {
		return fmt.Errorf("edge: serving contract admitted a revoked stream")
	}
	return nil
}
