package broker

import (
	"sync"

	protocolexec "github.com/yttydcs/myflowhub-proto/protocol/exec"
)

var (
	execOnce   sync.Once
	execBroker *Broker[protocolexec.CallResp]
)

// SharedExecCallBroker 返回 exec 子协议在本进程内共享的 call_resp 投递器。
func SharedExecCallBroker() *Broker[protocolexec.CallResp] {
	execOnce.Do(func() {
		execBroker = New[protocolexec.CallResp]()
	})
	return execBroker
}
