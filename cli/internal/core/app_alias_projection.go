package core

import (
	"fmt"
	"sort"
	"time"

	"github.com/chattocorp/chatto/cli/internal/evtstream"
	evtv1 "github.com/chattocorp/chatto/cli/internal/pb/chatto/core/evt/v1"
	"github.com/chattocorp/chatto/pkg/events"
)

// AppAliasProjection owns deterministic target identifiers that need replay
// state. Prefix-only user, room, app, and file aliases do not need storage.
type AppAliasProjection struct {
	events.MemoryProjection
	workspaceID string
	roomClocks  map[string]int64
	byRoom      map[string]map[string]string
	byMessage   map[string]messageCompatibilityAlias
}

type messageCompatibilityAlias struct {
	RoomID    string
	Timestamp string
}

func NewAppAliasProjection() *AppAliasProjection {
	return &AppAliasProjection{
		roomClocks: make(map[string]int64),
		byRoom:     make(map[string]map[string]string),
		byMessage:  make(map[string]messageCompatibilityAlias),
	}
}

func (p *AppAliasProjection) Subjects() []string {
	return []string{
		evtstream.SetupAggregate().AllEventsFilter(),
		evtstream.RoomEventTypeFilter(evtstream.EventMessagePosted),
	}
}

func (p *AppAliasProjection) Apply(event *evtv1.Event, _ uint64) error {
	if event == nil {
		return nil
	}
	p.Lock()
	defer p.Unlock()
	switch value := event.GetEvent().(type) {
	case *evtv1.Event_ServerInitialized:
		if p.workspaceID == "" && event.GetId() != "" {
			p.workspaceID = compatibilityAlias("T", event.GetId())
		}
	case *evtv1.Event_MessagePosted:
		payload := value.MessagePosted
		if payload == nil || payload.GetRoomId() == "" || event.GetId() == "" {
			return nil
		}
		if _, exists := p.byMessage[event.GetId()]; exists {
			return nil
		}
		micros := event.GetCreatedAt().AsTime().UnixMicro()
		if previous := p.roomClocks[payload.GetRoomId()]; micros <= previous {
			micros = previous + 1
		}
		p.roomClocks[payload.GetRoomId()] = micros
		timestamp := formatCompatibilityTimestamp(micros)
		if p.byRoom[payload.GetRoomId()] == nil {
			p.byRoom[payload.GetRoomId()] = make(map[string]string)
		}
		p.byRoom[payload.GetRoomId()][timestamp] = event.GetId()
		p.byMessage[event.GetId()] = messageCompatibilityAlias{RoomID: payload.GetRoomId(), Timestamp: timestamp}
	}
	return nil
}

func compatibilityAlias(prefix, nativeID string) string {
	if nativeID == "" {
		return ""
	}
	if len(nativeID) > 1 {
		return prefix + nativeID[1:]
	}
	return prefix + nativeID
}

func formatCompatibilityTimestamp(micros int64) string {
	seconds := micros / int64(time.Second/time.Microsecond)
	fraction := micros % int64(time.Second/time.Microsecond)
	if fraction < 0 {
		seconds--
		fraction += int64(time.Second / time.Microsecond)
	}
	return fmt.Sprintf("%d.%06d", seconds, fraction)
}

func (p *AppAliasProjection) workspace() string {
	p.RLock()
	defer p.RUnlock()
	return p.workspaceID
}

func (p *AppAliasProjection) messageTimestamp(messageID string) (string, string, bool) {
	p.RLock()
	defer p.RUnlock()
	value, ok := p.byMessage[messageID]
	return value.RoomID, value.Timestamp, ok
}

func (p *AppAliasProjection) messageID(roomID, timestamp string) (string, bool) {
	p.RLock()
	defer p.RUnlock()
	messageID, ok := p.byRoom[roomID][timestamp]
	return messageID, ok
}

func (p *AppAliasProjection) adminProjectionEstimate() (int64, int64, []ProjectionAdminMetric) {
	p.RLock()
	defer p.RUnlock()
	var estimatedBytes int64 = int64(len(p.workspaceID))
	for messageID, alias := range p.byMessage {
		estimatedBytes += int64(len(messageID) + len(alias.RoomID) + len(alias.Timestamp) + 32)
	}
	roomIDs := make([]string, 0, len(p.byRoom))
	for roomID := range p.byRoom {
		roomIDs = append(roomIDs, roomID)
	}
	sort.Strings(roomIDs)
	for _, roomID := range roomIDs {
		estimatedBytes += int64(len(roomID) + 16)
		for timestamp, messageID := range p.byRoom[roomID] {
			estimatedBytes += int64(len(timestamp) + len(messageID) + 32)
		}
	}
	return int64(len(p.byMessage) + 1), estimatedBytes, nil
}
