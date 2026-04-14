package topicbus

// 本文件承载 SubProto 中 `topicbus` 模块里与 `actions` 相关的逻辑。

import (
	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/subproto/kit"
)

func registerActions(h *TopicBusHandler) []core.SubProcessAction {
	return []core.SubProcessAction{
		kit.NewAction(actionSubscribe, h.handleSubscribe),
		kit.NewAction(actionSubscribeBatch, h.handleSubscribeBatch),
		kit.NewAction(actionUnsubscribe, h.handleUnsubscribe),
		kit.NewAction(actionUnsubscribeBatch, h.handleUnsubscribeBatch),
		kit.NewAction(actionListSubs, h.handleListSubs),
		kit.NewAction(actionPublish, h.handlePublish),
	}
}
