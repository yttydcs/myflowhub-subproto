package topicbus

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
