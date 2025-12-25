package grpc

import (
	"sync"
	"time"

	pb "github.com/example/turboci-lite/gen/turboci/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// EventBroadcaster manages work plan event subscriptions and broadcasting.
type EventBroadcaster struct {
	mu          sync.RWMutex
	subscribers map[string]map[*Subscriber]struct{} // workPlanID -> subscribers
}

// Subscriber represents a single subscription to work plan events.
type Subscriber struct {
	WorkPlanID     string
	IncludeChecks  bool
	IncludeStages  bool
	CheckStateFilter map[pb.CheckState]struct{}
	StageStateFilter map[pb.StageState]struct{}
	CheckIDFilter    map[string]struct{}
	StageIDFilter    map[string]struct{}
	Events         chan *pb.WorkPlanEvent
	Done           chan struct{}
}

// NewEventBroadcaster creates a new event broadcaster.
func NewEventBroadcaster() *EventBroadcaster {
	return &EventBroadcaster{
		subscribers: make(map[string]map[*Subscriber]struct{}),
	}
}

// Subscribe creates a new subscription based on the watch request.
func (b *EventBroadcaster) Subscribe(req *pb.WatchWorkPlanRequest) *Subscriber {
	sub := &Subscriber{
		WorkPlanID:    req.WorkPlanId,
		IncludeChecks: req.IncludeChecks,
		IncludeStages: req.IncludeStages,
		Events:        make(chan *pb.WorkPlanEvent, 100), // Buffered channel for backpressure
		Done:          make(chan struct{}),
	}

	// Build state filters
	if len(req.CheckStateFilter) > 0 {
		sub.CheckStateFilter = make(map[pb.CheckState]struct{})
		for _, s := range req.CheckStateFilter {
			sub.CheckStateFilter[s] = struct{}{}
		}
	}
	if len(req.StageStateFilter) > 0 {
		sub.StageStateFilter = make(map[pb.StageState]struct{})
		for _, s := range req.StageStateFilter {
			sub.StageStateFilter[s] = struct{}{}
		}
	}

	// Build ID filters
	if len(req.CheckIdFilter) > 0 {
		sub.CheckIDFilter = make(map[string]struct{})
		for _, id := range req.CheckIdFilter {
			sub.CheckIDFilter[id] = struct{}{}
		}
	}
	if len(req.StageIdFilter) > 0 {
		sub.StageIDFilter = make(map[string]struct{})
		for _, id := range req.StageIdFilter {
			sub.StageIDFilter[id] = struct{}{}
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.subscribers[req.WorkPlanId] == nil {
		b.subscribers[req.WorkPlanId] = make(map[*Subscriber]struct{})
	}
	b.subscribers[req.WorkPlanId][sub] = struct{}{}

	return sub
}

// Unsubscribe removes a subscription.
func (b *EventBroadcaster) Unsubscribe(sub *Subscriber) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if subs, ok := b.subscribers[sub.WorkPlanID]; ok {
		delete(subs, sub)
		if len(subs) == 0 {
			delete(b.subscribers, sub.WorkPlanID)
		}
	}

	close(sub.Done)
}

// BroadcastCheckEvent broadcasts a check event to relevant subscribers.
func (b *EventBroadcaster) BroadcastCheckEvent(workPlanID string, checkID string, oldState, newState pb.CheckState, check *pb.Check) {
	b.mu.RLock()
	subs := b.subscribers[workPlanID]
	if len(subs) == 0 {
		b.mu.RUnlock()
		return
	}

	// Copy subscriber list to avoid holding lock during send
	subList := make([]*Subscriber, 0, len(subs))
	for sub := range subs {
		subList = append(subList, sub)
	}
	b.mu.RUnlock()

	event := &pb.WorkPlanEvent{
		Timestamp: timestamppb.New(time.Now()),
		Event: &pb.WorkPlanEvent_CheckEvent{
			CheckEvent: &pb.CheckEvent{
				CheckId:  checkID,
				OldState: oldState,
				NewState: newState,
				Check:    check,
			},
		},
	}

	for _, sub := range subList {
		if sub.shouldReceiveCheckEvent(checkID, newState) {
			select {
			case sub.Events <- event:
			default:
				// Channel full, skip event (backpressure)
			}
		}
	}
}

// BroadcastStageEvent broadcasts a stage event to relevant subscribers.
func (b *EventBroadcaster) BroadcastStageEvent(workPlanID string, stageID string, oldState, newState pb.StageState, stage *pb.Stage) {
	b.mu.RLock()
	subs := b.subscribers[workPlanID]
	if len(subs) == 0 {
		b.mu.RUnlock()
		return
	}

	// Copy subscriber list to avoid holding lock during send
	subList := make([]*Subscriber, 0, len(subs))
	for sub := range subs {
		subList = append(subList, sub)
	}
	b.mu.RUnlock()

	event := &pb.WorkPlanEvent{
		Timestamp: timestamppb.New(time.Now()),
		Event: &pb.WorkPlanEvent_StageEvent{
			StageEvent: &pb.StageEvent{
				StageId:  stageID,
				OldState: oldState,
				NewState: newState,
				Stage:    stage,
			},
		},
	}

	for _, sub := range subList {
		if sub.shouldReceiveStageEvent(stageID, newState) {
			select {
			case sub.Events <- event:
			default:
				// Channel full, skip event (backpressure)
			}
		}
	}
}

// BroadcastWorkPlanCompleted broadcasts a completion event to all subscribers.
func (b *EventBroadcaster) BroadcastWorkPlanCompleted(workPlanID string, checksCompleted, stagesCompleted int32, hasFailures bool) {
	b.mu.RLock()
	subs := b.subscribers[workPlanID]
	if len(subs) == 0 {
		b.mu.RUnlock()
		return
	}

	// Copy subscriber list
	subList := make([]*Subscriber, 0, len(subs))
	for sub := range subs {
		subList = append(subList, sub)
	}
	b.mu.RUnlock()

	event := &pb.WorkPlanEvent{
		Timestamp: timestamppb.New(time.Now()),
		Event: &pb.WorkPlanEvent_CompletedEvent{
			CompletedEvent: &pb.WorkPlanCompletedEvent{
				ChecksCompleted:  checksCompleted,
				StagesCompleted:  stagesCompleted,
				HasFailures:      hasFailures,
			},
		},
	}

	for _, sub := range subList {
		select {
		case sub.Events <- event:
		default:
			// Channel full, skip event
		}
	}
}

// shouldReceiveCheckEvent determines if the subscriber should receive this check event.
func (s *Subscriber) shouldReceiveCheckEvent(checkID string, newState pb.CheckState) bool {
	if !s.IncludeChecks {
		return false
	}

	// Check ID filter
	if len(s.CheckIDFilter) > 0 {
		if _, ok := s.CheckIDFilter[checkID]; !ok {
			return false
		}
	}

	// Check state filter
	if len(s.CheckStateFilter) > 0 {
		if _, ok := s.CheckStateFilter[newState]; !ok {
			return false
		}
	}

	return true
}

// shouldReceiveStageEvent determines if the subscriber should receive this stage event.
func (s *Subscriber) shouldReceiveStageEvent(stageID string, newState pb.StageState) bool {
	if !s.IncludeStages {
		return false
	}

	// Stage ID filter
	if len(s.StageIDFilter) > 0 {
		if _, ok := s.StageIDFilter[stageID]; !ok {
			return false
		}
	}

	// Stage state filter
	if len(s.StageStateFilter) > 0 {
		if _, ok := s.StageStateFilter[newState]; !ok {
			return false
		}
	}

	return true
}

// HasSubscribers returns true if there are any subscribers for the given work plan.
func (b *EventBroadcaster) HasSubscribers(workPlanID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[workPlanID]) > 0
}

// SubscriberCount returns the number of subscribers for a work plan.
func (b *EventBroadcaster) SubscriberCount(workPlanID string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers[workPlanID])
}
