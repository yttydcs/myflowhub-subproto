package broker

// 本文件承载 SubProto 中 `broker` 模块里与 `exec_call` 相关的逻辑。

import (
	"sync"

	protocolexec "github.com/yttydcs/myflowhub-proto/protocol/exec"
)

var (
	execOnce   sync.Once
	execBroker *Broker[protocolexec.CallResp]
)

// SharedExecCallBroker 返回 exec 子协议在本进程内共享的 call_resp 投递器。
// SharedExecCallBroker 复用单例，避免不同 handler 实例之间的 call_resp 匹配断裂。
func SharedExecCallBroker() *Broker[protocolexec.CallResp] {
	execOnce.Do(func() {
		execBroker = New[protocolexec.CallResp]()
	})
	return execBroker
}
