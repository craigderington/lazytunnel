package backup

import (
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// Mode selects how an import treats tunnels that are stored but absent from
// the archive.
type Mode string

const (
	// ModeMerge updates and creates, and never deletes.
	ModeMerge Mode = "merge"
	// ModeReplace additionally deletes stored tunnels absent from the archive,
	// so the fleet ends up mirroring the file exactly.
	ModeReplace Mode = "replace"
)

// ParseMode maps a ?mode= query value onto a Mode, defaulting to merge.
func ParseMode(s string) (Mode, error) {
	switch s {
	case "", string(ModeMerge):
		return ModeMerge, nil
	case string(ModeReplace):
		return ModeReplace, nil
	default:
		return "", fmt.Errorf("unknown import mode %q, expected merge or replace", s)
	}
}

// Action is what an import will do to a single tunnel.
type Action string

const (
	ActionCreate Action = "create"
	ActionUpdate Action = "update"
	ActionSkip   Action = "skip"
	ActionDelete Action = "delete"
)

// PlanItem is one tunnel's worth of intended change.
type PlanItem struct {
	Action Action            `json:"action"`
	Name   string            `json:"name"`
	ID     string            `json:"id"`
	Reason string            `json:"reason,omitempty"`
	Spec   *types.TunnelSpec `json:"-"` // nil for skip and delete
}

// ImportPlan is the full set of intended changes.
type ImportPlan struct {
	Mode  Mode       `json:"mode"`
	Items []PlanItem `json:"items"`
}

// PlanOptions carries the identity inputs Plan cannot derive from the archive.
type PlanOptions struct {
	Mode Mode
	// DefaultOwner is applied to created tunnels whose entry has no owner.
	DefaultOwner string
	// NewID mints IDs for created tunnels. Injected for deterministic tests.
	NewID func() string
	// Now stamps CreatedAt and UpdatedAt. Injected for deterministic tests.
	Now Clock
}

// ArchiveInvalidError reports every validation failure in a single value.
type ArchiveInvalidError struct {
	Errors []EntryError
}

func (e ArchiveInvalidError) Error() string {
	if len(e.Errors) == 0 {
		return "archive is invalid"
	}
	return fmt.Sprintf("archive is invalid: %d problem(s), first: %s", len(e.Errors), e.Errors[0].Error())
}

// Plan diffs an archive against the currently stored tunnels.
//
// It performs no I/O, so every rule here is unit-testable without a database.
// Tunnels are matched on name — the UNIQUE column in the tunnels table — and a
// matched tunnel keeps its stored ID, Owner and CreatedAt, because a restore
// must not reassign ownership or churn the IDs the web UI's saved ordering
// depends on.
func Plan(current []*types.TunnelSpec, archive *Archive, opts PlanOptions) (*ImportPlan, error) {
	if errs := ValidateArchive(archive); len(errs) > 0 {
		return nil, ArchiveInvalidError{Errors: errs}
	}

	if opts.Mode == "" {
		opts.Mode = ModeMerge
	}
	if opts.NewID == nil {
		opts.NewID = func() string { return uuid.New().String() }
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.DefaultOwner == "" {
		opts.DefaultOwner = "api-user"
	}

	byName := make(map[string]*types.TunnelSpec, len(current))
	usedIDs := make(map[string]bool, len(current))
	for _, spec := range current {
		byName[spec.Name] = spec
		usedIDs[spec.ID] = true
	}

	plan := &ImportPlan{Mode: opts.Mode, Items: []PlanItem{}}
	matched := make(map[string]bool, len(archive.Tunnels))

	for _, entry := range archive.Tunnels {
		spec := SpecFromEntry(entry)
		// SpecFromEntry trims the name, so matching against the stored specs
		// (also keyed on their, already-trimmed, Name) agrees with
		// ValidateArchive's trimmed empty/length/duplicate checks by
		// construction. Matching on the raw, untrimmed entry.Name would let
		// " prod-db" and "prod-db" pass validation as distinct names yet
		// silently churn the same tunnel's identity.
		existing, found := byName[spec.Name]

		if !found {
			id := entry.ID
			for id == "" || usedIDs[id] {
				id = opts.NewID()
			}
			usedIDs[id] = true

			spec.ID = id
			if spec.Owner == "" {
				spec.Owner = opts.DefaultOwner
			}
			spec.CreatedAt = opts.Now().UTC()
			spec.UpdatedAt = spec.CreatedAt

			plan.Items = append(plan.Items, PlanItem{
				Action: ActionCreate,
				Name:   spec.Name,
				ID:     spec.ID,
				Spec:   spec,
			})
			continue
		}

		matched[spec.Name] = true

		// An existing tunnel keeps its identity across a restore.
		spec.ID = existing.ID
		spec.Owner = existing.Owner
		spec.CreatedAt = existing.CreatedAt

		if entriesEqual(entry, existing) {
			plan.Items = append(plan.Items, PlanItem{
				Action: ActionSkip,
				Name:   spec.Name,
				ID:     spec.ID,
				Reason: "identical to stored tunnel",
			})
			continue
		}

		spec.UpdatedAt = opts.Now().UTC()
		plan.Items = append(plan.Items, PlanItem{
			Action: ActionUpdate,
			Name:   spec.Name,
			ID:     spec.ID,
			Spec:   spec,
		})
	}

	if opts.Mode == ModeReplace {
		for _, spec := range current {
			if matched[spec.Name] {
				continue
			}
			plan.Items = append(plan.Items, PlanItem{
				Action: ActionDelete,
				Name:   spec.Name,
				ID:     spec.ID,
				Reason: "not present in archive",
			})
		}
	}

	return plan, nil
}

// entriesEqual reports whether an archive entry describes exactly what is
// already stored.
//
// Both sides are round-tripped through SpecFromEntry and EntryFromSpec so that
// representation differences — a nil versus empty hop slice, an omitted
// desired_status — never register as a change. ID and Owner are zeroed because
// Plan resolves them from the stored tunnel rather than the file. Everything
// else, desired_status included, counts: a tunnel whose only change is stopped
// to active must not be skipped.
func entriesEqual(entry TunnelEntry, stored *types.TunnelSpec) bool {
	a := EntryFromSpec(SpecFromEntry(entry))
	b := EntryFromSpec(stored)
	a.ID, b.ID = "", ""
	a.Owner, b.Owner = "", ""
	return reflect.DeepEqual(a, b)
}
