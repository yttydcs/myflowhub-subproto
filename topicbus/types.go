package topicbus

import protocol "github.com/yttydcs/myflowhub-proto/protocol/topicbus"

// 子协议：Topic 订阅/发布。
const SubProtoTopicBus uint8 = protocol.SubProtoTopicBus

// 动作常量定义。
const (
	actionSubscribe          = protocol.ActionSubscribe
	actionSubscribeResp      = protocol.ActionSubscribeResp
	actionSubscribeBatch     = protocol.ActionSubscribeBatch
	actionSubscribeBatchResp = protocol.ActionSubscribeBatchResp

	actionUnsubscribe          = protocol.ActionUnsubscribe
	actionUnsubscribeResp      = protocol.ActionUnsubscribeResp
	actionUnsubscribeBatch     = protocol.ActionUnsubscribeBatch
	actionUnsubscribeBatchResp = protocol.ActionUnsubscribeBatchResp

	actionListSubs     = protocol.ActionListSubs
	actionListSubsResp = protocol.ActionListSubsResp

	actionPublish = protocol.ActionPublish
)

type message = protocol.Message
type subscribeReq = protocol.SubscribeReq
type subscribeBatchReq = protocol.SubscribeBatchReq
type publishReq = protocol.PublishReq
type resp = protocol.Resp
type listResp = protocol.ListResp
