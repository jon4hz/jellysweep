package policy

import (
	"context"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
)

// Policy is the interface for all deletion policies.
type Policy interface {
	Apply(*database.Media) error
	ShouldTriggerDeletion(context.Context, database.Media) (bool, error)
	GetDeletionAt(context.Context, database.Media) (time.Time, error)
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
func (e *Engine) ShouldTriggerDeletion(ctx context.Context, media database.Media) (bool, error) {
	// usually we shouldn't get protected media here because the database query filters them out.
	// but just to be safe:
	if media.ProtectedUntil != nil && !media.ProtectedUntil.IsZero() && media.ProtectedUntil.After(time.Now()) {
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

// GetDeleteAt gets the earliest deletion time from all policies for the media item.
func (e *Engine) GetDeleteAt(ctx context.Context, media database.Media) (time.Time, error) {
	var earliest time.Time

	for _, policy := range e.policies {
		deleteAt, err := policy.GetDeletionAt(ctx, media)
		if err != nil {
			return time.Time{}, err
		}
		if deleteAt.IsZero() {
			continue
		}
		if earliest.IsZero() || deleteAt.Before(earliest) {
			earliest = deleteAt
		}
	}

	return earliest, nil
}
