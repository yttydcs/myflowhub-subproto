package management

// 本文件承载 SubProto 中 `management` 模块里与 `actions` 相关的逻辑。

import core "github.com/yttydcs/myflowhub-core"

// registerActions 汇总 management 模块的 echo、info、config 与 node 查询入口。
func registerActions(h *ManagementHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		// echo
		registerEchoActions(h),
		// info
		registerNodeInfoActions(h),
		// config
		registerConfigGetActions(h),
		registerConfigSetActions(h),
		registerConfigListActions(h),
		// nodes
		registerListNodesActions(h),
		registerListSubtreeActions(h),
	}
}
