package varstore

import protocol "github.com/yttydcs/myflowhub-proto/protocol/varstore"

const (
	varActionSet           = protocol.ActionSet
	varActionAssistSet     = protocol.ActionAssistSet
	varActionSetResp       = protocol.ActionSetResp
	varActionAssistSetResp = protocol.ActionAssistSetResp
	varActionUpSet         = protocol.ActionUpSet
	varActionNotifySet     = protocol.ActionNotifySet

	varActionGet           = protocol.ActionGet
	varActionAssistGet     = protocol.ActionAssistGet
	varActionGetResp       = protocol.ActionGetResp
	varActionAssistGetResp = protocol.ActionAssistGetResp

	varActionList           = protocol.ActionList
	varActionAssistList     = protocol.ActionAssistList
	varActionListResp       = protocol.ActionListResp
	varActionAssistListResp = protocol.ActionAssistListResp

	varActionRevoke           = protocol.ActionRevoke
	varActionAssistRevoke     = protocol.ActionAssistRevoke
	varActionRevokeResp       = protocol.ActionRevokeResp
	varActionAssistRevokeResp = protocol.ActionAssistRevokeResp
	varActionUpRevoke         = protocol.ActionUpRevoke
	varActionNotifyRevoke     = protocol.ActionNotifyRevoke

	varActionSubscribe           = protocol.ActionSubscribe
	varActionAssistSubscribe     = protocol.ActionAssistSubscribe
	varActionSubscribeResp       = protocol.ActionSubscribeResp
	varActionAssistSubscribeResp = protocol.ActionAssistSubscribeResp
	varActionUnsubscribe         = protocol.ActionUnsubscribe
	varActionAssistUnsubscribe   = protocol.ActionAssistUnsubscribe

	varActionVarChanged = protocol.ActionVarChanged
	varActionVarDeleted = protocol.ActionVarDeleted

	visibilityPublic  = protocol.VisibilityPublic
	visibilityPrivate = protocol.VisibilityPrivate
)

type varMessage = protocol.Message
type setReq = protocol.SetReq
type getReq = protocol.GetReq
type listReq = protocol.ListReq
type subscribeReq = protocol.SubscribeReq
type varResp = protocol.VarResp

type varRecord struct {
	Value      string
	Owner      uint32
	IsPublic   bool
	Visibility string
	Type       string
}

type pendingKey struct {
	owner uint32
	name  string
	kind  string
}

const (
	pendingKindGet       = "get"
	pendingKindList      = "list"
	pendingKindSet       = "set"
	pendingKindRevoke    = "revoke"
	pendingKindSubscribe = "subscribe"
)

type pendingSubscriber struct {
	connID     string
	subscriber uint32
	msgID      uint32
	traceID    uint32
}

type pendingWaiter struct {
	connID  string
	msgID   uint32
	traceID uint32
}

type connClosedEvent struct {
	ConnID string
	NodeID uint32
}
