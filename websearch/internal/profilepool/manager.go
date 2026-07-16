package profilepool

import "fmt"

type ReplacementFactory func(Snapshot) (Profile, error)

// Manager reconciles retired identities without interrupting in-flight work.
type Manager struct {
	pool    *Pool
	factory ReplacementFactory
}

func NewManager(pool *Pool, factory ReplacementFactory) *Manager {
	return &Manager{pool: pool, factory: factory}
}

func (manager *Manager) Reconcile() error {
	if manager == nil || manager.pool == nil {
		return fmt.Errorf("profile manager dependencies are nil")
	}
	for _, snapshot := range manager.pool.Snapshot().Profiles {
		if snapshot.State != StateRetired {
			continue
		}
		var replacement Profile
		var err error
		if manager.factory != nil {
			replacement, err = manager.factory(snapshot)
		} else {
			replacement, err = manager.pool.recreate(snapshot.ID)
		}
		if err != nil {
			return err
		}
		if err := manager.pool.replaceRetired(snapshot.ID, replacement); err != nil {
			return err
		}
	}
	return nil
}
