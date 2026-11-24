package agenthub

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type AgentMessage struct {
	Type      string          `json:"type"`
	RequestID string          `json:"request_id,omitempty"`
	Action    string          `json:"action,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

type NodeHeartbeat struct {
	Hostname      string `json:"hostname"`
	SwarmActive   bool   `json:"swarm_active"`
	SwarmNodeRole string `json:"swarm_node_role,omitempty"`
	Containers    int    `json:"containers"`
	Timestamp     int64  `json:"timestamp"`
}

type ConnectedNode struct {
	NodeID        int64         `json:"node_id"`
	Name          string        `json:"name"`
	Online        bool          `json:"online"`
	LastHeartbeat NodeHeartbeat `json:"last_heartbeat"`
	ConnectedAt   time.Time     `json:"connected_at"`
}

type agentSession struct {
	nodeID      int64
	name        string
	conn        *websocket.Conn
	writeMu     sync.Mutex
	connectedAt time.Time
	heartbeat   NodeHeartbeat
}

type Hub struct {
	mu       sync.RWMutex
	sessions map[int64]*agentSession
	pending  map[string]chan json.RawMessage
}

func NewHub() *Hub {
	return &Hub{
		sessions: make(map[int64]*agentSession),
		pending:  make(map[string]chan json.RawMessage),
	}
}

func (h *Hub) Register(nodeID int64, name string, conn *websocket.Conn) {
	sess := &agentSession{
		nodeID:      nodeID,
		name:        name,
		conn:        conn,
		connectedAt: time.Now(),
	}

	h.mu.Lock()
	if old, exists := h.sessions[nodeID]; exists {
		_ = old.conn.Close()
	}
	h.sessions[nodeID] = sess
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		if current, ok := h.sessions[nodeID]; ok && current == sess {
			delete(h.sessions, nodeID)
		}
		h.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		var msg AgentMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}

		switch msg.Type {
		case "heartbeat":
			var hb NodeHeartbeat
			if err := json.Unmarshal(msg.Payload, &hb); err == nil {
				h.mu.Lock()
				sess.heartbeat = hb
				h.mu.Unlock()
			}
		case "response":
			h.mu.RLock()
			ch, ok := h.pending[msg.RequestID]
			h.mu.RUnlock()
			if ok {
				select {
				case ch <- msg.Payload:
				default:
				}
			}
		}
	}
}

func (h *Hub) SendCommand(nodeID int64, action string, payload any, timeout time.Duration) (json.RawMessage, error) {
	h.mu.RLock()
	sess, ok := h.sessions[nodeID]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("node %d is offline", nodeID)
	}

	var rawPayload json.RawMessage
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		rawPayload = b
	}

	reqID := fmt.Sprintf("req-%d-%d", nodeID, time.Now().UnixNano())
	respCh := make(chan json.RawMessage, 1)

	h.mu.Lock()
	h.pending[reqID] = respCh
	h.mu.Unlock()

	defer func() {
		h.mu.Lock()
		delete(h.pending, reqID)
		h.mu.Unlock()
	}()

	sess.writeMu.Lock()
	err := sess.conn.WriteJSON(AgentMessage{
		Type:      "command",
		RequestID: reqID,
		Action:    action,
		Payload:   rawPayload,
	})
	sess.writeMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to write command to agent: %w", err)
	}

	select {
	case resp := <-respCh:
		return resp, nil
	case <-time.After(timeout):
		return nil, errors.New("agent command timed out")
	}
}

func (h *Hub) ListConnectedNodes() map[int64]ConnectedNode {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make(map[int64]ConnectedNode, len(h.sessions))
	for id, sess := range h.sessions {
		out[id] = ConnectedNode{
			NodeID:        id,
			Name:          sess.name,
			Online:        true,
			LastHeartbeat: sess.heartbeat,
			ConnectedAt:   sess.connectedAt,
		}
	}
	return out
}
