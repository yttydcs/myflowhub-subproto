package auth

import protocol "github.com/yttydcs/myflowhub-proto/protocol/auth"

// 动作常量定义
const (
	actionRegister            = protocol.ActionRegister
	actionAssistRegister      = protocol.ActionAssistRegister
	actionRegisterResp        = protocol.ActionRegisterResp
	actionAssistRegisterResp  = protocol.ActionAssistRegisterResp
	actionLogin               = protocol.ActionLogin
	actionAssistLogin         = protocol.ActionAssistLogin
	actionLoginResp           = protocol.ActionLoginResp
	actionAssistLoginResp     = protocol.ActionAssistLoginResp
	actionRevoke              = protocol.ActionRevoke
	actionRevokeResp          = protocol.ActionRevokeResp
	actionAssistQueryCred     = protocol.ActionAssistQueryCred
	actionAssistQueryCredResp = protocol.ActionAssistQueryCredResp
	actionOffline             = protocol.ActionOffline
	actionAssistOffline       = protocol.ActionAssistOffline
	actionGetPerms            = protocol.ActionGetPerms
	actionGetPermsResp        = protocol.ActionGetPermsResp
	actionListRoles           = protocol.ActionListRoles
	actionListRolesResp       = protocol.ActionListRolesResp
	actionPermsInvalidate     = protocol.ActionPermsInvalidate
	actionPermsSnapshot       = protocol.ActionPermsSnapshot
	actionUpLogin             = protocol.ActionUpLogin
	actionUpLoginResp         = protocol.ActionUpLoginResp
)

type message = protocol.Message
type registerData = protocol.RegisterData
type loginData = protocol.LoginData
type revokeData = protocol.RevokeData
type queryCredData = protocol.QueryCredData
type offlineData = protocol.OfflineData
type respData = protocol.RespData
type permsQueryData = protocol.PermsQueryData
type invalidateData = protocol.InvalidateData
type rolePermEntry = protocol.RolePermEntry
type listRolesReq = protocol.ListRolesReq
type upLoginData = protocol.UpLoginData

type bindingRecord struct {
	NodeID uint32
	Role   string
	Perms  []string
	PubKey []byte
}
