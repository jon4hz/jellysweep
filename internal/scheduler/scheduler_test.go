package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/go-co-op/gocron/v2"
	"github.com/stretchr/testify/require"
)

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	s, err := New()
	require.NoError(t, err)
	t.Cleanup(func() { s.Stop() }) //nolint:errcheck
	return s
}

var everyTwelveHours = gocron.CronJob("0 */12 * * *", false)

func TestAddJobRegistersJobInfo(t *testing.T) {
	s := newTestScheduler(t)
	require.NoError(t, s.AddSingletonJob("cleanup", "Cleanup", "desc", "0 */12 * * *", everyTwelveHours,
		func(context.Context) error { return nil }, true))

	job, ok := s.GetJob("cleanup")
	require.True(t, ok)
	require.Equal(t, JobStatusScheduled, job.Status)
	require.True(t, job.Enabled)
	require.True(t, job.Singleton)
	require.True(t, job.InstantAfterStart)
	require.Equal(t, "0 */12 * * *", job.Schedule)
	require.Len(t, s.GetJobs(), 1)
}

func TestAddJobInvalidCronFails(t *testing.T) {
	s := newTestScheduler(t)
	err := s.AddJob("bad", "Bad", "desc", "nope", gocron.CronJob("nope", false),
		func(context.Context) error { return nil }, false)
	require.Error(t, err)
	_, ok := s.GetJob("bad")
	require.False(t, ok)
}

func TestWrappedJobTracksRunsAndErrors(t *testing.T) {
	s := newTestScheduler(t)
	calls := 0
	var jobErr error
	require.NoError(t, s.AddJob("job", "Job", "desc", "0 */12 * * *", everyTwelveHours,
		func(context.Context) error { calls++; return jobErr }, false))

	// Drive the wrapped function directly instead of waiting for cron.
	run := s.wrapJobFunc("job", s.jobFuncs["job"])

	run()
	job, _ := s.GetJob("job")
	require.Equal(t, 1, calls)
	require.Equal(t, 1, job.RunCount)
	require.Equal(t, JobStatusCompleted, job.Status)
	require.Zero(t, job.ErrorCount)
	require.False(t, job.LastRun.IsZero())

	jobErr = errors.New("boom")
	run()
	require.Equal(t, 2, job.RunCount)
	require.Equal(t, 1, job.ErrorCount)
	require.Equal(t, JobStatusFailed, job.Status)
	require.Equal(t, "boom", job.LastError)

	jobErr = nil
	run()
	require.Equal(t, JobStatusCompleted, job.Status)
	require.Empty(t, job.LastError, "a successful run clears the last error")
}

func TestDisabledJobIsSkipped(t *testing.T) {
	s := newTestScheduler(t)
	calls := 0
	require.NoError(t, s.AddJob("job", "Job", "desc", "0 */12 * * *", everyTwelveHours,
		func(context.Context) error { calls++; return nil }, false))
	run := s.wrapJobFunc("job", s.jobFuncs["job"])

	require.NoError(t, s.DisableJob("job"))
	run()
	require.Zero(t, calls, "disabled jobs must not execute")
	job, _ := s.GetJob("job")
	require.Zero(t, job.RunCount)

	require.NoError(t, s.EnableJob("job"))
	run()
	require.Equal(t, 1, calls)
}

func TestUnknownJobErrors(t *testing.T) {
	s := newTestScheduler(t)
	require.Error(t, s.EnableJob("nope"))
	require.Error(t, s.DisableJob("nope"))
	require.Error(t, s.RunJobNow("nope"))
}

func TestWrappedJobUnknownIDIsNoop(t *testing.T) {
	s := newTestScheduler(t)
	require.NotPanics(t, func() {
		s.wrapJobFunc("ghost", func(context.Context) error { return nil })()
	})
}
