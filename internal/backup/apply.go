package backup

import (
	"context"
	"fmt"
)

// ItemResult records what happened to one planned item.
type ItemResult struct {
	Action Action `json:"action"`
	Name   string `json:"name"`
	ID     string `json:"id"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Report is the outcome of an import. A dry-run report carries the intended
// actions with no Error fields set.
type Report struct {
	Mode    Mode         `json:"mode"`
	DryRun  bool         `json:"dry_run"`
	Items   []ItemResult `json:"items"`
	Created int          `json:"created"`
	Updated int          `json:"updated"`
	Skipped int          `json:"skipped"`
	Deleted int          `json:"deleted"`
	Failed  int          `json:"failed"`
}

func (r *Report) add(item ItemResult) {
	r.Items = append(r.Items, item)

	if item.Error != "" {
		r.Failed++
		return
	}

	switch item.Action {
	case ActionCreate:
		r.Created++
	case ActionUpdate:
		r.Updated++
	case ActionSkip:
		r.Skipped++
	case ActionDelete:
		r.Deleted++
	}
}

// NewDryRunReport renders a plan as a report without touching storage.
func NewDryRunReport(plan *ImportPlan) *Report {
	report := &Report{Mode: plan.Mode, DryRun: true, Items: []ItemResult{}}
	for _, item := range plan.Items {
		report.add(ItemResult{
			Action: item.Action,
			Name:   item.Name,
			ID:     item.ID,
			Reason: item.Reason,
		})
	}
	return report
}

// Apply writes a plan to storage.
//
// Plan has already validated the whole archive, so most failures are caught
// before anything is written. tunnel.Storage exposes no transaction API, so
// this is validate-then-write rather than rollback: if a write fails partway,
// the report names exactly which items landed and Apply returns an error
// alongside it. Merge is idempotent, so re-running converges.
func Apply(ctx context.Context, store Store, plan *ImportPlan) (*Report, error) {
	report := &Report{Mode: plan.Mode, Items: []ItemResult{}}
	var firstErr error

	for _, item := range plan.Items {
		result := ItemResult{
			Action: item.Action,
			Name:   item.Name,
			ID:     item.ID,
			Reason: item.Reason,
		}

		var err error
		switch item.Action {
		case ActionCreate, ActionUpdate:
			err = store.Save(ctx, item.Spec)
		case ActionDelete:
			err = store.Delete(ctx, item.ID)
		case ActionSkip:
			// Nothing to write.
		}

		if err != nil {
			result.Error = err.Error()
			if firstErr == nil {
				firstErr = fmt.Errorf("failed to %s tunnel %q: %w", item.Action, item.Name, err)
			}
		}

		report.add(result)
	}

	return report, firstErr
}
