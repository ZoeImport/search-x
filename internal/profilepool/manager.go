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
	if manager == nil || manager.pool == nil || manager.factory == nil {
		return fmt.Errorf("profile manager dependencies are nil")
	}
	for _, snapshot := range manager.pool.Snapshot().Profiles {
		if snapshot.State != StateRetired {
			continue
		}
		replacement, err := manager.factory(snapshot)
		if err != nil {
			return err
		}
		if err := manager.pool.replaceRetired(snapshot.ID, replacement); err != nil {
			return err
		}
	}
	return nil
}
