package auth

import protocol "github.com/yttydcs/myflowhub-proto/protocol/auth"

// 动作常量定义
const (
	actionRegister                 = protocol.ActionRegister
	actionAssistRegister           = protocol.ActionAssistRegister
	actionRegisterResp             = protocol.ActionRegisterResp
	actionAssistRegisterResp       = protocol.ActionAssistRegisterResp
	actionLogin                    = protocol.ActionLogin
	actionAssistLogin              = protocol.ActionAssistLogin
	actionLoginResp                = protocol.ActionLoginResp
	actionAssistLoginResp          = protocol.ActionAssistLoginResp
	actionRevoke                   = protocol.ActionRevoke
	actionRevokeResp               = protocol.ActionRevokeResp
	actionAssistQueryCred          = protocol.ActionAssistQueryCred
	actionAssistQueryCredResp      = protocol.ActionAssistQueryCredResp
	actionOffline                  = protocol.ActionOffline
	actionAssistOffline            = protocol.ActionAssistOffline
	actionGetPerms                 = protocol.ActionGetPerms
	actionGetPermsResp             = protocol.ActionGetPermsResp
	actionListRoles                = protocol.ActionListRoles
	actionListRolesResp            = protocol.ActionListRolesResp
	actionPermsInvalidate          = protocol.ActionPermsInvalidate
	actionPermsSnapshot            = protocol.ActionPermsSnapshot
	actionListPendingRegisters     = protocol.ActionListPendingRegisters
	actionListPendingRegistersResp = protocol.ActionListPendingRegistersResp
	actionApproveRegister          = protocol.ActionApproveRegister
	actionApproveRegisterResp      = protocol.ActionApproveRegisterResp
	actionRejectRegister           = protocol.ActionRejectRegister
	actionRejectRegisterResp       = protocol.ActionRejectRegisterResp
	actionIssueRegisterPermit      = protocol.ActionIssueRegisterPermit
	actionIssueRegisterPermitResp  = protocol.ActionIssueRegisterPermitResp
	actionRevokeRegisterPermit     = protocol.ActionRevokeRegisterPermit
	actionRevokeRegisterPermitResp = protocol.ActionRevokeRegisterPermitResp
	actionUpLogin                  = protocol.ActionUpLogin
	actionUpLoginResp              = protocol.ActionUpLoginResp
)

type message = protocol.Message
type registerData = protocol.RegisterData
type loginData = protocol.LoginData
type respData = protocol.RespData
type revokeData = protocol.RevokeData
type queryCredData = protocol.QueryCredData
type offlineData = protocol.OfflineData
type permsQueryData = protocol.PermsQueryData
type invalidateData = protocol.InvalidateData
type rolePermEntry = protocol.RolePermEntry
type listRolesReq = protocol.ListRolesReq
type pendingRegisterInfo = protocol.PendingRegisterInfo
type listPendingRegistersReq = protocol.ListPendingRegistersReq
type listPendingRegistersResp = protocol.ListPendingRegistersResp
type approveRegisterReq = protocol.ApproveRegisterReq
type approveRegisterResp = protocol.ApproveRegisterResp
type rejectRegisterReq = protocol.RejectRegisterReq
type rejectRegisterResp = protocol.RejectRegisterResp
type issueRegisterPermitReq = protocol.IssueRegisterPermitReq
type issueRegisterPermitResp = protocol.IssueRegisterPermitResp
type revokeRegisterPermitReq = protocol.RevokeRegisterPermitReq
type revokeRegisterPermitResp = protocol.RevokeRegisterPermitResp
type upLoginData = protocol.UpLoginData

type bindingRecord struct {
	NodeID uint32
	Role   string
	Perms  []string
	PubKey []byte
}
