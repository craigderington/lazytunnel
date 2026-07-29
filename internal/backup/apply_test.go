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

	// Verify the good tunnel actually persisted to the store, not just reported.
	if store.saves != 1 {
		t.Errorf("got %d saves, want 1 — report self-reporting is not enough", store.saves)
	}
	spec, exists := store.specs[store.order[0]]
	if !exists {
		t.Fatal("the good tunnel is not in store.specs")
	}
	if spec.Name != "good" {
		t.Errorf("got stored tunnel name %q, want good", spec.Name)
	}
}

func TestNewDryRunReportWritesNothing(t *testing.T) {
	stored := sampleSpec()

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

	// Verify the report faithfully renders the plan item by item.
	if len(report.Items) != len(plan.Items) {
		t.Errorf("got %d report items, want %d (same as plan)", len(report.Items), len(plan.Items))
	}
	for i, reportItem := range report.Items {
		planItem := plan.Items[i]
		if reportItem.Action != planItem.Action {
			t.Errorf("item %d: got action %q, want %q", i, reportItem.Action, planItem.Action)
		}
		if reportItem.Name != planItem.Name {
			t.Errorf("item %d: got name %q, want %q", i, reportItem.Name, planItem.Name)
		}
		if reportItem.ID != planItem.ID {
			t.Errorf("item %d: got ID %q, want %q", i, reportItem.ID, planItem.ID)
		}
	}
}

func TestApplyThenReplanConverges(t *testing.T) {
	// End-to-end idempotence: plan → apply → re-plan against the mutated store
	// must skip everything. This is the property the whole merge design rests on.
	// Tests both ModeMerge and ModeReplace.
	archive := archiveOf(validEntry("a"), validEntry("b"))

	for _, mode := range []Mode{ModeMerge, ModeReplace} {
		t.Run(string(mode), func(t *testing.T) {
			// Phase 1: plan and apply against empty store.
			store := newFakeStore()
			plan1, err := Plan(nil, archive, testOptions(mode))
			if err != nil {
				t.Fatalf("Plan returned error: %v", err)
			}

			report1, err := Apply(context.Background(), store, plan1)
			if err != nil {
				t.Fatalf("Apply returned error: %v", err)
			}
			if report1.Created != 2 {
				t.Fatalf("expected 2 creates, got %d", report1.Created)
			}

			// Phase 2: re-read the mutated store and re-plan.
			current, err := store.List(context.Background())
			if err != nil {
				t.Fatalf("store.List returned error: %v", err)
			}

			plan2, err := Plan(current, archive, testOptions(mode))
			if err != nil {
				t.Fatalf("second Plan returned error: %v", err)
			}

			// Phase 3: every item in the second plan must be ActionSkip.
			for _, item := range plan2.Items {
				if item.Action != ActionSkip {
					t.Errorf("after apply, %q has action %q, want skip — idempotence violated", item.Name, item.Action)
				}
			}
		})
	}
}
