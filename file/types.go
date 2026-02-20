package file

import protocol "github.com/yttydcs/myflowhub-proto/protocol/file"

// 子协议：file（节点间文件传输）。
const SubProtoFile uint8 = protocol.SubProtoFile

// payload[0]：帧类型。
const (
	kindCtrl byte = protocol.KindCtrl
	kindData byte = protocol.KindData
	kindAck  byte = protocol.KindAck
)

const (
	actionRead      = protocol.ActionRead
	actionWrite     = protocol.ActionWrite
	actionReadResp  = protocol.ActionReadResp
	actionWriteResp = protocol.ActionWriteResp
)

const (
	opPull     = protocol.OpPull
	opOffer    = protocol.OpOffer
	opList     = protocol.OpList
	opReadText = protocol.OpReadText
)

type message = protocol.Message
type readReq = protocol.ReadReq
type readResp = protocol.ReadResp
type writeReq = protocol.WriteReq
type writeResp = protocol.WriteResp
