package varstore

import (
	"context"

	core "github.com/yttydcs/myflowhub-core"
)

func chooseSetResp(assisted bool) string {
	if assisted {
		return varActionAssistSetResp
	}
	return varActionSetResp
}

func chooseGetResp(assisted bool) string {
	if assisted {
		return varActionAssistGetResp
	}
	return varActionGetResp
}

func chooseListResp(assisted bool) string {
	if assisted {
		return varActionAssistListResp
	}
	return varActionListResp
}

func chooseRevokeResp(assisted bool) string {
	if assisted {
		return varActionAssistRevokeResp
	}
	return varActionRevokeResp
}

func chooseSubscribeResp(assisted bool) string {
	if assisted {
		return varActionAssistSubscribeResp
	}
	return varActionSubscribeResp
}

func firstNonZero(a, b uint32) uint32 {
	if a != 0 {
		return a
	}
	return b
}

func validVarName(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if ch >= 'a' && ch <= 'z' {
			continue
		}
		if ch >= 'A' && ch <= 'Z' {
			continue
		}
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '_' {
			continue
		}
		return false
	}
	return true
}

func ownerConnID(ctx context.Context, srv core.IServer, owner uint32) string {
	if srv == nil || owner == 0 {
		return ""
	}
	if c, ok := srv.ConnManager().GetByNode(owner); ok {
		return c.ID()
	}
	return ""
}
