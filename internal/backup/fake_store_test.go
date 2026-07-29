package backup

import (
	"context"
	"fmt"

	"github.com/craigderington/lazytunnel/pkg/types"
)

// fakeStore is an in-memory Store for tests. Insertion order is preserved so
// List is deterministic.
type fakeStore struct {
	order   []string
	specs   map[string]*types.TunnelSpec
	saveErr map[string]error // by tunnel name
	delErr  map[string]error // by tunnel ID
	saves   int
	deletes int
}

func newFakeStore(specs ...*types.TunnelSpec) *fakeStore {
	f := &fakeStore{
		specs:   make(map[string]*types.TunnelSpec, len(specs)),
		saveErr: make(map[string]error),
		delErr:  make(map[string]error),
	}
	for _, s := range specs {
		f.order = append(f.order, s.ID)
		f.specs[s.ID] = s
	}
	return f
}

func (f *fakeStore) List(ctx context.Context) ([]*types.TunnelSpec, error) {
	out := make([]*types.TunnelSpec, 0, len(f.order))
	for _, id := range f.order {
		if spec, ok := f.specs[id]; ok {
			out = append(out, spec)
		}
	}
	return out, nil
}

func (f *fakeStore) Save(ctx context.Context, spec *types.TunnelSpec) error {
	if err, ok := f.saveErr[spec.Name]; ok {
		return err
	}
	f.saves++
	if _, exists := f.specs[spec.ID]; !exists {
		f.order = append(f.order, spec.ID)
	}
	f.specs[spec.ID] = spec
	return nil
}

func (f *fakeStore) Delete(ctx context.Context, id string) error {
	if err, ok := f.delErr[id]; ok {
		return err
	}
	if _, exists := f.specs[id]; !exists {
		return fmt.Errorf("tunnel not found: %s", id)
	}
	f.deletes++
	delete(f.specs, id)
	for i, existing := range f.order {
		if existing == id {
			f.order = append(f.order[:i], f.order[i+1:]...)
			break
		}
	}
	return nil
}
