package varstore

import (
	"context"
	"encoding/json"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerVarActions(h *VarStoreHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(varActionSet, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleSet(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(varActionAssistSet, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleSet(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(varActionSetResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleSetResp(ctx, data)
		}),
		kit.NewAction(varActionAssistSetResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleSetResp(ctx, data)
		}),
		kit.NewAction(varActionUpSet, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleUpSet(ctx, hdr, data)
		}),
		kit.NewAction(varActionNotifySet, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleNotifySet(ctx, hdr, data)
		}),

		kit.NewAction(varActionGet, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleGet(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(varActionAssistGet, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleGet(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(varActionGetResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleGetResp(ctx, data)
		}),
		kit.NewAction(varActionAssistGetResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleGetResp(ctx, data)
		}),

		kit.NewAction(varActionList, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleList(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(varActionAssistList, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleList(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(varActionListResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleListResp(ctx, data)
		}),
		kit.NewAction(varActionAssistListResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleListResp(ctx, data)
		}),

		kit.NewAction(varActionRevoke, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRevoke(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(varActionAssistRevoke, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleRevoke(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(varActionRevokeResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleRevokeResp(ctx, data)
		}),
		kit.NewAction(varActionAssistRevokeResp, func(ctx context.Context, _ core.IConnection, _ core.IHeader, data json.RawMessage) {
			h.handleRevokeResp(ctx, data)
		}),
		kit.NewAction(varActionUpRevoke, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleUpRevoke(ctx, hdr, data)
		}),
		kit.NewAction(varActionNotifyRevoke, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleNotifyRevoke(ctx, hdr, data)
		}),

		kit.NewAction(varActionSubscribe, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleSubscribe(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(varActionAssistSubscribe, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleSubscribe(ctx, conn, hdr, data, true)
		}),
		kit.NewAction(varActionSubscribeResp, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleSubscribeResp(ctx, hdr, data)
		}),
		kit.NewAction(varActionAssistSubscribeResp, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleSubscribeResp(ctx, hdr, data)
		}),
		kit.NewAction(varActionUnsubscribe, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleUnsubscribe(ctx, conn, hdr, data, false)
		}),
		kit.NewAction(varActionAssistUnsubscribe, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleUnsubscribe(ctx, conn, hdr, data, true)
		}),

		kit.NewAction(varActionVarChanged, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleVarChanged(ctx, hdr, data)
		}, kit.WithKind(kit.ActionKindNotify)),
		kit.NewAction(varActionVarDeleted, func(ctx context.Context, _ core.IConnection, hdr core.IHeader, data json.RawMessage) {
			h.handleVarDeleted(ctx, hdr, data)
		}, kit.WithKind(kit.ActionKindNotify)),
	}
}
