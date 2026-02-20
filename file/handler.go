package file

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/header"
	permission "github.com/yttydcs/myflowhub-core/kit/permission"
	"github.com/yttydcs/myflowhub-core/subproto"
)

const (
	permRead  = "file.read"
	permWrite = "file.write"
)

type Handler struct {
	subproto.BaseSubProcess
	log     *slog.Logger
	cfg     core.IConfig
	permCfg *permission.Config

	mu   sync.RWMutex
	recv map[[16]byte]*recvSession
	send map[[16]byte]*sendSession

	janitorRunning atomic.Bool
	lastJanitorSec atomic.Int64
}

func NewHandler(log *slog.Logger) *Handler {
	return NewHandlerWithConfig(nil, log)
}

func NewHandlerWithConfig(cfg core.IConfig, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.Default()
	}
	h := &Handler{
		log:  log,
		cfg:  cfg,
		recv: make(map[[16]byte]*recvSession),
		send: make(map[[16]byte]*sendSession),
	}
	if cfg != nil {
		h.permCfg = permission.SharedConfig(cfg)
	}
	if h.permCfg == nil {
		h.permCfg = permission.NewConfig(nil)
	}
	return h
}

func (h *Handler) SubProto() uint8 { return SubProtoFile }

func (h *Handler) Init() bool { return true }

func (h *Handler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if hdr == nil || len(payload) == 0 {
		return
	}
	switch payload[0] {
	case kindCtrl:
		h.handleCtrl(ctx, conn, hdr, payload)
		h.maybeJanitor(ctx)
	case kindData:
		h.handleData(ctx, conn, hdr, payload)
	case kindAck:
		h.handleAck(ctx, conn, hdr, payload)
	default:
		return
	}
}

func (h *Handler) handleCtrl(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if len(payload) < 2 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}

	var msg message
	if err := json.Unmarshal(payload[1:], &msg); err != nil {
		return
	}
	action := strings.ToLower(strings.TrimSpace(msg.Action))
	switch action {
	case actionRead:
		var req readReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendReadResp(ctx, hdr, hdr.SourceID(), readResp{Code: 400, Msg: "invalid read", Op: strings.TrimSpace(req.Op)})
			return
		}
		h.handleReadRequest(ctx, conn, hdr, payload, req)
	case actionWrite:
		var req writeReq
		if err := json.Unmarshal(msg.Data, &req); err != nil {
			h.sendWriteResp(ctx, hdr, hdr.SourceID(), writeResp{Code: 400, Msg: "invalid write", Op: strings.TrimSpace(req.Op)})
			return
		}
		h.handleWriteRequest(ctx, conn, hdr, payload, req)
	case actionReadResp:
		if hdr.TargetID() == srv.NodeID() {
			h.handleReadRespLocal(ctx, hdr, msg.Data)
			return
		}
		h.forwardCtrlByHeaderTarget(ctx, hdr, payload)
	case actionWriteResp:
		if hdr.TargetID() == srv.NodeID() {
			h.handleWriteRespLocal(ctx, hdr, msg.Data)
			return
		}
		h.forwardCtrlByHeaderTarget(ctx, hdr, payload)
	default:
		// 未知 action：仅做逐跳路由转发（target!=local 时）
		if hdr.TargetID() != srv.NodeID() {
			h.forwardCtrlByHeaderTarget(ctx, hdr, payload)
		}
	}
}

func (h *Handler) handleReadRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req readReq) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	op := strings.ToLower(strings.TrimSpace(req.Op))
	requester := hdr.SourceID()

	var target uint32
	switch op {
	case opPull:
		target = req.Target
		if target == 0 {
			h.sendReadResp(ctx, hdr, requester, readResp{Code: 400, Msg: "target required", Op: opPull})
			return
		}
	case opList:
		target = req.Target
		if target == 0 {
			target = srv.NodeID()
		}
	case opReadText:
		target = req.Target
		if target == 0 {
			h.sendReadResp(ctx, hdr, requester, readResp{Code: 400, Msg: "target required", Op: opReadText})
			return
		}
	default:
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 400, Msg: "invalid op", Op: op})
		return
	}

	h.routeCtrlRequest(ctx, conn, hdr, payload, requester, permRead, target,
		func(code int, msg string) {
			h.sendReadResp(ctx, hdr, requester, readResp{Code: code, Msg: msg, Op: op})
		},
		func(cfg handlerConfig) {
			switch op {
			case opPull:
				h.handlePullAsProvider(ctx, hdr, req, cfg)
			case opList:
				h.handleListLocal(ctx, hdr, req, cfg)
			case opReadText:
				h.handleReadTextLocal(ctx, hdr, req, cfg)
			}
		},
	)
}

func (h *Handler) handleWriteRequest(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte, req writeReq) {
	op := strings.ToLower(strings.TrimSpace(req.Op))
	requester := hdr.SourceID()
	if op != opOffer {
		h.sendWriteResp(ctx, hdr, requester, writeResp{Code: 400, Msg: "invalid op", Op: op})
		return
	}
	if req.Target == 0 {
		h.sendWriteResp(ctx, hdr, requester, writeResp{Code: 400, Msg: "target required", Op: opOffer})
		return
	}

	h.routeCtrlRequest(ctx, conn, hdr, payload, requester, permWrite, req.Target,
		func(code int, msg string) {
			h.sendWriteResp(ctx, hdr, requester, writeResp{Code: code, Msg: msg, Op: opOffer, SessionID: strings.TrimSpace(req.SessionID)})
		},
		func(cfg handlerConfig) {
			h.handleOfferAsConsumer(ctx, hdr, req, cfg)
		},
	)
}

func (h *Handler) routeCtrlRequest(
	ctx context.Context,
	conn core.IConnection,
	hdr core.IHeader,
	payload []byte,
	requester uint32,
	perm string,
	target uint32,
	sendErr func(code int, msg string),
	handleLocal func(cfg handlerConfig),
) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	local := srv.NodeID()
	cm := srv.ConnManager()
	if cm == nil {
		return
	}
	cfg := loadConfig(h.cfg)

	// 来自父节点：下游无条件信任父节点，不在此处做权限判定。
	if isParentConn(conn) {
		if target == local {
			handleLocal(cfg)
			return
		}
		h.forwardCtrlEnd(ctx, hdr, payload, target)
		return
	}

	// 目标为本节点：本节点即 LCA，执行权限判定与本地处理。
	if target == local {
		if !h.hasPermission(requester, perm) {
			sendErr(403, "permission denied")
			return
		}
		handleLocal(cfg)
		return
	}

	// 目标在本子树内？
	targetConn, ok := cm.GetByNode(target)
	if !ok || targetConn == nil {
		// 不在本子树：上送父节点（若无父则 not found）
		parent, _ := findParentConn(cm)
		if parent == nil {
			sendErr(404, "not found")
			return
		}
		fwdHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			sendErr(500, "hop limit exceeded")
			h.log.Warn("drop ctrl frame due to hop_limit", "target", target, "source", hdr.SourceID())
			return
		}
		fwdHdr.WithTargetID(target)
		h.sendToConn(ctx, parent, fwdHdr, payload)
		return
	}

	// 判定 requester 与 target 是否处于同一 child 分支；若是则将请求下送到该 child 继续判定（本节点非 LCA）。
	if requesterConn, ok2 := cm.GetByNode(requester); ok2 && requesterConn != nil && requesterConn.ID() == targetConn.ID() {
		nextNode := connNodeID(requesterConn)
		if nextNode == 0 {
			sendErr(500, "invalid route")
			return
		}
		fwdHdr, ok := header.CloneToTCPForForward(hdr)
		if !ok {
			sendErr(500, "hop limit exceeded")
			h.log.Warn("drop ctrl frame due to hop_limit", "target", nextNode, "source", hdr.SourceID())
			return
		}
		fwdHdr.WithTargetID(nextNode)
		h.sendToConn(ctx, requesterConn, fwdHdr, payload)
		return
	}

	// 本节点为 LCA：判定权限后，按端到端 target 转交。
	if !h.hasPermission(requester, perm) {
		sendErr(403, "permission denied")
		return
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		sendErr(500, "hop limit exceeded")
		h.log.Warn("drop ctrl frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return
	}
	fwdHdr.WithTargetID(target)
	h.sendToConn(ctx, targetConn, fwdHdr, payload)
}

func (h *Handler) hasPermission(nodeID uint32, perm string) bool {
	if h.permCfg == nil {
		return false
	}
	return h.permCfg.Has(nodeID, perm)
}

func (h *Handler) forwardCtrlByHeaderTarget(ctx context.Context, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	target := hdr.TargetID()
	if target == 0 {
		return
	}
	h.forwardCtrlEnd(ctx, hdr, payload, target)
}

func (h *Handler) forwardCtrlEnd(ctx context.Context, hdr core.IHeader, payload []byte, target uint32) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	cm := srv.ConnManager()
	if cm == nil {
		return
	}
	var next core.IConnection
	if c, ok := cm.GetByNode(target); ok && c != nil {
		next = c
	} else {
		parent, _ := findParentConn(cm)
		next = parent
	}
	if next == nil {
		return
	}
	fwdHdr, ok := header.CloneToTCPForForward(hdr)
	if !ok {
		h.log.Warn("drop ctrl frame due to hop_limit", "target", target, "source", hdr.SourceID())
		return
	}
	fwdHdr.WithTargetID(target)
	h.sendToConn(ctx, next, fwdHdr, payload)
}

func (h *Handler) sendToConn(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	if conn == nil || hdr == nil || len(payload) == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		_ = conn.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
		return
	}
	_ = srv.Send(ctx, conn.ID(), hdr, payload)
}

func (h *Handler) sendReadResp(ctx context.Context, reqHdr core.IHeader, target uint32, data readResp) {
	data.Op = strings.ToLower(strings.TrimSpace(data.Op))
	h.sendCtrlToNode(ctx, reqHdr, target, message{Action: actionReadResp, Data: mustJSON(data)})
}

func (h *Handler) sendWriteResp(ctx context.Context, reqHdr core.IHeader, target uint32, data writeResp) {
	data.Op = strings.ToLower(strings.TrimSpace(data.Op))
	h.sendCtrlToNode(ctx, reqHdr, target, message{Action: actionWriteResp, Data: mustJSON(data)})
}

func (h *Handler) sendCtrlToNode(ctx context.Context, reqHdr core.IHeader, target uint32, msg message) {
	if target == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	src := srv.NodeID()
	body, _ := json.Marshal(msg)
	payload := make([]byte, 1+len(body))
	payload[0] = kindCtrl
	copy(payload[1:], body)

	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorOKResp).
		WithSubProto(SubProtoFile).
		WithSourceID(src).
		WithTargetID(target)
	if reqHdr != nil {
		hdr = hdr.WithMsgID(reqHdr.GetMsgID()).WithTraceID(reqHdr.GetTraceID())
	}

	// 逐跳选择下一跳连接：先命中子树，否则上送父节点
	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	} else {
		parent, _ := findParentConn(srv.ConnManager())
		next = parent
	}
	if next == nil {
		return
	}
	h.sendToConn(ctx, next, hdr, payload)
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}

func isParentConn(c core.IConnection) bool {
	if c == nil {
		return false
	}
	if role, ok := c.GetMeta(core.MetaRoleKey); ok {
		if s, ok2 := role.(string); ok2 && s == core.RoleParent {
			return true
		}
	}
	return false
}

func connNodeID(conn core.IConnection) uint32 {
	if conn == nil {
		return 0
	}
	if v, ok := conn.GetMeta("nodeID"); ok {
		switch vv := v.(type) {
		case uint32:
			return vv
		case uint64:
			return uint32(vv)
		case int:
			if vv >= 0 {
				return uint32(vv)
			}
		case int64:
			if vv >= 0 {
				return uint32(vv)
			}
		case float64:
			if vv >= 0 {
				return uint32(vv)
			}
		}
	}
	return 0
}

func findParentConn(cm core.IConnectionManager) (core.IConnection, uint32) {
	if cm == nil {
		return nil, 0
	}
	var parent core.IConnection
	var parentNode uint32
	cm.Range(func(c core.IConnection) bool {
		if isParentConn(c) {
			parent = c
			parentNode = connNodeID(c)
			return false
		}
		return true
	})
	return parent, parentNode
}

func (h *Handler) handleListLocal(ctx context.Context, hdr core.IHeader, req readReq, cfg handlerConfig) {
	requester := uint32(0)
	if hdr != nil {
		requester = hdr.SourceID()
	}
	dir, err := sanitizeDir(req.Dir)
	if err != nil {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 400, Msg: "invalid dir", Op: opList, Dir: req.Dir})
		return
	}
	root := filepath.Join(cfg.BaseDir, filepath.FromSlash(dir))
	entries, err := os.ReadDir(root)
	if err != nil {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 404, Msg: "not found", Op: opList, Dir: dir, Files: []string{}, Dirs: []string{}})
		return
	}
	dirs := make([]string, 0, len(entries))
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if e == nil {
			continue
		}
		if e.IsDir() {
			dirs = append(dirs, e.Name())
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(dirs)
	sort.Strings(files)
	h.sendReadResp(ctx, hdr, requester, readResp{Code: 1, Msg: "ok", Op: opList, Dir: dir, Files: files, Dirs: dirs})
}

func (h *Handler) handleReadTextLocal(ctx context.Context, hdr core.IHeader, req readReq, cfg handlerConfig) {
	srv := core.ServerFromContext(ctx)
	if srv == nil || hdr == nil {
		return
	}
	local := srv.NodeID()
	requester := hdr.SourceID()
	provider := local
	if req.Target != 0 && req.Target != local {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 400, Msg: "target mismatch", Op: opReadText})
		return
	}
	dir := strings.TrimSpace(req.Dir)
	name := strings.TrimSpace(req.Name)
	finalPath, _, err := resolvePaths(cfg.BaseDir, dir, name)
	if err != nil {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 400, Msg: "invalid path", Op: opReadText, Provider: provider, Consumer: requester, Dir: dir, Name: name})
		return
	}
	info, err := os.Stat(finalPath)
	if err != nil || info == nil || info.IsDir() {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 404, Msg: "not found", Op: opReadText, Provider: provider, Consumer: requester, Dir: dir, Name: name})
		return
	}
	if cfg.MaxSizeBytes > 0 && uint64(info.Size()) > cfg.MaxSizeBytes {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 413, Msg: "too large", Op: opReadText, Provider: provider, Consumer: requester, Dir: dir, Name: name, Size: uint64(info.Size())})
		return
	}

	maxBytes := uint32(req.MaxBytes)
	if maxBytes == 0 {
		maxBytes = 64 * 1024
	}
	if maxBytes > 256*1024 {
		maxBytes = 256 * 1024
	}

	f, err := os.Open(finalPath)
	if err != nil {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 500, Msg: "open failed", Op: opReadText})
		return
	}
	defer func() { _ = f.Close() }()

	buf := make([]byte, maxBytes)
	n, rerr := io.ReadFull(f, buf)
	if rerr == io.ErrUnexpectedEOF || rerr == io.EOF {
		// ok
	} else if rerr != nil {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 500, Msg: "read failed", Op: opReadText})
		return
	}
	buf = buf[:n]
	truncated := uint64(n) < uint64(info.Size())
	if !utf8.Valid(buf) {
		h.sendReadResp(ctx, hdr, requester, readResp{Code: 415, Msg: "not text", Op: opReadText, Provider: provider, Consumer: requester, Dir: dir, Name: name})
		return
	}
	h.sendReadResp(ctx, hdr, requester, readResp{
		Code:      1,
		Msg:       "ok",
		Op:        opReadText,
		Provider:  provider,
		Consumer:  requester,
		Dir:       dir,
		Name:      name,
		Size:      uint64(info.Size()),
		Text:      string(buf),
		Truncated: truncated,
	})
}

func (h *Handler) handlePullAsProvider(ctx context.Context, hdr core.IHeader, req readReq, cfg handlerConfig) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	consumer := uint32(0)
	if hdr != nil {
		consumer = hdr.SourceID()
	}
	provider := srv.NodeID()

	finalPath, _, err := resolvePaths(cfg.BaseDir, req.Dir, req.Name)
	if err != nil {
		h.sendReadResp(ctx, hdr, consumer, readResp{Code: 400, Msg: "invalid path", Op: opPull, Provider: provider, Consumer: consumer, Dir: req.Dir, Name: req.Name})
		return
	}
	info, err := os.Stat(finalPath)
	if err != nil || info.IsDir() {
		h.sendReadResp(ctx, hdr, consumer, readResp{Code: 404, Msg: "not found", Op: opPull, Provider: provider, Consumer: consumer, Dir: req.Dir, Name: req.Name})
		return
	}
	size := uint64(info.Size())
	if cfg.MaxSizeBytes > 0 && size > cfg.MaxSizeBytes {
		h.sendReadResp(ctx, hdr, consumer, readResp{Code: 413, Msg: "too large", Op: opPull, Provider: provider, Consumer: consumer, Dir: req.Dir, Name: req.Name, Size: size})
		return
	}
	if h.totalSessions() >= cfg.MaxConcurrent {
		h.sendReadResp(ctx, hdr, consumer, readResp{Code: 429, Msg: "too many sessions", Op: opPull, Provider: provider, Consumer: consumer, Dir: req.Dir, Name: req.Name, Size: size})
		return
	}

	startFrom := req.ResumeFrom
	if startFrom > size {
		startFrom = 0
	}
	sid, err := newUUID()
	if err != nil {
		h.sendReadResp(ctx, hdr, consumer, readResp{Code: 500, Msg: "uuid failed", Op: opPull})
		return
	}

	resp := readResp{
		Code:      1,
		Msg:       "ok",
		Op:        opPull,
		SessionID: uuidToString(sid),
		Provider:  provider,
		Consumer:  consumer,
		Dir:       strings.TrimSpace(req.Dir),
		Name:      strings.TrimSpace(req.Name),
		Size:      size,
		StartFrom: startFrom,
		Chunk:     uint32(cfg.ChunkBytes),
	}
	h.sendReadResp(ctx, hdr, consumer, resp)

	// 发送端会话：异步发送 DATA
	if startFrom >= size {
		return
	}
	h.addSendSession(sid, &sendSession{
		id:         sid,
		provider:   provider,
		consumer:   consumer,
		dir:        strings.TrimSpace(req.Dir),
		name:       strings.TrimSpace(req.Name),
		filePath:   finalPath,
		size:       size,
		startFrom:  startFrom,
		lastActive: time.Now(),
	})
	go h.sendFileData(ctx, sid)
}

func (h *Handler) handleReadRespLocal(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp readResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.Code != 1 {
		return
	}
	op := strings.ToLower(strings.TrimSpace(resp.Op))
	if op != opPull {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	local := srv.NodeID()
	if resp.Consumer != 0 && resp.Consumer != local {
		return
	}
	sid, ok := parseUUID(resp.SessionID)
	if !ok {
		return
	}
	provider := resp.Provider
	if provider == 0 && hdr != nil {
		provider = hdr.SourceID()
	}
	if provider == 0 {
		return
	}

	cfg := loadConfig(h.cfg)
	if cfg.MaxSizeBytes > 0 && resp.Size > cfg.MaxSizeBytes {
		return
	}
	if h.totalSessions() >= cfg.MaxConcurrent {
		return
	}
	overwrite := true
	finalPath, partPath, err := resolvePaths(cfg.BaseDir, resp.Dir, resp.Name)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		return
	}
	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return
	}
	startFrom := resp.StartFrom
	if startFrom == resp.Size && resp.Size != 0 {
		_ = f.Sync()
		_ = f.Close()
		if h.verifyPart(partPath, resp.Size, strings.TrimSpace(resp.Sha256)) {
			if overwrite {
				_ = os.Remove(finalPath)
			}
			_ = os.Rename(partPath, finalPath)
		}
		return
	}
	if err := f.Truncate(int64(startFrom)); err != nil {
		_ = f.Close()
		return
	}
	if _, err := f.Seek(int64(startFrom), io.SeekStart); err != nil {
		_ = f.Close()
		return
	}

	h.addRecvSession(sid, &recvSession{
		id:              sid,
		provider:        provider,
		consumer:        local,
		dir:             strings.TrimSpace(resp.Dir),
		name:            strings.TrimSpace(resp.Name),
		finalPath:       finalPath,
		partPath:        partPath,
		size:            resp.Size,
		sha256Hex:       strings.TrimSpace(resp.Sha256),
		overwrite:       overwrite,
		file:            f,
		expectedOffset:  startFrom,
		pending:         make(map[uint64][]byte),
		maxPendingBytes: uint64(cfg.ChunkBytes) * 8,
		lastActive:      time.Now(),
	})
}

func (h *Handler) handleOfferAsConsumer(ctx context.Context, hdr core.IHeader, req writeReq, cfg handlerConfig) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	local := srv.NodeID()
	provider := uint32(0)
	if hdr != nil {
		provider = hdr.SourceID()
	}
	consumer := local
	if req.Target != 0 && req.Target != local {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 400, Msg: "target mismatch", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID)})
		return
	}
	if cfg.MaxSizeBytes > 0 && req.Size > cfg.MaxSizeBytes {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 413, Msg: "too large", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}
	sid, ok := parseUUID(req.SessionID)
	if !ok {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 400, Msg: "invalid session", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}

	overwrite := true
	if req.Overwrite != nil {
		overwrite = *req.Overwrite
	}
	finalPath, partPath, err := resolvePaths(cfg.BaseDir, req.Dir, req.Name)
	if err != nil {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 400, Msg: "invalid path", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o755); err != nil {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 500, Msg: "mkdir failed", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}
	if !overwrite {
		if _, err := os.Stat(finalPath); err == nil {
			h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 409, Msg: "exists", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
			return
		}
	}

	if h.totalSessions() >= cfg.MaxConcurrent {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 429, Msg: "too many sessions", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}

	resumeFrom := uint64(0)
	if st, err := os.Stat(partPath); err == nil && !st.IsDir() {
		if uint64(st.Size()) <= req.Size {
			resumeFrom = uint64(st.Size())
		} else {
			_ = os.Remove(partPath)
		}
	}
	if resumeFrom == req.Size && req.Size != 0 {
		if h.verifyPart(partPath, req.Size, strings.TrimSpace(req.Sha256)) {
			if overwrite {
				_ = os.Remove(finalPath)
			}
			_ = os.Rename(partPath, finalPath)
		} else {
			_ = os.Remove(partPath)
		}
		h.sendWriteResp(ctx, hdr, provider, writeResp{
			Code:       1,
			Msg:        "ok",
			Op:         opOffer,
			SessionID:  strings.TrimSpace(req.SessionID),
			Provider:   provider,
			Consumer:   consumer,
			Dir:        strings.TrimSpace(req.Dir),
			Name:       strings.TrimSpace(req.Name),
			Size:       req.Size,
			Sha256:     strings.TrimSpace(req.Sha256),
			Accept:     true,
			ResumeFrom: resumeFrom,
		})
		return
	}

	f, err := os.OpenFile(partPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 500, Msg: "open failed", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}
	if err := f.Truncate(int64(resumeFrom)); err != nil {
		_ = f.Close()
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 500, Msg: "truncate failed", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}
	if _, err := f.Seek(int64(resumeFrom), io.SeekStart); err != nil {
		_ = f.Close()
		h.sendWriteResp(ctx, hdr, provider, writeResp{Code: 500, Msg: "seek failed", Op: opOffer, SessionID: strings.TrimSpace(req.SessionID), Accept: false})
		return
	}

	h.addRecvSession(sid, &recvSession{
		id:              sid,
		provider:        provider,
		consumer:        consumer,
		dir:             strings.TrimSpace(req.Dir),
		name:            strings.TrimSpace(req.Name),
		finalPath:       finalPath,
		partPath:        partPath,
		size:            req.Size,
		sha256Hex:       strings.TrimSpace(req.Sha256),
		overwrite:       overwrite,
		file:            f,
		expectedOffset:  resumeFrom,
		pending:         make(map[uint64][]byte),
		maxPendingBytes: uint64(cfg.ChunkBytes) * 8,
		lastActive:      time.Now(),
	})

	h.sendWriteResp(ctx, hdr, provider, writeResp{
		Code:       1,
		Msg:        "ok",
		Op:         opOffer,
		SessionID:  strings.TrimSpace(req.SessionID),
		Provider:   provider,
		Consumer:   consumer,
		Dir:        strings.TrimSpace(req.Dir),
		Name:       strings.TrimSpace(req.Name),
		Size:       req.Size,
		Sha256:     strings.TrimSpace(req.Sha256),
		Accept:     true,
		ResumeFrom: resumeFrom,
	})
}

func (h *Handler) handleWriteRespLocal(ctx context.Context, hdr core.IHeader, data json.RawMessage) {
	var resp writeResp
	if err := json.Unmarshal(data, &resp); err != nil {
		return
	}
	if resp.Code != 1 || !resp.Accept {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	local := srv.NodeID()
	consumer := uint32(0)
	if hdr != nil {
		consumer = hdr.SourceID()
	}
	provider := local
	if resp.Provider != 0 && resp.Provider != provider {
		return
	}
	sid, ok := parseUUID(resp.SessionID)
	if !ok {
		return
	}
	dir := strings.TrimSpace(resp.Dir)
	name := strings.TrimSpace(resp.Name)
	if name == "" || resp.Size == 0 {
		return
	}
	cfg := loadConfig(h.cfg)
	if h.totalSessions() >= cfg.MaxConcurrent {
		return
	}
	filePath, _, err := resolvePaths(cfg.BaseDir, dir, name)
	if err != nil {
		return
	}
	// 发送端会话：异步发送 DATA
	h.addSendSession(sid, &sendSession{
		id:         sid,
		provider:   provider,
		consumer:   consumer,
		dir:        dir,
		name:       name,
		filePath:   filePath,
		size:       resp.Size,
		sha256Hex:  strings.TrimSpace(resp.Sha256),
		startFrom:  resp.ResumeFrom,
		lastActive: time.Now(),
	})
	go h.sendFileData(ctx, sid)
}

func (h *Handler) handleData(ctx context.Context, _ core.IConnection, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	kind, bh, body, ok := decodeBinHeaderV1(payload)
	if !ok || kind != kindData || bh.Ver != binVerV1 {
		return
	}
	sess := h.getRecvSession(bh.SessionID)
	if sess == nil {
		return
	}
	if hdr.TargetID() != sess.consumer || hdr.SourceID() != sess.provider {
		return
	}
	if uint64(len(body)) == 0 && (bh.Flags&binFlagFIN) == 0 {
		return
	}
	offset := bh.Offset
	if offset > sess.size {
		return
	}
	if offset+uint64(len(body)) > sess.size {
		return
	}

	now := time.Now()
	expected, completed, wrote := h.applyRecvChunk(sess, offset, body, (bh.Flags&binFlagFIN) != 0, now)
	if !wrote {
		return
	}
	h.maybeSendAck(ctx, sess, expected, now)
	if completed {
		h.finishRecvSession(ctx, bh.SessionID)
	}
}

func (h *Handler) applyRecvChunk(s *recvSession, offset uint64, data []byte, fin bool, now time.Time) (expected uint64, completed bool, wrote bool) {
	if s == nil {
		return 0, false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return s.expectedOffset, false, false
	}
	s.lastActive = now
	if fin {
		s.finSeen = true
	}

	if offset < s.expectedOffset {
		return s.expectedOffset, false, false
	}
	if offset > s.expectedOffset {
		if s.pendingBytes+uint64(len(data)) > s.maxPendingBytes {
			return s.expectedOffset, false, false
		}
		if s.pending == nil {
			s.pending = make(map[uint64][]byte)
		}
		if _, exists := s.pending[offset]; exists {
			return s.expectedOffset, false, false
		}
		cp := make([]byte, len(data))
		copy(cp, data)
		s.pending[offset] = cp
		s.pendingBytes += uint64(len(cp))
		return s.expectedOffset, false, false
	}

	// offset == expected: 顺序写入，并冲刷 pending
	if len(data) > 0 {
		if _, err := s.file.Write(data); err != nil {
			return s.expectedOffset, false, false
		}
		s.expectedOffset += uint64(len(data))
		wrote = true
	}
	for {
		next, ok := s.pending[s.expectedOffset]
		if !ok {
			break
		}
		delete(s.pending, s.expectedOffset)
		s.pendingBytes -= uint64(len(next))
		if len(next) > 0 {
			if _, err := s.file.Write(next); err != nil {
				break
			}
			s.expectedOffset += uint64(len(next))
			wrote = true
		}
	}
	expected = s.expectedOffset
	completed = s.finSeen && s.expectedOffset == s.size
	return expected, completed, wrote
}

func (h *Handler) maybeSendAck(ctx context.Context, s *recvSession, expected uint64, now time.Time) {
	if s == nil {
		return
	}
	cfg := loadConfig(h.cfg)
	step := uint64(cfg.ChunkBytes) * 4
	if step == 0 {
		step = 256 * 1024
	}

	s.mu.Lock()
	should := expected == s.size || expected-s.lastAckOffset >= step || now.Sub(s.lastAckTime) >= 500*time.Millisecond
	if !should {
		s.mu.Unlock()
		return
	}
	s.lastAckOffset = expected
	s.lastAckTime = now
	provider := s.provider
	consumer := s.consumer
	sid := s.id
	s.mu.Unlock()

	if provider == 0 || consumer == 0 {
		return
	}
	payload := encodeBinHeaderV1(kindAck, sid, expected, false, nil)
	hdr := (&header.HeaderTcp{}).
		WithMajor(header.MajorMsg).
		WithSubProto(SubProtoFile).
		WithSourceID(consumer).
		WithTargetID(provider)
	h.sendToNode(ctx, provider, hdr, payload)
}

func (h *Handler) sendToNode(ctx context.Context, target uint32, hdr core.IHeader, payload []byte) {
	if target == 0 || hdr == nil || len(payload) == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil || srv.ConnManager() == nil {
		return
	}
	var next core.IConnection
	if c, ok := srv.ConnManager().GetByNode(target); ok && c != nil {
		next = c
	} else {
		parent, _ := findParentConn(srv.ConnManager())
		next = parent
	}
	if next == nil {
		return
	}
	h.sendToConn(ctx, next, hdr, payload)
}

func (h *Handler) finishRecvSession(ctx context.Context, sid [16]byte) {
	sess := h.getRecvSession(sid)
	if sess == nil {
		return
	}
	sess.mu.Lock()
	f := sess.file
	part := sess.partPath
	final := sess.finalPath
	wantSize := sess.size
	wantSha := strings.TrimSpace(sess.sha256Hex)
	overwrite := sess.overwrite
	sess.file = nil
	sess.mu.Unlock()

	if f != nil {
		_ = f.Sync()
		_ = f.Close()
	}

	ok := h.verifyPart(part, wantSize, wantSha)
	if !ok {
		h.removeRecvSession(sid)
		return
	}

	if overwrite {
		_ = os.Remove(final)
	}
	if err := os.Rename(part, final); err != nil {
		h.removeRecvSession(sid)
		return
	}
	h.removeRecvSession(sid)
}

func (h *Handler) verifyPart(path string, size uint64, shaHex string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	if uint64(st.Size()) != size {
		return false
	}
	if shaHex == "" {
		return true
	}
	sum, err := sha256File(path)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(shaHex), sum)
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (h *Handler) handleAck(ctx context.Context, _ core.IConnection, hdr core.IHeader, payload []byte) {
	if hdr == nil {
		return
	}
	kind, bh, _, ok := decodeBinHeaderV1(payload)
	if !ok || kind != kindAck || bh.Ver != binVerV1 {
		return
	}
	sess := h.getSendSession(bh.SessionID)
	if sess == nil {
		return
	}
	if hdr.TargetID() != sess.provider || hdr.SourceID() != sess.consumer {
		return
	}
	sess.mu.Lock()
	if bh.Offset > sess.ackedUntil {
		sess.ackedUntil = bh.Offset
	}
	sess.lastActive = time.Now()
	sess.mu.Unlock()
}

func (h *Handler) sendFileData(ctx context.Context, sid [16]byte) {
	sess := h.getSendSession(sid)
	if sess == nil {
		return
	}
	cfg := loadConfig(h.cfg)
	chunkBytes := cfg.ChunkBytes
	if chunkBytes <= 0 {
		chunkBytes = 256 * 1024
	}

	sess.mu.Lock()
	path := sess.filePath
	provider := sess.provider
	consumer := sess.consumer
	startFrom := sess.startFrom
	wantSize := sess.size
	sess.mu.Unlock()

	if path == "" || provider == 0 || consumer == 0 {
		return
	}
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		return
	}
	totalSize := uint64(st.Size())
	if wantSize != 0 && totalSize != wantSize {
		return
	}
	if startFrom > totalSize {
		startFrom = 0
	}
	if _, err := f.Seek(int64(startFrom), io.SeekStart); err != nil {
		return
	}
	offset := startFrom
	buf := make([]byte, chunkBytes)
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			data := buf[:n]
			fin := offset+uint64(n) == totalSize
			payload := encodeBinHeaderV1(kindData, sid, offset, fin, data)
			hdr := (&header.HeaderTcp{}).
				WithMajor(header.MajorMsg).
				WithSubProto(SubProtoFile).
				WithSourceID(provider).
				WithTargetID(consumer)
			h.sendToNode(ctx, consumer, hdr, payload)
			offset += uint64(n)
			if fin {
				break
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			break
		}
	}
	h.removeSendSession(sid)
}

func (h *Handler) totalSessions() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.recv) + len(h.send)
}

func (h *Handler) addRecvSession(id [16]byte, s *recvSession) {
	if s == nil {
		return
	}
	h.mu.Lock()
	h.recv[id] = s
	h.mu.Unlock()
}

func (h *Handler) getRecvSession(id [16]byte) *recvSession {
	h.mu.RLock()
	s := h.recv[id]
	h.mu.RUnlock()
	return s
}

func (h *Handler) removeRecvSession(id [16]byte) {
	h.mu.Lock()
	if s := h.recv[id]; s != nil {
		delete(h.recv, id)
	}
	h.mu.Unlock()
}

func (h *Handler) addSendSession(id [16]byte, s *sendSession) {
	if s == nil {
		return
	}
	h.mu.Lock()
	h.send[id] = s
	h.mu.Unlock()
}

func (h *Handler) getSendSession(id [16]byte) *sendSession {
	h.mu.RLock()
	s := h.send[id]
	h.mu.RUnlock()
	return s
}

func (h *Handler) removeSendSession(id [16]byte) {
	h.mu.Lock()
	if s := h.send[id]; s != nil {
		delete(h.send, id)
	}
	h.mu.Unlock()
}

func (h *Handler) maybeJanitor(ctx context.Context) {
	cfg := loadConfig(h.cfg)
	if cfg.IncompleteTTLSec <= 0 {
		return
	}
	interval := cfg.IncompleteTTLSec / 2
	if interval < 300 {
		interval = 300
	}
	now := time.Now().Unix()
	last := h.lastJanitorSec.Load()
	if last != 0 && now-last < interval {
		return
	}
	if !h.lastJanitorSec.CompareAndSwap(last, now) {
		return
	}
	if !h.janitorRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer h.janitorRunning.Store(false)
		h.cleanupExpiredSessions(cfg)
		h.cleanupExpiredParts(cfg)
	}()
}

func (h *Handler) cleanupExpiredSessions(cfg handlerConfig) {
	if cfg.IncompleteTTLSec <= 0 {
		return
	}
	deadline := time.Now().Add(-time.Duration(cfg.IncompleteTTLSec) * time.Second)
	var expired [][16]byte

	h.mu.RLock()
	for id, s := range h.recv {
		if s == nil {
			continue
		}
		s.mu.Lock()
		last := s.lastActive
		if !last.IsZero() && last.Before(deadline) {
			f := s.file
			part := s.partPath
			s.file = nil
			s.mu.Unlock()
			if f != nil {
				_ = f.Close()
			}
			if part != "" {
				_ = os.Remove(part)
			}
			expired = append(expired, id)
			continue
		}
		s.mu.Unlock()
	}
	h.mu.RUnlock()

	if len(expired) == 0 {
		return
	}
	h.mu.Lock()
	for _, id := range expired {
		delete(h.recv, id)
	}
	h.mu.Unlock()
}

func (h *Handler) cleanupExpiredParts(cfg handlerConfig) {
	if cfg.IncompleteTTLSec <= 0 || strings.TrimSpace(cfg.BaseDir) == "" {
		return
	}
	deadline := time.Now().Add(-time.Duration(cfg.IncompleteTTLSec) * time.Second)
	root := strings.TrimSpace(cfg.BaseDir)
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), ".part") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.ModTime().Before(deadline) {
			_ = os.Remove(path)
		}
		return nil
	})
}
