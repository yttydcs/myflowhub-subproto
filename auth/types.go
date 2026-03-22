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
type revokeData = protocol.RevokeData
type queryCredData = protocol.QueryCredData
type offlineData = protocol.OfflineData
type permsQueryData = protocol.PermsQueryData
type invalidateData = protocol.InvalidateData
type rolePermEntry = protocol.RolePermEntry
type listRolesReq = protocol.ListRolesReq
type upLoginData = protocol.UpLoginData

// NOTE:
// `display_name` is being rolled out ahead of the auth Proto upgrade.
// Keep the local wire structs backward compatible so auth can accept and
// echo the field without changing the shared Proto package yet.
type registerData struct {
	DeviceID    string `json:"device_id"`
	NodeID      uint32 `json:"node_id,omitempty"`
	PubKey      string `json:"pubkey,omitempty"`
	NodePub     string `json:"node_pub,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	TS          int64  `json:"ts,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
}

type loginData struct {
	DeviceID    string `json:"device_id"`
	NodeID      uint32 `json:"node_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	TS          int64  `json:"ts,omitempty"`
	Nonce       string `json:"nonce,omitempty"`
	Sig         string `json:"sig,omitempty"`
	Alg         string `json:"alg,omitempty"`
}

type respData struct {
	Code        int      `json:"code"`
	Msg         string   `json:"msg,omitempty"`
	DeviceID    string   `json:"device_id,omitempty"`
	NodeID      uint32   `json:"node_id,omitempty"`
	HubID       uint32   `json:"hub_id,omitempty"`
	Role        string   `json:"role,omitempty"`
	Perms       []string `json:"perms,omitempty"`
	PubKey      string   `json:"pubkey,omitempty"`
	NodePub     string   `json:"node_pub,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	TS          int64    `json:"ts,omitempty"`
	Nonce       string   `json:"nonce,omitempty"`
}

type bindingRecord struct {
	NodeID uint32
	Role   string
	Perms  []string
	PubKey []byte
}
