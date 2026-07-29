package backup

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func testOptions(mode Mode) PlanOptions {
	n := 0
	return PlanOptions{
		Mode:         mode,
		DefaultOwner: "importer",
		NewID: func() string {
			n++
			return "generated-id-" + string(rune('0'+n))
		},
		Now: fixedClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)),
	}
}

func archiveOf(entries ...TunnelEntry) *Archive {
	return &Archive{Version: SchemaVersion, Tunnels: entries}
}

func itemFor(t *testing.T, plan *ImportPlan, name string) PlanItem {
	t.Helper()
	for _, item := range plan.Items {
		if item.Name == name {
			return item
		}
	}
	t.Fatalf("no plan item for %q; plan has %d items", name, len(plan.Items))
	return PlanItem{}
}

func TestPlanCreatesTunnelWithNoNameMatch(t *testing.T) {
	plan, err := Plan(nil, archiveOf(validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	item := itemFor(t, plan, "staging-api")
	if item.Action != ActionCreate {
		t.Errorf("got action %q, want create", item.Action)
	}
	if item.Spec == nil {
		t.Fatal("create item must carry a spec")
	}
	if item.Spec.Owner != "importer" {
		t.Errorf("got owner %q, want importer (the DefaultOwner)", item.Spec.Owner)
	}
}

func TestPlanUpdatesOnNameMatchAndPreservesIdentity(t *testing.T) {
	stored := sampleSpec()
	stored.Owner = "original-owner"

	changed := EntryFromSpec(stored)
	changed.ID = "a-completely-different-id"
	changed.Owner = "someone-else"
	changed.LocalPort = 15432

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(changed), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	item := itemFor(t, plan, "prod-db")
	if item.Action != ActionUpdate {
		t.Fatalf("got action %q, want update", item.Action)
	}
	if item.Spec.ID != stored.ID {
		t.Errorf("got ID %q, want the stored ID %q — the UI's saved ordering keys on it",
			item.Spec.ID, stored.ID)
	}
	if item.Spec.Owner != "original-owner" {
		t.Errorf("got owner %q, want original-owner — a restore must not reassign ownership",
			item.Spec.Owner)
	}
	if !item.Spec.CreatedAt.Equal(stored.CreatedAt) {
		t.Errorf("got CreatedAt %v, want the stored %v", item.Spec.CreatedAt, stored.CreatedAt)
	}
	if item.Spec.LocalPort != 15432 {
		t.Errorf("got LocalPort %d, want 15432 — the change should be applied", item.Spec.LocalPort)
	}
}

func TestPlanSkipsIdenticalTunnel(t *testing.T) {
	stored := sampleSpec()
	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(EntryFromSpec(stored)), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip — re-import must not bounce a healthy tunnel", item.Action)
	}
}

func TestPlanDoesNotSkipWhenOnlyDesiredStatusChanges(t *testing.T) {
	stored := sampleSpec()
	stored.DesiredStatus = types.DesiredStatusStopped

	entry := EntryFromSpec(stored)
	entry.DesiredStatus = "active"

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionUpdate {
		t.Fatalf("got action %q, want update — stopped to active is a real change", item.Action)
	}
}

func TestPlanTreatsOmittedDesiredStatusAsStopped(t *testing.T) {
	stored := sampleSpec()
	stored.DesiredStatus = types.DesiredStatusStopped

	entry := EntryFromSpec(stored)
	entry.DesiredStatus = "" // hand-written archive omitting the field

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip — an omitted field is not a change", item.Action)
	}
}

func TestPlanMergeNeverDeletes(t *testing.T) {
	stored := sampleSpec()
	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("other")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	for _, item := range plan.Items {
		if item.Action == ActionDelete {
			t.Fatalf("merge produced a delete for %q", item.Name)
		}
	}
}

func TestPlanReplaceDeletesTunnelsAbsentFromArchive(t *testing.T) {
	stored := sampleSpec()
	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("other")), testOptions(ModeReplace))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	item := itemFor(t, plan, "prod-db")
	if item.Action != ActionDelete {
		t.Fatalf("got action %q, want delete", item.Action)
	}
	if item.ID != stored.ID {
		t.Errorf("got delete ID %q, want %q", item.ID, stored.ID)
	}
	if item.Spec != nil {
		t.Error("a delete item must not carry a spec")
	}
}

func TestPlanReusesFreeArchiveID(t *testing.T) {
	entry := validEntry("staging-api")
	entry.ID = "id-from-the-backup"

	plan, err := Plan(nil, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "staging-api"); item.Spec.ID != "id-from-the-backup" {
		t.Fatalf("got ID %q, want the archive's ID reused for a faithful restore", item.Spec.ID)
	}
}

func TestPlanRegeneratesCollidingArchiveID(t *testing.T) {
	stored := sampleSpec() // name prod-db, ID 9f1c...

	entry := validEntry("staging-api")
	entry.ID = stored.ID // same ID, different name

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	item := itemFor(t, plan, "staging-api")
	if item.Spec.ID == stored.ID {
		t.Fatal("a colliding ID must be regenerated, not allowed to clobber an unrelated tunnel")
	}
	if item.Spec.ID != "generated-id-1" {
		t.Errorf("got ID %q, want generated-id-1", item.Spec.ID)
	}
}

func TestPlanMintsIDWhenArchiveHasNone(t *testing.T) {
	plan, err := Plan(nil, archiveOf(validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "staging-api"); item.Spec.ID != "generated-id-1" {
		t.Fatalf("got ID %q, want generated-id-1", item.Spec.ID)
	}
}

func TestPlanRejectsInvalidArchive(t *testing.T) {
	bad := archiveOf(validEntry("a"), validEntry("a")) // duplicate names
	_, err := Plan(nil, bad, testOptions(ModeMerge))
	if err == nil {
		t.Fatal("expected an error for an invalid archive, got nil")
	}
	var invalid ArchiveInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want ArchiveInvalidError", err)
	}
	if len(invalid.Errors) == 0 {
		t.Error("ArchiveInvalidError must carry the underlying entry errors")
	}
}

// TestPlanDoesNotMutateItsInputs re-runs Plan against the same unchanged
// inputs and checks round 2 is identical to round 1. Plan is pure, so this
// proves Plan does not mutate current or archive as a side effect of
// planning — it does NOT by itself prove end-to-end idempotence of an actual
// restore (that requires Apply, which is Task 5's responsibility).
func TestPlanDoesNotMutateItsInputs(t *testing.T) {
	stored := []*types.TunnelSpec{sampleSpec()}
	archive := archiveOf(EntryFromSpec(stored[0]))

	for round := 1; round <= 2; round++ {
		plan, err := Plan(stored, archive, testOptions(ModeReplace))
		if err != nil {
			t.Fatalf("round %d: Plan returned error: %v", round, err)
		}
		for _, item := range plan.Items {
			if item.Action != ActionSkip {
				t.Fatalf("round %d: got action %q for %q, want skip — Plan must not mutate its inputs across calls",
					round, item.Action, item.Name)
			}
		}
	}
}

func TestPlanEmptyArchiveInMergeModeChangesNothing(t *testing.T) {
	plan, err := Plan([]*types.TunnelSpec{sampleSpec()}, archiveOf(), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Items) != 0 {
		t.Fatalf("got %d items, want 0", len(plan.Items))
	}
}

func TestParseMode(t *testing.T) {
	for input, want := range map[string]Mode{"": ModeMerge, "merge": ModeMerge, "replace": ModeReplace} {
		got, err := ParseMode(input)
		if err != nil {
			t.Errorf("ParseMode(%q) returned error: %v", input, err)
		}
		if got != want {
			t.Errorf("ParseMode(%q) = %q, want %q", input, got, want)
		}
	}
	if _, err := ParseMode("obliterate"); err == nil {
		t.Error("expected an error for an unknown mode, got nil")
	}
}

func TestPlanNilArchiveReturnsArchiveInvalidErrorNotPanic(t *testing.T) {
	_, err := Plan(nil, nil, testOptions(ModeMerge))
	if err == nil {
		t.Fatal("expected an error for a nil archive, got nil")
	}
	var invalid ArchiveInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("got %T, want ArchiveInvalidError", err)
	}
	if len(invalid.Errors) == 0 {
		t.Error("ArchiveInvalidError must carry the underlying entry errors")
	}
}

func TestPlanTrimsNameSoWhitespaceMatchesStoredTunnel(t *testing.T) {
	stored := sampleSpec()
	entry := EntryFromSpec(stored)
	entry.Name = "  " + entry.Name + "  " // hand-edited archive with stray whitespace

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("got %d items, want 1 — untrimmed whitespace must match the stored tunnel, not create a second one", len(plan.Items))
	}
	item := itemFor(t, plan, "prod-db")
	if item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip — untrimmed whitespace around an otherwise-identical name must still match", item.Action)
	}
	if item.ID != stored.ID {
		t.Errorf("got ID %q, want the stored ID %q — untrimmed whitespace must not churn identity", item.ID, stored.ID)
	}
}

func TestPlanSkipsWhenBothSidesOmitLocalBindAddress(t *testing.T) {
	stored := sampleSpec()
	stored.LocalBindAddress = ""

	entry := EntryFromSpec(stored)
	entry.LocalBindAddress = "" // hand-written archive omitting the field

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if item := itemFor(t, plan, "prod-db"); item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip — an omitted local_bind_address must normalize the same way on both sides, not read as a change", item.Action)
	}
}

func TestPlanTwoNewEntriesWithSameArchiveIDRegeneratesSecond(t *testing.T) {
	a := validEntry("staging-api")
	a.ID = "dup-id"
	b := validEntry("staging-worker")
	b.ID = "dup-id"

	plan, err := Plan(nil, archiveOf(a, b), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	first := itemFor(t, plan, "staging-api")
	second := itemFor(t, plan, "staging-worker")
	if first.Spec.ID != "dup-id" {
		t.Errorf("got first ID %q, want dup-id — the first claimant keeps the archive's ID", first.Spec.ID)
	}
	if second.Spec.ID == "dup-id" {
		t.Fatal("second entry must not collide with the first entry's ID")
	}
	if second.Spec.ID != "generated-id-1" {
		t.Errorf("got second ID %q, want generated-id-1", second.Spec.ID)
	}
}

func TestPlanLaterEntryCollidingWithMintedIDRegenerates(t *testing.T) {
	a := validEntry("staging-api") // no ID -> mints "generated-id-1"
	b := validEntry("staging-worker")
	b.ID = "generated-id-1" // collides with what 'a' is about to mint

	plan, err := Plan(nil, archiveOf(a, b), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	first := itemFor(t, plan, "staging-api")
	second := itemFor(t, plan, "staging-worker")
	if first.Spec.ID != "generated-id-1" {
		t.Fatalf("got first ID %q, want generated-id-1", first.Spec.ID)
	}
	if second.Spec.ID == first.Spec.ID {
		t.Fatal("second entry's explicit ID collided with a minted ID and must be regenerated, not allowed to clobber it")
	}
	if second.Spec.ID != "generated-id-2" {
		t.Errorf("got second ID %q, want generated-id-2", second.Spec.ID)
	}
}

// TestPlanRoundTripsWhitespacePaddedNameAsSkip pins the regression the
// reviewer caught: a stored tunnel whose Name is not trimmed (internal/api/validation.go's
// SanitizeString strips only null and control bytes, not whitespace, so
// "prod-db " with a trailing space can genuinely be stored) must still match
// its own, now-trimmed, exported archive entry. Before the fix, byName was
// keyed on the raw stored Name while the archive-derived spec had already
// been trimmed, so an UNMODIFIED export-then-replace round trip produced a
// create+delete pair instead of a single skip — silently reassigning the
// tunnel's ID on every restore.
func TestPlanRoundTripsWhitespacePaddedNameAsSkip(t *testing.T) {
	stored := sampleSpec()
	stored.Name = "prod-db " // trailing space, as SanitizeString would allow

	entry := EntryFromSpec(stored) // trims to "prod-db"

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(entry), testOptions(ModeReplace))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}
	if len(plan.Items) != 1 {
		t.Fatalf("got %d items, want 1 — an unmodified round trip of a whitespace-padded name must not create+delete", len(plan.Items))
	}
	item := plan.Items[0]
	if item.Action != ActionSkip {
		t.Fatalf("got action %q, want skip", item.Action)
	}
	if item.ID != stored.ID {
		t.Errorf("got ID %q, want the stored ID %q — identity must not churn", item.ID, stored.ID)
	}
}

// TestPlanFallsBackToUUIDWhenNewIDNeverFreesUp guards against the ID-mint
// loop hanging forever. NewID is an injection point; a test double or future
// caller that always returns the same already-used ID must not stall Plan,
// since Plan takes no context and has no cancellation path. The goroutine and
// timeout below make a would-be hang fail fast instead of blocking the test
// suite.
func TestPlanFallsBackToUUIDWhenNewIDNeverFreesUp(t *testing.T) {
	existing := sampleSpec()
	existing.ID = "collision-only-id"

	entry := validEntry("staging-api") // no ID of its own; Plan must mint one
	opts := PlanOptions{
		Mode:         ModeMerge,
		DefaultOwner: "importer",
		NewID:        func() string { return "collision-only-id" }, // never free
		Now:          fixedClock(time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)),
	}

	type result struct {
		plan *ImportPlan
		err  error
	}
	done := make(chan result, 1)
	go func() {
		plan, err := Plan([]*types.TunnelSpec{existing}, archiveOf(entry), opts)
		done <- result{plan, err}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("Plan returned error: %v", r.err)
		}
		item := itemFor(t, r.plan, "staging-api")
		if item.Spec.ID == "" {
			t.Fatal("expected a minted ID, got empty")
		}
		if item.Spec.ID == "collision-only-id" {
			t.Fatal("Plan must not accept an ID that a pathological NewID keeps colliding with")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Plan did not return within 2s — the ID-mint loop appears to be unbounded")
	}
}

// TestPlanRefusesWhenStoredNamesCollideAfterTrimming pins the gap the
// reviewer found in the trim fix itself: the database's UNIQUE(name)
// constraint compares raw strings, so two DISTINCT stored rows — "prod-db"
// and "prod-db " — can both exist and collide onto the same trimmed byName
// key. Before this fix, the second row silently overwrote the first in the
// map, and one of the two tunnels vanished from the plan entirely: not
// created, updated, skipped, or deleted, just absent. There is no correct
// tunnel to prefer, so Plan must refuse to run rather than guess — and it
// must report this as a problem with the stored data, not the archive file,
// so callers don't mislabel a perfectly valid archive as invalid.
func TestPlanRefusesWhenStoredNamesCollideAfterTrimming(t *testing.T) {
	a := sampleSpec()
	a.Name = "prod-db"
	a.ID = "id-a"

	b := sampleSpec()
	b.Name = "prod-db " // trailing space: a distinct row under UNIQUE(name)
	b.ID = "id-b"

	entry := validEntry("prod-db")

	plan, err := Plan([]*types.TunnelSpec{a, b}, archiveOf(entry), testOptions(ModeReplace))
	if err == nil {
		t.Fatal("expected an error for colliding stored names, got nil")
	}
	if plan != nil {
		t.Fatal("expected a nil plan — no caller should be able to act on a partial diff")
	}

	var invalid ArchiveInvalidError
	if errors.As(err, &invalid) {
		t.Fatal("got ArchiveInvalidError — this is a problem with the stored data, not the archive file, and must not be reported as an invalid archive")
	}

	msg := err.Error()
	if !strings.Contains(msg, `"prod-db"`) || !strings.Contains(msg, `"prod-db "`) {
		t.Errorf("error message must name both conflicting tunnels, got %q", msg)
	}
}
