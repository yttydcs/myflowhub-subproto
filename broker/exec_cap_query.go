package broker

// 本文件承载 SubProto 中 `broker` 模块里与 `exec_cap_query` 相关的逻辑。

import (
	"sync"

	protocolexec "github.com/yttydcs/myflowhub-proto/protocol/exec"
)

var (
	execCapQueryOnce   sync.Once
	execCapQueryBroker *Broker[protocolexec.CapQueryResp]
)

// SharedExecCapQueryBroker 返回 exec 子协议在本进程内共享的 cap_query_resp 投递器。
func SharedExecCapQueryBroker() *Broker[protocolexec.CapQueryResp] {
	execCapQueryOnce.Do(func() {
		execCapQueryBroker = New[protocolexec.CapQueryResp]()
	})
	return execCapQueryBroker
}
