package backup

import (
	"errors"
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

func TestPlanOfUnchangedArchiveIsAllSkip(t *testing.T) {
	stored := []*types.TunnelSpec{sampleSpec()}
	archive := archiveOf(EntryFromSpec(stored[0]))

	for round := 1; round <= 2; round++ {
		plan, err := Plan(stored, archive, testOptions(ModeReplace))
		if err != nil {
			t.Fatalf("round %d: Plan returned error: %v", round, err)
		}
		for _, item := range plan.Items {
			if item.Action != ActionSkip {
				t.Fatalf("round %d: got action %q for %q, want skip — import must be idempotent",
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
