package engine

// End-to-end lifecycle scenarios for the deletion engine, driven through
// runCleanupJob against a real database and fake external services.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jon4hz/jellysweep/internal/database"
	"github.com/jon4hz/jellysweep/internal/policy"
	"github.com/stretchr/testify/require"
)

// Scenario 1: the happy path. An item is picked up, waits out the cleanup
// delay untouched, and only then is deleted everywhere.
func TestLifecycleMarkWaitDelete(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Old Movie")
	show := h.addSeries("Old Show")

	// First run: both items are marked, nothing is deleted.
	h.mustRunCleanup()
	movieRow := h.assertMarked(movie)
	h.assertMarked(show)
	h.assertHistory(movie, database.HistoryEventPickedUp)
	require.WithinDuration(t, time.Now().Add(30*24*time.Hour), movieRow.DefaultDeleteAt, time.Minute,
		"default cleanup delay is 30 days")
	h.assertNotDeletedAnywhere(movie)
	h.assertNotDeletedAnywhere(show)

	// Second run while waiting: no duplicate marking, still no deletion.
	h.mustRunCleanup()
	rowAfterSecondRun := h.assertMarked(movie)
	require.Equal(t, movieRow.ID, rowAfterSecondRun.ID, "waiting items must not be re-marked")
	h.assertNotDeletedAnywhere(movie)

	// The delay passes: both items are deleted in the arr and Jellyfin.
	h.elapseDeleteDelay(movie)
	h.elapseDeleteDelay(show)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(movie)
	h.assertDeletedEverywhere(show)
}

// Scenario 2: keep request approved -> protection -> expiry -> fresh pickup ->
// eventual deletion. The full protection round trip.
func TestLifecycleKeepRequestApproveExpireRedelete(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Beloved Movie")
	h.mustRunCleanup()
	firstRow := h.assertMarked(movie)

	// User requests to keep it; no auto-approval -> pending.
	autoApproved, err := h.requestKeep(movie)
	require.NoError(t, err)
	require.False(t, autoApproved)
	pending, err := h.db.GetMediaWithPendingRequest(t.Context())
	require.NoError(t, err)
	require.Len(t, pending, 1)

	// Admin approves: protected for the default 90 days.
	require.NoError(t, h.decideKeep(movie, true))
	protectedRow := h.assertMarked(movie)
	require.NotNil(t, protectedRow.ProtectedUntil)
	require.WithinDuration(t, time.Now().Add(90*24*time.Hour), *protectedRow.ProtectedUntil, time.Minute)
	h.assertHistory(movie, database.HistoryEventRequestApproved, database.HistoryEventProtected)

	// Even with the delete date long past, protection wins.
	h.elapseDeleteDelay(movie)
	h.mustRunCleanup()
	h.assertNotDeletedAnywhere(movie)

	// Protection expires: the same run tombstones the protected row and picks
	// the item up again as a fresh candidate with a new 30-day clock.
	h.elapseProtection(movie)
	h.mustRunCleanup()
	h.assertTombstone(movie, database.DBDeleteReasonProtectionExpired)
	h.assertHistory(movie, database.HistoryEventProtectionExpired)
	freshRow := h.assertMarked(movie)
	require.NotEqual(t, firstRow.ID, freshRow.ID, "re-pickup must be a fresh row")
	require.Nil(t, freshRow.ProtectedUntil)
	require.WithinDuration(t, time.Now().Add(30*24*time.Hour), freshRow.DefaultDeleteAt, time.Minute)

	// A fresh row also means a fresh keep request is possible.
	_, err = h.requestKeep(movie)
	require.NoError(t, err, "a new row allows a new keep request")

	// Without another approval, the clock runs out and the item is deleted.
	h.elapseDeleteDelay(movie)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(movie)
}

// Scenario 3: keep request denied -> unkeepable -> no second request -> deleted.
// Variant: the denial happens while the item is protected; protection must go.
func TestLifecycleKeepRequestDeniedUnkeepable(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Doomed Movie")
	h.mustRunCleanup()

	_, err := h.requestKeep(movie)
	require.NoError(t, err)

	// Admin protects it first (simulates deny-while-protected).
	row := h.assertMarked(movie)
	require.NoError(t, h.e.MarkMediaAsProtected(t.Context(), row.ID, h.admin.ID))

	// Then the request is denied: unkeepable, and the protection is cleared.
	require.NoError(t, h.decideKeep(movie, false))
	denied := h.assertMarked(movie)
	require.True(t, denied.Unkeepable)
	require.Nil(t, denied.ProtectedUntil, "denying must clear existing protection")
	h.assertHistory(movie, database.HistoryEventRequestDenied)

	// No second chance: the unkeepable flag is checked before the
	// one-request-per-media rule, so this surfaces as ErrUnkeepableMedia.
	_, err = h.requestKeep(movie)
	require.ErrorIs(t, err, ErrUnkeepableMedia)

	h.elapseDeleteDelay(movie)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(movie)
}

// Scenario 4: streaming during the wait unmarks the item; once the stream ages
// out, the item is picked up again.
func TestLifecycleStreamedDuringWait(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Rewatched Movie")
	h.mustRunCleanup()
	h.assertMarked(movie)

	// Someone watches it two days later (inside the 30-day threshold).
	h.stream(movie, time.Now().Add(-2*24*time.Hour))
	h.mustRunCleanup()
	h.assertTombstone(movie, database.DBDeleteReasonStreamed)
	h.assertHistory(movie, database.HistoryEventStreamed)
	h.assertNotDeletedAnywhere(movie)
	h.assertNotMarked(movie)

	// The stream ages beyond the threshold: fresh pickup.
	h.stream(movie, time.Now().Add(-40*24*time.Hour))
	h.mustRunCleanup()
	h.assertMarked(movie)
}

// Scenario 5: an item that disappears from Jellyfin is cleaned out of the
// database without touching the arr.
func TestLifecycleVanishedFromJellyfin(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Vanishing Movie")
	h.mustRunCleanup()
	h.assertMarked(movie)

	h.vanishFromJellyfin(movie)
	h.mustRunCleanup()
	h.assertTombstone(movie, database.DBDeleteReasonMissingInJellyfin)
	h.assertHistory(movie, database.HistoryEventNotFoundAnymore)
	require.False(t, h.radarr.hasDeleted(movie.arrID), "vanished items must not trigger arr deletions")
}

// Scenario 6: dry run marks and tracks but never deletes anything, anywhere.
func TestLifecycleDryRun(t *testing.T) {
	h := newHarness(t, withDryRun())
	movie := h.addMovie("Safe Movie")

	h.mustRunCleanup()
	h.assertMarked(movie)

	h.elapseDeleteDelay(movie)
	h.mustRunCleanup()
	h.assertNotDeletedAnywhere(movie)
	h.assertMarked(movie)
	requireNoHistoryEvent(t, h, movie, database.HistoryEventDeleted)
}

// Scenario 7: a disk usage policy fires before the default delete date when
// the disk is full - and stays quiet when it is not.
func TestLifecycleDiskUsageTriggersEarly(t *testing.T) {
	h := newHarness(t, withDiskThresholds("Movies", 90, 2))
	h.usageFn = staticDiskUsage(95)
	movie := h.addMovie("Space Hog")

	h.mustRunCleanup()
	row := h.assertMarked(movie)
	require.Len(t, row.DiskUsageDeletePolicies, 1)

	// Only the disk usage date elapses; the default date is still 30 days out.
	h.elapseDiskUsageDate(movie)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(movie)
}

func TestLifecycleDiskUsageBelowThresholdWaits(t *testing.T) {
	h := newHarness(t, withDiskThresholds("Movies", 90, 2))
	h.usageFn = staticDiskUsage(50)
	movie := h.addMovie("Modest Movie")

	h.mustRunCleanup()
	h.elapseDiskUsageDate(movie)
	h.mustRunCleanup()
	h.assertNotDeletedAnywhere(movie)
	h.assertMarked(movie)
}

func TestLifecycleEstimatedDeleteAtFollowsDiskUsage(t *testing.T) {
	h := newHarness(t, withDiskThresholds("Movies", 90, 2))
	h.usageFn = staticDiskUsage(95)
	movie := h.addMovie("Space Hog")

	// Above the threshold the estimate is the (earlier) disk usage date.
	h.mustRunCleanup()
	row := h.assertMarked(movie)
	require.Len(t, row.DiskUsageDeletePolicies, 1)
	require.WithinDuration(t, row.DiskUsageDeletePolicies[0].DeleteDate, row.EstimatedDeleteAt, time.Second)
	require.True(t, row.EstimatedDeleteAt.Before(row.DefaultDeleteAt))

	// Once usage drops below the threshold the estimate falls back to the default date.
	h.usageFn = staticDiskUsage(50)
	h.mustRunCleanup()
	row = h.assertMarked(movie)
	require.WithinDuration(t, row.DefaultDeleteAt, row.EstimatedDeleteAt, time.Second)
}

// Scenario 8: a failure while marking blocks the deletion pass entirely - the
// central safety interlock of the cleanup job.
func TestLifecycleMarkErrorBlocksCleanup(t *testing.T) {
	h := newHarness(t)
	dueMovie := h.addMovie("Due Movie")
	h.mustRunCleanup()
	h.elapseDeleteDelay(dueMovie)

	// A new item whose stats lookup breaks poisons the marking phase.
	brokenMovie := h.addMovie("Broken Stats Movie")
	h.stats.err[brokenMovie.jellyfinID] = errors.New("stats backend down")

	err := h.runCleanup()
	require.Error(t, err, "the run must surface the marking failure")
	h.assertNotDeletedAnywhere(dueMovie)
	h.assertMarked(dueMovie)

	// Stats recover: the next run deletes the due item.
	delete(h.stats.err, brokenMovie.jellyfinID)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(dueMovie)
}

// Scenario 9: if Jellyfin cannot be reached at all, the run aborts before
// touching anything.
func TestLifecycleGatherFailureAborts(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Untouchable Movie")
	h.mustRunCleanup()
	h.elapseDeleteDelay(movie)

	h.jf.getErr = errors.New("jellyfin unreachable")
	require.Error(t, h.runCleanup())
	h.assertNotDeletedAnywhere(movie)
	h.assertMarked(movie)
}

// Scenario 10: partial deletion - the arr succeeded but Jellyfin failed.
// KNOWN-BEHAVIOR: the row is tombstoned anyway; the files are gone (the arr is
// the source of truth) and the stale Jellyfin entry resolves on its next scan.
func TestLifecyclePartialDeletionJellyfinFails(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Half Deleted Movie")
	h.mustRunCleanup()
	h.elapseDeleteDelay(movie)

	h.jf.removeErr[movie.jellyfinID] = errors.New("jellyfin down")
	h.mustRunCleanup()
	require.True(t, h.radarr.hasDeleted(movie.arrID))
	require.False(t, h.jf.hasRemoved(movie.jellyfinID))
	h.assertTombstone(movie, database.DBDeleteReasonDefault)
}

// Scenario 11: the arr deletion fails - the row survives and the deletion is
// retried on the next run.
func TestLifecycleArrDeleteFailsRetriedNextRun(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Stubborn Movie")
	h.mustRunCleanup()
	h.elapseDeleteDelay(movie)

	h.radarr.deleteErr[movie.arrID] = errors.New("radarr down")
	h.mustRunCleanup()
	h.assertNotDeletedAnywhere(movie)
	h.assertMarked(movie)

	delete(h.radarr.deleteErr, movie.arrID)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(movie)
}

// Scenario 12: no stats backend configured - the full lifecycle must work
// without panicking (regression for the nil Statser).
func TestLifecycleNoStatsBackend(t *testing.T) {
	h := newHarness(t, withNilStats())
	movie := h.addMovie("Unwatched Movie")

	h.mustRunCleanup()
	h.assertMarked(movie)

	h.elapseDeleteDelay(movie)
	h.mustRunCleanup()
	h.assertDeletedEverywhere(movie)
}

// Scenario 13: a protected item that gets streamed keeps its protection and
// its request history (regression for the includeProtected fix).
func TestLifecycleProtectedItemStreamed(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Protected Favorite")
	h.mustRunCleanup()

	_, err := h.requestKeep(movie)
	require.NoError(t, err)
	require.NoError(t, h.decideKeep(movie, true))

	h.stream(movie, time.Now().Add(-24*time.Hour))
	h.mustRunCleanup()

	row := h.assertMarked(movie)
	require.NotNil(t, row.ProtectedUntil, "streaming must not strip protection")
	require.NotZero(t, row.Request.ID, "the keep request history must survive")
}

// Scenario 14: legacy tag migration on a fresh database - tags become rows and
// are then wiped from the arrs.
func TestLifecycleTagMigration(t *testing.T) {
	h := newHarness(t, withTagMigration())
	deleteTagged := h.addMovie("Legacy Delete", withArrTags("jellysweep-delete-2027-03-01"))
	keepTagged := h.addMovie("Legacy Keep", withArrTags("jellysweep-must-keep-2027-06-01-alice"))
	mustDeleteTagged := h.addMovie("Legacy Must Delete", withArrTags(
		"jellysweep-delete-2027-03-01", "jellysweep-must-delete-for-sure"))
	diskTagged := h.addMovie("Legacy Disk", withArrTags("jellysweep-delete-du90-2027-08-23"))
	untagged := h.addMovie("Untagged Movie")

	h.mustRunCleanup()

	deleteRow := h.assertMarked(deleteTagged)
	require.Equal(t, time.Date(2027, 3, 1, 0, 0, 0, 0, time.UTC), deleteRow.DefaultDeleteAt.UTC())

	keepRow := h.assertMarked(keepTagged)
	require.NotNil(t, keepRow.ProtectedUntil)
	require.Equal(t, time.Date(2027, 6, 1, 0, 0, 0, 0, time.UTC), keepRow.ProtectedUntil.UTC())

	mustDeleteRow := h.assertMarked(mustDeleteTagged)
	require.True(t, mustDeleteRow.Unkeepable)

	diskRow := h.assertMarked(diskTagged)
	require.Len(t, diskRow.DiskUsageDeletePolicies, 1)
	require.Equal(t, 90.0, diskRow.DiskUsageDeletePolicies[0].Threshold)

	// The untagged item is not migrated, but the regular pipeline of the same
	// run picks it up as a normal candidate.
	untaggedRow := h.assertMarked(untagged)
	require.WithinDuration(t, time.Now().Add(30*24*time.Hour), untaggedRow.DefaultDeleteAt, time.Minute)

	// All legacy tags are wiped from both arrs after migration.
	require.NotZero(t, h.radarr.tagResets, "radarr tags must be reset after migration")
	require.NotZero(t, h.sonarr.tagResets, "sonarr tags must be reset after migration")
}

// --- helpers ---

func staticDiskUsage(percent float64) policy.UsageFunc {
	return func(context.Context, database.MediaType) (float64, bool, error) { return percent, true, nil }
}

func requireNoHistoryEvent(t *testing.T, h *harness, m *mediaRef, unwanted database.HistoryEventType) {
	t.Helper()
	events, err := h.db.GetHistoryEventsByJellyfinID(t.Context(), m.jellyfinID)
	require.NoError(t, err)
	for _, event := range events {
		require.NotEqual(t, unwanted, event.EventType)
	}
}

// Scenario 15: an exclude tag added in the arr after the item was marked
// removes it from the queue; removing the tag again makes it a candidate.
func TestLifecycleExcludeTagAddedDuringWait(t *testing.T) {
	h := newHarness(t)
	h.cfg.Libraries["TV"].Filter.ExcludeTags = []string{"keep"}
	movie := h.addMovie("Tagged Later Movie")
	series := h.addSeries("Tagged Later Series")
	h.mustRunCleanup()
	h.assertMarked(movie)
	h.assertMarked(series)

	h.setArrTags(movie, "jellysweep-ignore")
	h.setArrTags(series, "4k", "keep")
	h.mustRunCleanup()

	for _, m := range []*mediaRef{movie, series} {
		h.assertNotMarked(m)
		h.assertTombstone(m, database.DBDeleteReasonExcludedByTag)
		h.assertHistory(m, database.HistoryEventExcludedByTag)
		h.assertNotDeletedAnywhere(m)
	}

	// Even once the delay would have passed, nothing is deleted.
	h.mustRunCleanup()
	h.assertNotMarked(movie)
	h.assertNotDeletedAnywhere(movie)

	// Dropping the tag makes the item a fresh candidate again.
	h.setArrTags(movie)
	h.mustRunCleanup()
	h.assertMarked(movie)
}

// Scenario 16: an exclude tag on a protected item must not strip the
// protection or orphan the keep request.
func TestLifecycleExcludeTagOnProtectedItem(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Protected Then Tagged")
	h.mustRunCleanup()

	_, err := h.requestKeep(movie)
	require.NoError(t, err)
	require.NoError(t, h.decideKeep(movie, true))

	h.setArrTags(movie, "jellysweep-ignore")
	h.mustRunCleanup()

	row := h.assertMarked(movie)
	require.NotNil(t, row.ProtectedUntil, "an exclude tag must not strip protection")
	require.NotZero(t, row.Request.ID, "the keep request history must survive")

	// Once the protection expires the row is dropped, and the tag keeps the
	// item out of the queue from then on.
	h.elapseProtection(movie)
	h.mustRunCleanup()
	h.assertNotMarked(movie)
	h.assertNotDeletedAnywhere(movie)
}

// Scenario 17: a queued item removed and re-added in the arr (new arr ID, same
// Jellyfin item) with an exclude tag is still dequeued.
func TestLifecycleExcludeTagAfterArrReadd(t *testing.T) {
	h := newHarness(t)
	movie := h.addMovie("Re-added Movie")
	h.mustRunCleanup()
	h.assertMarked(movie)

	h.readdToArr(movie, "jellysweep-ignore")
	h.mustRunCleanup()
	h.assertNotMarked(movie)
	h.assertTombstone(movie, database.DBDeleteReasonExcludedByTag)
	h.assertNotDeletedAnywhere(movie)
}
