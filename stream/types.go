package stream

// 本文件承载 SubProto 中 `stream` 模块里与 `types` 相关的逻辑。

import protocol "github.com/yttydcs/myflowhub-proto/protocol/stream"

const SubProtoStream uint8 = protocol.SubProtoStream

const (
	kindCtrl byte = protocol.KindCtrl
	kindData byte = protocol.KindData
	kindAck  byte = protocol.KindAck
)

const (
	actionAnnounce             = protocol.ActionAnnounce
	actionAnnounceResp         = protocol.ActionAnnounceResp
	actionWithdraw             = protocol.ActionWithdraw
	actionWithdrawResp         = protocol.ActionWithdrawResp
	actionListSources          = protocol.ActionListSources
	actionListSourcesResp      = protocol.ActionListSourcesResp
	actionGetSource            = protocol.ActionGetSource
	actionGetSourceResp        = protocol.ActionGetSourceResp
	actionAnnounceConsumer     = protocol.ActionAnnounceConsumer
	actionAnnounceConsumerResp = protocol.ActionAnnounceConsumerResp
	actionWithdrawConsumer     = protocol.ActionWithdrawConsumer
	actionWithdrawConsumerResp = protocol.ActionWithdrawConsumerResp
	actionListConsumers        = protocol.ActionListConsumers
	actionListConsumersResp    = protocol.ActionListConsumersResp
	actionGetConsumer          = protocol.ActionGetConsumer
	actionGetConsumerResp      = protocol.ActionGetConsumerResp
	actionSubscribe            = protocol.ActionSubscribe
	actionSubscribeResp        = protocol.ActionSubscribeResp
	actionUnsubscribe          = protocol.ActionUnsubscribe
	actionUnsubscribeResp      = protocol.ActionUnsubscribeResp
	actionConnect              = protocol.ActionConnect
	actionConnectResp          = protocol.ActionConnectResp
	actionDisconnect           = protocol.ActionDisconnect
	actionDisconnectResp       = protocol.ActionDisconnectResp
	actionSignal               = protocol.ActionSignal
	actionSignalResp           = protocol.ActionSignalResp
)

const (
	permPublish   = protocol.PermStreamPublish
	permConsume   = protocol.PermStreamConsume
	permSubscribe = protocol.PermStreamSubscribe
	permConnect   = protocol.PermStreamConnect
)

const (
	streamKindMusic  = protocol.StreamKindMusic
	streamKindVideo  = protocol.StreamKindVideo
	streamKindText   = protocol.StreamKindText
	streamKindCustom = protocol.StreamKindCustom
)

const (
	modeLive    = protocol.ModeLive
	modeBounded = protocol.ModeBounded
)

const (
	unitModeFrame = protocol.UnitModeFrame
	unitModeChunk = protocol.UnitModeChunk
)

const (
	signalOpPause           = protocol.SignalOpPause
	signalOpResume          = protocol.SignalOpResume
	signalOpMetadataUpdate  = protocol.SignalOpMetadataUpdate
	signalOpKeyframeRequest = protocol.SignalOpKeyframeRequest
	signalOpCustom          = protocol.SignalOpCustom
)

const (
	headerVersionV1       = protocol.HeaderVersionV1
	dataFlagEOS           = protocol.DataFlagEOS
	dataFlagKeyframe      = protocol.DataFlagKeyframe
	dataFlagConfig        = protocol.DataFlagConfig
	dataFlagDiscontinuity = protocol.DataFlagDiscontinuity
)

const (
	actionDeliveryPrepare      = "delivery_prepare"
	actionDeliveryPrepareResp  = "delivery_prepare_resp"
	actionDeliveryActivate     = "delivery_activate"
	actionDeliveryActivateResp = "delivery_activate_resp"
	actionDeliveryAbort        = "delivery_abort"
	actionDeliveryAbortResp    = "delivery_abort_resp"
	actionDeliveryClose        = "delivery_close"
	actionDeliveryCloseResp    = "delivery_close_resp"
)

type message = protocol.Message

type sourceDescriptor = protocol.SourceDescriptor
type consumerDescriptor = protocol.ConsumerDescriptor

type announceReq = protocol.AnnounceReq
type announceResp = protocol.AnnounceResp
type withdrawReq = protocol.WithdrawReq
type withdrawResp = protocol.WithdrawResp
type listSourcesReq = protocol.ListSourcesReq
type listSourcesResp = protocol.ListSourcesResp
type getSourceReq = protocol.GetSourceReq
type getSourceResp = protocol.GetSourceResp
type announceConsumerReq = protocol.AnnounceConsumerReq
type announceConsumerResp = protocol.AnnounceConsumerResp
type withdrawConsumerReq = protocol.WithdrawConsumerReq
type withdrawConsumerResp = protocol.WithdrawConsumerResp
type listConsumersReq = protocol.ListConsumersReq
type listConsumersResp = protocol.ListConsumersResp
type getConsumerReq = protocol.GetConsumerReq
type getConsumerResp = protocol.GetConsumerResp
type subscribeReq = protocol.SubscribeReq
type subscribeResp = protocol.SubscribeResp
type unsubscribeReq = protocol.UnsubscribeReq
type unsubscribeResp = protocol.UnsubscribeResp
type connectReq = protocol.ConnectReq
type connectResp = protocol.ConnectResp
type disconnectReq = protocol.DisconnectReq
type disconnectResp = protocol.DisconnectResp
type signalReq = protocol.SignalReq
type signalResp = protocol.SignalResp

type streamDataHeaderV1 = protocol.StreamDataHeaderV1
type streamAckHeaderV1 = protocol.StreamAckHeaderV1
