package policy

import (
	"context"
	"time"

	"github.com/jon4hz/jellysweep/database"
)

// Policy is the interface for all deletion policies.
type Policy interface {
	Apply(*database.Media) error
	ShouldTriggerDeletion(context.Context, *database.Media) (bool, error)
}

// Engine is the policy engine that applies all available policies to a media item.
type Engine struct {
	policies []Policy
}

// NewEngine creates a new policy engine.
func NewEngine() *Engine {
	return &Engine{
		policies: []Policy{},
	}
}

// SetPolicies sets the policies for the engine, replacing any existing ones.
func (e *Engine) SetPolicies(policies ...Policy) {
	e.policies = policies
}

// ApplyAll applies all registered policies to a media item.
func (e *Engine) ApplyAll(media *database.Media) error {
	for _, policy := range e.policies {
		if err := policy.Apply(media); err != nil {
			return err
		}
	}
	return nil
}

// ShouldTriggerDeletion checks if any policy indicates that the media should be deleted.
// All policies will be checked until one returns true.
func (e *Engine) ShouldTriggerDeletion(ctx context.Context, media *database.Media) (bool, error) {
	if !media.ProtectedUntil.IsZero() && media.ProtectedUntil.Before(time.Now()) {
		// If the media is protected until a certain date, do not delete it
		return false, nil
	}
	for _, policy := range e.policies {
		trigger, err := policy.ShouldTriggerDeletion(ctx, media)
		if err != nil {
			return false, err
		}
		if trigger {
			return true, nil
		}
	}
	return false, nil
}
