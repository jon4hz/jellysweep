package policy

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/stretchr/testify/require"
)

// stubPolicy is a controllable Policy implementation.
type stubPolicy struct {
	applyErr   error
	trigger    bool
	triggerErr error
	applied    int
	checked    int
}

func (s *stubPolicy) Apply(*database.Media) error {
	s.applied++
	return s.applyErr
}

func (s *stubPolicy) ShouldTriggerDeletion(context.Context, database.Media) (bool, error) {
	s.checked++
	return s.trigger, s.triggerErr
}

func TestEngineApplyAll(t *testing.T) {
	a, b := &stubPolicy{}, &stubPolicy{}
	e := NewEngine()
	e.SetPolicies(a, b)

	require.NoError(t, e.ApplyAll(&database.Media{}))
	require.Equal(t, 1, a.applied)
	require.Equal(t, 1, b.applied)
}

func TestEngineApplyAllStopsOnError(t *testing.T) {
	a := &stubPolicy{applyErr: errors.New("boom")}
	b := &stubPolicy{}
	e := NewEngine()
	e.SetPolicies(a, b)

	require.Error(t, e.ApplyAll(&database.Media{}))
	require.Zero(t, b.applied, "policies after a failing one must not run")
}

func TestEngineShouldTriggerDeletionFirstTrueWins(t *testing.T) {
	a := &stubPolicy{trigger: false}
	b := &stubPolicy{trigger: true}
	c := &stubPolicy{trigger: true}
	e := NewEngine()
	e.SetPolicies(a, b, c)

	got, err := e.ShouldTriggerDeletion(t.Context(), database.Media{})
	require.NoError(t, err)
	require.True(t, got)
	require.Equal(t, 1, a.checked)
	require.Equal(t, 1, b.checked)
	require.Zero(t, c.checked, "evaluation must stop at the first triggering policy")
}

func TestEngineShouldTriggerDeletionProtectedShortCircuits(t *testing.T) {
	// The protection guard must win even over a policy that wants to delete.
	p := &stubPolicy{trigger: true}
	e := NewEngine()
	e.SetPolicies(p)

	protectedUntil := time.Now().Add(time.Hour)
	got, err := e.ShouldTriggerDeletion(t.Context(), database.Media{ProtectedUntil: &protectedUntil})
	require.NoError(t, err)
	require.False(t, got)
	require.Zero(t, p.checked, "policies must not be consulted for protected media")
}

func TestEngineShouldTriggerDeletionExpiredProtectionConsultsPolicies(t *testing.T) {
	p := &stubPolicy{trigger: true}
	e := NewEngine()
	e.SetPolicies(p)

	protectedUntil := time.Now().Add(-time.Hour)
	got, err := e.ShouldTriggerDeletion(t.Context(), database.Media{ProtectedUntil: &protectedUntil})
	require.NoError(t, err)
	require.True(t, got, "expired protection no longer blocks deletion")
}

func TestEngineShouldTriggerDeletionPropagatesError(t *testing.T) {
	p := &stubPolicy{triggerErr: errors.New("boom")}
	e := NewEngine()
	e.SetPolicies(p)

	got, err := e.ShouldTriggerDeletion(t.Context(), database.Media{})
	require.Error(t, err)
	require.False(t, got)
}

func TestEngineNoPoliciesNeverTriggers(t *testing.T) {
	e := NewEngine()
	got, err := e.ShouldTriggerDeletion(t.Context(), database.Media{DefaultDeleteAt: time.Now().Add(-time.Hour)})
	require.NoError(t, err)
	require.False(t, got)
}
