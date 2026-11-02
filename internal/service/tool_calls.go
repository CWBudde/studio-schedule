package service

import (
	"context"
	"fmt"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// RecordToolCall records that p called the MCP server's tool, and how the
// call ended (ADR 0022): an audit event mcp-tool-called with p as its
// actor, no target, the tool, the outcome ("ok" or the kind of the error)
// and p.Via, which must be the MCP channel. The call's arguments are never
// recorded.
func (s *AuthService) RecordToolCall(ctx context.Context, p Principal, tool, outcome string) error {
	err := s.store.InTx(ctx, store.ReadWrite, func(ctx context.Context, tx store.Tx) error {
		return s.auditEvent(ctx, tx, domain.AuditEvent{
			ActorID: p.AccountID, Action: domain.AuditMCPToolCalled,
			Details: domain.AuditDetails{Tool: tool, Outcome: outcome, Via: p.Via},
		})
	})
	if err != nil {
		return fmt.Errorf("record the call of %s: %w", tool, err)
	}

	return nil
}
