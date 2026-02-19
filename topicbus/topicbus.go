package topicbus

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"strings"
	"sync"

	core "github.com/yttydcs/myflowhub-core"
	"github.com/yttydcs/myflowhub-core/eventbus"
	"github.com/yttydcs/myflowhub-core/header"
	"github.com/yttydcs/myflowhub-core/subproto"
)

// TopicBusHandler 提供 topic 的订阅/退订与逐级转发 publish。
//
// 设计要点：
// - 仅存内存订阅关系（不持久化）；连接断开即清理。
// - 每个节点只存“直连连接订阅了什么 topic”；逐级向上汇总，逐级向下转发。
// - publish 不回显给发送连接。
type TopicBusHandler struct {
	subproto.ActionBaseSubProcess
	log *slog.Logger

	mu sync.RWMutex
	// topic -> connID set
	topicSubs map[string]map[string]struct{}
	// connID -> topic set
	connSubs map[string]map[string]struct{}
	// topic -> whether this node has subscribed upstream (best effort)
	upstreamActive map[string]bool

	// 用于检测父连接变化并触发全量重订阅（仅在父连接已具备 nodeID 时）。
	parentConnID   string
	parentConnNode uint32

	eventSubOnce sync.Once
}

func NewTopicBusHandler(log *slog.Logger) *TopicBusHandler {
	return NewTopicBusHandlerWithConfig(nil, log)
}

func NewTopicBusHandlerWithConfig(_ core.IConfig, log *slog.Logger) *TopicBusHandler {
	if log == nil {
		log = slog.Default()
	}
	return &TopicBusHandler{
		log:            log,
		topicSubs:      make(map[string]map[string]struct{}),
		connSubs:       make(map[string]map[string]struct{}),
		upstreamActive: make(map[string]bool),
	}
}

func (h *TopicBusHandler) SubProto() uint8 { return SubProtoTopicBus }

func (h *TopicBusHandler) Init() bool {
	h.initActions()
	return true
}

func (h *TopicBusHandler) initActions() {
	h.ResetActions()
	for _, act := range registerActions(h) {
		h.RegisterAction(act)
	}
}

func (h *TopicBusHandler) OnReceive(ctx context.Context, conn core.IConnection, hdr core.IHeader, payload []byte) {
	h.ensureConnCloseSubscription(ctx)
	h.maybeResubscribeUpstream(ctx)

	var msg message
	if err := json.Unmarshal(payload, &msg); err != nil {
		h.log.Warn("topicbus invalid payload", "err", err)
		return
	}
	entry, ok := h.LookupAction(msg.Action)
	if !ok {
		h.log.Debug("unknown topicbus action", "action", msg.Action)
		return
	}
	entry.Handle(ctx, conn, hdr, msg.Data)
}

func (h *TopicBusHandler) handleSubscribe(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req subscribeReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendSimpleResp(ctx, conn, hdr, actionSubscribeResp, resp{Code: 400, Msg: "invalid subscribe"})
		return
	}
	needUp := h.addSubscriptions(conn, []string{req.Topic})
	h.sendSimpleResp(ctx, conn, hdr, actionSubscribeResp, resp{Code: 1, Msg: "ok", Topic: req.Topic})
	h.maybeSubscribeUpstream(ctx, needUp)
}

func (h *TopicBusHandler) handleSubscribeBatch(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req subscribeBatchReq
	_ = json.Unmarshal(data, &req)
	needUp := h.addSubscriptions(conn, req.Topics)
	h.sendSimpleResp(ctx, conn, hdr, actionSubscribeBatchResp, resp{Code: 1, Msg: "ok", Topics: uniqueStable(req.Topics)})
	h.maybeSubscribeUpstream(ctx, needUp)
}

func (h *TopicBusHandler) handleUnsubscribe(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req subscribeReq
	if err := json.Unmarshal(data, &req); err != nil {
		h.sendSimpleResp(ctx, conn, hdr, actionUnsubscribeResp, resp{Code: 1, Msg: "ok", Topic: ""})
		return
	}
	needUpUnsub := h.removeSubscriptions(conn, []string{req.Topic})
	h.sendSimpleResp(ctx, conn, hdr, actionUnsubscribeResp, resp{Code: 1, Msg: "ok", Topic: req.Topic})
	h.maybeUnsubscribeUpstream(ctx, needUpUnsub)
}

func (h *TopicBusHandler) handleUnsubscribeBatch(ctx context.Context, conn core.IConnection, hdr core.IHeader, data json.RawMessage) {
	var req subscribeBatchReq
	_ = json.Unmarshal(data, &req)
	needUpUnsub := h.removeSubscriptions(conn, req.Topics)
	h.sendSimpleResp(ctx, conn, hdr, actionUnsubscribeBatchResp, resp{Code: 1, Msg: "ok", Topics: uniqueStable(req.Topics)})
	h.maybeUnsubscribeUpstream(ctx, needUpUnsub)
}

func (h *TopicBusHandler) handleListSubs(ctx context.Context, conn core.IConnection, hdr core.IHeader, _ json.RawMessage) {
	topics := h.listConnTopics(conn)
	h.sendSimpleResp(ctx, conn, hdr, actionListSubsResp, listResp{Code: 1, Topics: topics})
}

func (h *TopicBusHandler) handlePublish(ctx context.Context, conn core.IConnection, _ core.IHeader, data json.RawMessage) {
	var req publishReq
	if err := json.Unmarshal(data, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		return
	}

	// 先本层向下转发，再上送父节点。
	payload, _ := json.Marshal(message{Action: actionPublish, Data: data})
	h.broadcastToSubscribers(ctx, conn, req.Topic, payload)
	h.forwardPublishUpstream(ctx, conn, payload)
}

func (h *TopicBusHandler) sendSimpleResp(ctx context.Context, conn core.IConnection, reqHdr core.IHeader, action string, data any) {
	if conn == nil {
		return
	}
	raw, _ := json.Marshal(data)
	payload, _ := json.Marshal(message{Action: action, Data: raw})
	hdr := h.buildRespHeader(ctx, reqHdr)
	if srv := core.ServerFromContext(ctx); srv != nil {
		_ = srv.Send(ctx, conn.ID(), hdr, payload)
		return
	}
	_ = conn.SendWithHeader(hdr, payload, header.HeaderTcpCodec{})
}

func (h *TopicBusHandler) buildRespHeader(ctx context.Context, reqHdr core.IHeader) core.IHeader {
	base := &header.HeaderTcp{}
	if reqHdr != nil {
		if cloned := header.CloneToTCP(reqHdr); cloned != nil {
			base = cloned
		}
	}
	src := uint32(0)
	if srv := core.ServerFromContext(ctx); srv != nil {
		src = srv.NodeID()
	}
	tgt := uint32(0)
	if reqHdr != nil && reqHdr.SourceID() != 0 {
		tgt = reqHdr.SourceID()
	}
	return base.WithMajor(header.MajorOKResp).WithSubProto(SubProtoTopicBus).WithSourceID(src).WithTargetID(tgt)
}

// addSubscriptions 将 topics 加入 conn 的订阅；返回需要向上订阅的 topics（本节点该 topic 从 0->1）。
func (h *TopicBusHandler) addSubscriptions(conn core.IConnection, topics []string) []string {
	if conn == nil || len(topics) == 0 {
		return nil
	}
	connID := conn.ID()
	needUp := make([]string, 0)

	h.mu.Lock()
	connSet, ok := h.connSubs[connID]
	if !ok {
		connSet = make(map[string]struct{})
		h.connSubs[connID] = connSet
	}
	for _, t := range topics {
		// topic 无约束，不做 Trim/校验，原样存储。
		if _, exists := connSet[t]; exists {
			continue
		}
		connSet[t] = struct{}{}

		subs, ok := h.topicSubs[t]
		if !ok {
			subs = make(map[string]struct{})
			h.topicSubs[t] = subs
		}
		wasEmpty := len(subs) == 0
		subs[connID] = struct{}{}
		if wasEmpty && !h.upstreamActive[t] {
			h.upstreamActive[t] = true // 先标记，避免并发重复上送；失败会回滚
			needUp = append(needUp, t)
		}
	}
	h.mu.Unlock()
	return needUp
}

// removeSubscriptions 从 conn 的订阅移除 topics；返回需要向上退订的 topics（本节点该 topic 从 1->0）。
func (h *TopicBusHandler) removeSubscriptions(conn core.IConnection, topics []string) []string {
	if conn == nil || len(topics) == 0 {
		return nil
	}
	connID := conn.ID()
	needUpUnsub := make([]string, 0)

	h.mu.Lock()
	connSet, ok := h.connSubs[connID]
	if !ok {
		h.mu.Unlock()
		return nil
	}
	for _, t := range topics {
		if _, exists := connSet[t]; !exists {
			continue
		}
		delete(connSet, t)

		if subs, ok := h.topicSubs[t]; ok {
			delete(subs, connID)
			if len(subs) == 0 {
				delete(h.topicSubs, t)
				if h.upstreamActive[t] {
					h.upstreamActive[t] = false // 先标记，避免并发重复退订
					needUpUnsub = append(needUpUnsub, t)
				}
			}
		}
	}
	if len(connSet) == 0 {
		delete(h.connSubs, connID)
	}
	h.mu.Unlock()
	return needUpUnsub
}

func (h *TopicBusHandler) listConnTopics(conn core.IConnection) []string {
	if conn == nil {
		return nil
	}
	connID := conn.ID()
	h.mu.RLock()
	set := h.connSubs[connID]
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	h.mu.RUnlock()
	sort.Strings(out)
	return out
}

func (h *TopicBusHandler) broadcastToSubscribers(ctx context.Context, src core.IConnection, topic string, payload []byte) {
	if len(payload) == 0 {
		return
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	srcConnID := ""
	if src != nil {
		srcConnID = src.ID()
	}

	// 快照订阅者列表，避免持锁发送。
	h.mu.RLock()
	subs := h.topicSubs[topic]
	targetIDs := make([]string, 0, len(subs))
	for id := range subs {
		if id == srcConnID {
			continue // 不回显
		}
		targetIDs = append(targetIDs, id)
	}
	h.mu.RUnlock()
	if len(targetIDs) == 0 {
		return
	}

	baseHdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(SubProtoTopicBus).WithSourceID(srv.NodeID())
	for _, id := range targetIDs {
		c, ok := srv.ConnManager().Get(id)
		if !ok || c == nil {
			continue
		}
		hdr := baseHdr.Clone()
		if nid := connNodeID(c); nid != 0 {
			hdr.WithTargetID(nid)
		}
		_ = srv.Send(ctx, c.ID(), hdr, payload)
	}
}

func (h *TopicBusHandler) forwardPublishUpstream(ctx context.Context, src core.IConnection, payload []byte) {
	parent := h.findParent(ctx)
	if parent == nil {
		return
	}
	if src != nil && src.ID() == parent.ID() {
		return // 来自父节点，不再向上回传
	}
	h.forwardToConn(ctx, parent, payload)
}

func (h *TopicBusHandler) maybeSubscribeUpstream(ctx context.Context, topics []string) {
	if len(topics) == 0 {
		return
	}
	parent := h.findParent(ctx)
	if parent == nil {
		h.rollbackUpstreamMarks(topics, true)
		return
	}
	if err := h.forwardSubscribeBatch(ctx, parent, topics); err != nil {
		h.log.Warn("topicbus subscribe upstream failed", "err", err)
		h.rollbackUpstreamMarks(topics, true)
	}
}

func (h *TopicBusHandler) maybeUnsubscribeUpstream(ctx context.Context, topics []string) {
	if len(topics) == 0 {
		return
	}
	parent := h.findParent(ctx)
	if parent == nil {
		return
	}
	if err := h.forwardUnsubscribeBatch(ctx, parent, topics); err != nil {
		h.log.Warn("topicbus unsubscribe upstream failed", "err", err)
		// 退订失败不回滚（保持 false），避免发送回环；后续 subscribe 会再上送。
	}
}

func (h *TopicBusHandler) rollbackUpstreamMarks(topics []string, active bool) {
	h.mu.Lock()
	for _, t := range topics {
		// active=true 表示之前将 upstreamActive[t] 置为 true 但上送失败，需要回滚为 false。
		if active {
			h.upstreamActive[t] = false
		}
	}
	h.mu.Unlock()
}

func (h *TopicBusHandler) forwardSubscribeBatch(ctx context.Context, target core.IConnection, topics []string) error {
	req := subscribeBatchReq{Topics: uniqueStable(topics)}
	raw, _ := json.Marshal(req)
	payload, _ := json.Marshal(message{Action: actionSubscribeBatch, Data: raw})
	return h.forwardToConn(ctx, target, payload)
}

func (h *TopicBusHandler) forwardUnsubscribeBatch(ctx context.Context, target core.IConnection, topics []string) error {
	req := subscribeBatchReq{Topics: uniqueStable(topics)}
	raw, _ := json.Marshal(req)
	payload, _ := json.Marshal(message{Action: actionUnsubscribeBatch, Data: raw})
	return h.forwardToConn(ctx, target, payload)
}

func (h *TopicBusHandler) forwardToConn(ctx context.Context, target core.IConnection, payload []byte) error {
	if target == nil || len(payload) == 0 {
		return nil
	}
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return target.SendWithHeader((&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(SubProtoTopicBus), payload, header.HeaderTcpCodec{})
	}
	hdr := (&header.HeaderTcp{}).WithMajor(header.MajorCmd).WithSubProto(SubProtoTopicBus).WithSourceID(srv.NodeID())
	if nid := connNodeID(target); nid != 0 {
		hdr.WithTargetID(nid)
	}
	return srv.Send(ctx, target.ID(), hdr, payload)
}

func (h *TopicBusHandler) findParent(ctx context.Context) core.IConnection {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return nil
	}
	cm := srv.ConnManager()
	if cm == nil {
		return nil
	}
	var parent core.IConnection
	cm.Range(func(c core.IConnection) bool {
		if role, ok := c.GetMeta(core.MetaRoleKey); ok {
			if s, ok2 := role.(string); ok2 && s == core.RoleParent {
				parent = c
				return false
			}
		}
		return true
	})
	return parent
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

func (h *TopicBusHandler) maybeResubscribeUpstream(ctx context.Context) {
	parent := h.findParent(ctx)
	if parent == nil {
		h.mu.Lock()
		h.parentConnID = ""
		h.parentConnNode = 0
		h.mu.Unlock()
		return
	}
	parentNode := connNodeID(parent)
	if parentNode == 0 {
		return
	}
	parentID := parent.ID()

	h.mu.RLock()
	same := parentID == h.parentConnID && parentNode == h.parentConnNode
	h.mu.RUnlock()
	if same {
		return
	}

	topics := h.snapshotActiveTopics()
	if len(topics) == 0 {
		h.mu.Lock()
		h.parentConnID = parentID
		h.parentConnNode = parentNode
		h.mu.Unlock()
		return
	}
	if err := h.forwardSubscribeBatch(ctx, parent, topics); err != nil {
		h.log.Warn("topicbus resubscribe upstream failed", "err", err)
		return
	}

	h.mu.Lock()
	h.parentConnID = parentID
	h.parentConnNode = parentNode
	for _, t := range topics {
		h.upstreamActive[t] = true
	}
	h.mu.Unlock()
}

func (h *TopicBusHandler) snapshotActiveTopics() []string {
	h.mu.RLock()
	topics := make([]string, 0, len(h.topicSubs))
	for t, subs := range h.topicSubs {
		if len(subs) > 0 {
			topics = append(topics, t)
		}
	}
	h.mu.RUnlock()
	sort.Strings(topics)
	return topics
}

func (h *TopicBusHandler) ensureConnCloseSubscription(ctx context.Context) {
	srv := core.ServerFromContext(ctx)
	if srv == nil {
		return
	}
	h.eventSubOnce.Do(func() {
		eb := srv.EventBus()
		if eb == nil {
			return
		}
		eb.Subscribe("conn.closed", func(evCtx context.Context, evt eventbus.Event) {
			connID, _ := parseConnClosed(evt.Data)
			if connID == "" {
				return
			}
			needUpUnsub := h.removeConnAll(connID)
			h.maybeUnsubscribeUpstream(evCtx, needUpUnsub)
		})
	})
}

func (h *TopicBusHandler) removeConnAll(connID string) []string {
	h.mu.Lock()
	topics, ok := h.connSubs[connID]
	if !ok {
		h.mu.Unlock()
		return nil
	}
	delete(h.connSubs, connID)
	needUp := make([]string, 0)
	for t := range topics {
		if subs, ok2 := h.topicSubs[t]; ok2 {
			delete(subs, connID)
			if len(subs) == 0 {
				delete(h.topicSubs, t)
				if h.upstreamActive[t] {
					h.upstreamActive[t] = false
					needUp = append(needUp, t)
				}
			}
		}
	}
	h.mu.Unlock()
	return needUp
}

func parseConnClosed(data any) (string, uint32) {
	switch v := data.(type) {
	case map[string]any:
		connID := ""
		nodeID := uint32(0)
		if c, ok := v["conn_id"]; ok {
			if s, ok2 := c.(string); ok2 {
				connID = s
			}
		}
		if c, ok := v["connID"]; ok {
			if s, ok2 := c.(string); ok2 {
				connID = s
			}
		}
		if n, ok := v["node_id"]; ok {
			switch vv := n.(type) {
			case uint32:
				nodeID = vv
			case uint64:
				nodeID = uint32(vv)
			case int:
				if vv >= 0 {
					nodeID = uint32(vv)
				}
			case int64:
				if vv >= 0 {
					nodeID = uint32(vv)
				}
			case float64:
				if vv >= 0 {
					nodeID = uint32(vv)
				}
			}
		}
		return connID, nodeID
	default:
		return "", 0
	}
}

func uniqueStable(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
