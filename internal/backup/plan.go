package backup

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// maxIDMintAttempts bounds how many times Plan will call opts.NewID looking
// for a free ID before giving up on it and falling back to uuid.New, which is
// collision-free by construction. NewID is an injection point: without a
// bound, a test double or future caller that keeps returning an already-used
// (or empty) ID would hang Plan forever, and Plan takes no context to cancel
// on.
const maxIDMintAttempts = 10

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

	// byName and matched are keyed on the TRIMMED name throughout, because
	// EntryFromSpec/SpecFromEntry trim the archive side (see below), and a
	// stored tunnel's Name is not guaranteed trimmed: internal/api/validation.go's
	// SanitizeString strips only null and control bytes, not whitespace, so a
	// tunnel genuinely named "prod-db " (trailing space) can exist today. If
	// this map were keyed on the raw stored Name, an unmodified round trip of
	// that exact tunnel would fail to match its own trimmed archive entry and
	// churn its identity — a delete+recreate in replace mode, a spurious
	// duplicate in merge mode.
	byName := make(map[string]*types.TunnelSpec, len(current))
	usedIDs := make(map[string]bool, len(current))
	for _, spec := range current {
		byName[strings.TrimSpace(spec.Name)] = spec
		usedIDs[spec.ID] = true
	}

	plan := &ImportPlan{Mode: opts.Mode, Items: []PlanItem{}}
	matched := make(map[string]bool, len(archive.Tunnels))

	for _, entry := range archive.Tunnels {
		spec := SpecFromEntry(entry)
		// spec.Name is already trimmed by SpecFromEntry, and byName is keyed
		// on the trimmed stored name (see above), so this lookup agrees with
		// ValidateArchive's trimmed checks by construction.
		existing, found := byName[spec.Name]

		if !found {
			id := entry.ID
			for attempts := 0; (id == "" || usedIDs[id]) && attempts < maxIDMintAttempts; attempts++ {
				id = opts.NewID()
			}
			if id == "" || usedIDs[id] {
				// The injected NewID exhausted its attempts without producing
				// a free ID. Fall back to a real UUID, which cannot collide.
				id = uuid.New().String()
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
			if matched[strings.TrimSpace(spec.Name)] {
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
