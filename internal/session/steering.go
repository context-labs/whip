package session

import "time"

type InputDelivery string

const (
	DeliveryQueued InputDelivery = "queued"
	DeliverySteer  InputDelivery = "steer"
)

type InputSteeringID string

// InputSteeringRef is accepted routing intent. Consumed distinguishes a message
// actually presented to that turn from an input still waiting in the queue.
// An untaken input can subsequently open its own turn; it is never retargeted.
type InputSteeringRef struct {
	ID       InputSteeringID
	TurnID   TurnID
	Consumed bool
}

type SteerInputRequest struct {
	ID        InputSteeringID
	SessionID SessionID
	InputID   InputID
	TurnID    TurnID
}

type InputSteering struct {
	SteerInputRequest
	CreatedAt time.Time
}

type InputSteeringResult struct {
	Steering InputSteering
	Input    *Input
	Deleted  bool
}
