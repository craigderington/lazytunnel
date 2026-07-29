package backup

import (
	"context"
	"errors"
	"testing"

	"github.com/craigderington/lazytunnel/pkg/types"
)

func TestApplyWritesCreatesAndUpdates(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	changed := EntryFromSpec(stored)
	changed.LocalPort = 15432

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(changed, validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1", report.Created)
	}
	if report.Updated != 1 {
		t.Errorf("got Updated %d, want 1", report.Updated)
	}
	if store.saves != 2 {
		t.Errorf("got %d saves, want 2", store.saves)
	}
}

func TestApplySkipDoesNotWrite(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(EntryFromSpec(stored)), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Skipped != 1 {
		t.Errorf("got Skipped %d, want 1", report.Skipped)
	}
	if store.saves != 0 {
		t.Errorf("got %d saves, want 0 — a skip must not touch storage", store.saves)
	}
}

func TestApplyReplaceDeletes(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("other")), testOptions(ModeReplace))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err != nil {
		t.Fatalf("Apply returned error: %v", err)
	}
	if report.Deleted != 1 {
		t.Errorf("got Deleted %d, want 1", report.Deleted)
	}
	if store.deletes != 1 {
		t.Errorf("got %d deletes, want 1", store.deletes)
	}
}

func TestApplyReportsWhichItemsLandedOnPartialFailure(t *testing.T) {
	// Storage has no transaction API, so this is validate-then-write, not
	// rollback. The report must name exactly what succeeded.
	store := newFakeStore()
	store.saveErr["bad"] = errors.New("disk on fire")

	good := validEntry("good")
	bad := validEntry("bad")

	plan, err := Plan(nil, archiveOf(good, bad), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report, err := Apply(context.Background(), store, plan)
	if err == nil {
		t.Fatal("expected an error when a write fails, got nil")
	}
	if report == nil {
		t.Fatal("Apply must return the report even on failure")
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1 — the good tunnel did land", report.Created)
	}
	if report.Failed != 1 {
		t.Errorf("got Failed %d, want 1", report.Failed)
	}

	var failed *ItemResult
	for i := range report.Items {
		if report.Items[i].Name == "bad" {
			failed = &report.Items[i]
		}
	}
	if failed == nil {
		t.Fatal("report has no item for the failing tunnel")
	}
	if failed.Error == "" {
		t.Error("the failing item must carry its error message")
	}
}

func TestNewDryRunReportWritesNothing(t *testing.T) {
	stored := sampleSpec()
	store := newFakeStore(stored)

	plan, err := Plan([]*types.TunnelSpec{stored}, archiveOf(validEntry("staging-api")), testOptions(ModeMerge))
	if err != nil {
		t.Fatalf("Plan returned error: %v", err)
	}

	report := NewDryRunReport(plan)
	if !report.DryRun {
		t.Error("dry-run report must be flagged as such")
	}
	if report.Created != 1 {
		t.Errorf("got Created %d, want 1", report.Created)
	}
	if store.saves != 0 || store.deletes != 0 {
		t.Errorf("dry run touched storage: %d saves, %d deletes", store.saves, store.deletes)
	}
}
