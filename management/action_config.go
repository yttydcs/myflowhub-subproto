package management

import (
	"context"
	"encoding/json"
	"strings"

	core "github.com/yttydcs/myflowhub-core"
	coreconfig "github.com/yttydcs/myflowhub-core/config"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerConfigGetActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionConfigGet, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
		var req configGetReq
		if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.Key) == "" {
			h.sendActionResp(ctx, conn, hdr, actionConfigGetResp, configResp{Code: 400, Msg: "invalid key"})
			return
		}
		srv := core.ServerFromContext(ctx)
		if srv == nil || srv.Config() == nil {
			h.sendActionResp(ctx, conn, hdr, actionConfigGetResp, configResp{Code: 500, Msg: "config unavailable"})
			return
		}
		val, ok := srv.Config().Get(strings.TrimSpace(req.Key))
		if !ok {
			h.sendActionResp(ctx, conn, hdr, actionConfigGetResp, configResp{Code: 404, Msg: "not found", Key: req.Key})
			return
		}
		h.sendActionResp(ctx, conn, hdr, actionConfigGetResp, configResp{Code: 1, Msg: "ok", Key: req.Key, Value: val})
	})
}

// config_set: 更新配置项（仅支持可写 MapConfig）
func registerConfigSetActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionConfigSet, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
		var req configSetReq
		if err := json.Unmarshal(data, &req); err != nil || strings.TrimSpace(req.Key) == "" {
			h.sendActionResp(ctx, conn, hdr, actionConfigSetResp, configResp{Code: 400, Msg: "invalid key"})
			return
		}
		key := strings.TrimSpace(req.Key)
		srv := core.ServerFromContext(ctx)
		if srv == nil || srv.Config() == nil {
			h.sendActionResp(ctx, conn, hdr, actionConfigSetResp, configResp{Code: 500, Msg: "config unavailable"})
			return
		}
		cfg := srv.Config()
		if mc, ok := cfg.(*coreconfig.MapConfig); ok && mc != nil {
			mc.Set(key, req.Value)
			h.sendActionResp(ctx, conn, hdr, actionConfigSetResp, configResp{Code: 1, Msg: "ok", Key: key, Value: req.Value})
			return
		}
		// fallback: try interface with Set
		if setter, ok := cfg.(interface{ Set(string, string) }); ok {
			setter.Set(key, req.Value)
			h.sendActionResp(ctx, conn, hdr, actionConfigSetResp, configResp{Code: 1, Msg: "ok", Key: key, Value: req.Value})
			return
		}
		h.sendActionResp(ctx, conn, hdr, actionConfigSetResp, configResp{Code: 501, Msg: "config not writable"})
	})
}

// config_list: 列出全部配置键（仅支持可枚举配置）
func registerConfigListActions(h *ManagementHandler) core.SubProcessAction {
	return kit.NewAction(actionConfigList, func(ctx context.Context, conn core.IConnection, hdr core.IHeader, _ json.RawMessage) {
		srv := core.ServerFromContext(ctx)
		if srv == nil || srv.Config() == nil {
			h.sendActionResp(ctx, conn, hdr, actionConfigListResp, configListResp{Code: 500, Msg: "config unavailable"})
			return
		}
		cfg := srv.Config()
		if lister, ok := cfg.(interface{ Keys() []string }); ok && lister != nil {
			keys := lister.Keys()
			h.sendActionResp(ctx, conn, hdr, actionConfigListResp, configListResp{Code: 1, Msg: "ok", Keys: keys})
			return
		}
		h.sendActionResp(ctx, conn, hdr, actionConfigListResp, configListResp{Code: 501, Msg: "config not listable"})
	})
}

