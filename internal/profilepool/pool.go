package profilepool

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"sync"
	"time"

	"web-search-backend/internal/domain"
)

// Config controls profile selection and trust progression.
type Config struct {
	TrustedTrafficPercent int
	Trust                 TrustConfig
	Now                   func() time.Time
	Manifest              *ManifestStore
	QuarantineCooldown    time.Duration
	DegradedCooldown      time.Duration
	MaxRecoveryAttempts   int
	Limiter               interface{ Wait(context.Context) error }
}

type entry struct {
	profile             Profile
	state               State
	inFlight            int
	effectiveSamples    int
	successWeight       float64
	failureWeight       float64
	recentEWMA          float64
	consecutiveFailures int
	generation          uint64
	recoveryState       State
	quarantinedUntil    time.Time
	recoveryAttempts    int
}

// Pool schedules leases across profiles owned by one provider.
type Pool struct {
	provider domain.ProviderName
	config   Config

	mu              sync.Mutex
	profiles        []*entry
	byID            map[string]*entry
	notify          chan struct{}
	closed          bool
	autoWaiters     int
	explicitStreak  int
	manifestHealthy bool
}

// New validates homogeneous profiles and creates one provider pool.
func New(profiles []Profile, config Config) (*Pool, error) {
	if len(profiles) == 0 {
		return nil, fmt.Errorf("profile pool is empty")
	}
	if config.TrustedTrafficPercent <= 0 || config.TrustedTrafficPercent >= 100 {
		config.TrustedTrafficPercent = 80
	}
	config.Trust = config.Trust.normalized()
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.QuarantineCooldown <= 0 {
		config.QuarantineCooldown = 5 * time.Minute
	}
	if config.DegradedCooldown <= 0 {
		config.DegradedCooldown = time.Minute
	}
	if config.MaxRecoveryAttempts <= 0 {
		config.MaxRecoveryAttempts = 3
	}
	providerName := profiles[0].Provider()
	if providerName == "" || profiles[0].ID() == "" || profiles[0].Capacity() <= 0 {
		return nil, fmt.Errorf("profile 0 is invalid")
	}
	pool := &Pool{
		provider: providerName, config: config, byID: make(map[string]*entry, len(profiles)),
		notify: make(chan struct{}), manifestHealthy: true,
	}
	for index, profile := range profiles {
		if profile == nil {
			return nil, fmt.Errorf("profile %d is nil", index)
		}
		if profile.Provider() != providerName {
			return nil, fmt.Errorf("profile %q provider %q differs from pool provider %q", profile.ID(), profile.Provider(), providerName)
		}
		if profile.ID() == "" || profile.Capacity() <= 0 {
			return nil, fmt.Errorf("profile %d is invalid", index)
		}
		if _, exists := pool.byID[profile.ID()]; exists {
			return nil, fmt.Errorf("duplicate profile id %q", profile.ID())
		}
		value := &entry{profile: profile, state: StateProbation, recoveryState: StateProbation, recentEWMA: 1, generation: 1}
		if config.Manifest != nil {
			value.state = StateWarming
			manifest, exists, loadErr := config.Manifest.Load(profile.ID())
			if loadErr != nil {
				return nil, fmt.Errorf("load profile %q manifest: %w", profile.ID(), loadErr)
			}
			if exists {
				age := config.Now().Sub(manifest.UpdatedAt)
				factor := 1.0
				if age > 0 {
					factor = math.Exp(-math.Ln2 * float64(age) / float64(config.Trust.DecayHalfLife))
				}
				value.effectiveSamples, value.successWeight, value.failureWeight = manifest.EffectiveSamples, manifest.SuccessWeight, manifest.FailureWeight
				value.successWeight *= factor
				value.failureWeight *= factor
				value.effectiveSamples = int(math.Round(float64(value.effectiveSamples) * factor))
				value.recentEWMA = 0.5 + (manifest.RecentEWMA-0.5)*factor
				value.consecutiveFailures = manifest.ConsecutiveFailures
				value.generation, value.quarantinedUntil, value.recoveryAttempts = manifest.Generation+1, manifest.QuarantinedUntil, manifest.RecoveryAttempts
				if manifest.DesiredState == StateTrusted {
					value.recoveryState = StateTrusted
				}
			}
		}
		pool.profiles = append(pool.profiles, value)
		pool.byID[profile.ID()] = value
	}
	return pool, nil
}

// Activate completes startup warming after provider construction succeeds.
func (pool *Pool) Activate() error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	for _, value := range pool.profiles {
		if value.state != StateWarming {
			continue
		}
		value.state = value.recoveryState
		if err := pool.saveManifest(value); err != nil {
			return err
		}
	}
	pool.signal()
	return nil
}

// Drain removes a profile from new scheduling and retires it after in-flight leases finish.
func (pool *Pool) Drain(profileID string) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	value, ok := pool.byID[profileID]
	if !ok {
		return fmt.Errorf("profile %q not found", profileID)
	}
	value.state = StateDraining
	if value.inFlight == 0 {
		value.state = StateRetired
	}
	if err := pool.saveManifest(value); err != nil {
		return err
	}
	pool.signal()
	return nil
}

func (pool *Pool) replaceRetired(profileID string, replacement Profile) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	value, ok := pool.byID[profileID]
	if !ok || value.state != StateRetired {
		return fmt.Errorf("profile %q is not retired", profileID)
	}
	if replacement == nil || replacement.ID() != profileID || replacement.Provider() != pool.provider || replacement.Capacity() <= 0 {
		return fmt.Errorf("replacement profile %q is invalid", profileID)
	}
	if err := value.profile.Close(); err != nil {
		return err
	}
	value.profile = replacement
	value.state, value.recoveryState = StateProbation, StateProbation
	value.inFlight, value.effectiveSamples = 0, 0
	value.successWeight, value.failureWeight, value.recentEWMA = 0, 0, 1
	value.consecutiveFailures, value.recoveryAttempts = 0, 0
	value.generation++
	if err := pool.saveManifest(value); err != nil {
		return err
	}
	pool.signal()
	return nil
}

func (pool *Pool) recreate(profileID string) (Profile, error) {
	pool.mu.Lock()
	value, ok := pool.byID[profileID]
	pool.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("profile %q not found", profileID)
	}
	factory, ok := value.profile.(interface{ Recreate() (Profile, error) })
	if !ok {
		return nil, fmt.Errorf("profile %q is not recreatable", profileID)
	}
	return factory.Recreate()
}

// Provider returns the single provider represented by the pool.
func (pool *Pool) Provider() domain.ProviderName { return pool.provider }

// TryAcquire atomically selects a serving profile and reserves one slot.
func (pool *Pool) TryAcquire(requestID string) (*Lease, bool) {
	return pool.tryAcquire(requestID, false, false, "")
}

// TryAcquireAuto reserves a slot for automatic routing and resets explicit preference streak.
func (pool *Pool) TryAcquireAuto(requestID string) (*Lease, bool) {
	return pool.tryAcquire(requestID, true, false, "")
}

// TryAcquireAutoExcept reserves a serving profile other than excludedProfileID.
func (pool *Pool) TryAcquireAutoExcept(requestID, excludedProfileID string) (*Lease, bool) {
	return pool.tryAcquire(requestID, true, false, excludedProfileID)
}

func (pool *Pool) tryAcquire(requestID string, automatic, explicit bool, excludedProfileID string) (*Lease, bool) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	if pool.closed || !pool.manifestHealthy {
		return nil, false
	}
	if explicit && pool.autoWaiters > 0 && pool.explicitStreak >= 3 {
		return nil, false
	}
	pool.reconcileLocked()
	preferred := StateProbation
	if bucket(requestID) < pool.config.TrustedTrafficPercent {
		preferred = StateTrusted
	}
	selected := pool.selectEntry(preferred, excludedProfileID)
	if selected == nil {
		fallback := StateTrusted
		if preferred == StateTrusted {
			fallback = StateProbation
		}
		selected = pool.selectEntry(fallback, excludedProfileID)
	}
	if selected == nil {
		return nil, false
	}
	selected.inFlight++
	if automatic {
		pool.explicitStreak = 0
	} else if explicit {
		pool.explicitStreak++
	}
	return &Lease{pool: pool, entry: selected, acquiredAt: pool.config.Now()}, true
}

func (pool *Pool) reconcileLocked() {
	now := pool.config.Now()
	for _, value := range pool.profiles {
		if (value.state == StateQuarantined || value.state == StateDegraded) && !value.quarantinedUntil.After(now) {
			if value.recoveryAttempts >= pool.config.MaxRecoveryAttempts {
				value.state = StateDraining
				if value.inFlight == 0 {
					value.state = StateRetired
				}
				continue
			}
			value.recoveryAttempts++
			value.state = StateProbation
			value.consecutiveFailures = 0
		}
	}
}

func (pool *Pool) saveManifest(value *entry) error {
	if pool.config.Manifest == nil {
		return nil
	}
	err := pool.config.Manifest.Save(Manifest{ProfileID: value.profile.ID(), Provider: string(value.profile.Provider()), DesiredState: value.state, EffectiveSamples: value.effectiveSamples, SuccessWeight: value.successWeight, FailureWeight: value.failureWeight, RecentEWMA: value.recentEWMA, ConsecutiveFailures: value.consecutiveFailures, Generation: value.generation, QuarantinedUntil: value.quarantinedUntil, RecoveryAttempts: value.recoveryAttempts, UpdatedAt: pool.config.Now()})
	pool.manifestHealthy = err == nil
	return err
}

// Acquire waits for a profile slot or context cancellation.
func (pool *Pool) Acquire(ctx context.Context, requestID string) (*Lease, error) {
	for {
		if lease, ok := pool.tryAcquire(requestID, false, true, ""); ok {
			return lease, nil
		}
		pool.mu.Lock()
		notification := pool.notify
		pool.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for %s profile lease: %w", pool.provider, ctx.Err())
		case <-notification:
		}
	}
}

// AcquireAuto waits as an auto-route waiter, participating in 3:1 fairness.
func (pool *Pool) AcquireAuto(ctx context.Context, requestID string) (*Lease, error) {
	pool.mu.Lock()
	pool.autoWaiters++
	pool.mu.Unlock()
	defer func() { pool.mu.Lock(); pool.autoWaiters--; pool.mu.Unlock() }()
	for {
		if lease, ok := pool.tryAcquire(requestID, true, false, ""); ok {
			return lease, nil
		}
		pool.mu.Lock()
		notification := pool.notify
		pool.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("wait for %s auto profile lease: %w", pool.provider, ctx.Err())
		case <-notification:
		}
	}
}

func (pool *Pool) AutoWaiters() int { pool.mu.Lock(); defer pool.mu.Unlock(); return pool.autoWaiters }

// WaitAvailable waits until pool capacity or lifecycle state changes.
func (pool *Pool) WaitAvailable(ctx context.Context) error {
	pool.mu.Lock()
	pool.autoWaiters++
	notification := pool.notify
	pool.mu.Unlock()
	defer func() { pool.mu.Lock(); pool.autoWaiters--; pool.mu.Unlock() }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-notification:
		return nil
	}
}

func (pool *Pool) selectEntry(state State, excludedProfileID string) *entry {
	var selected *entry
	for _, candidate := range pool.profiles {
		if candidate.profile.ID() == excludedProfileID || candidate.state != state || candidate.inFlight >= candidate.profile.Capacity() {
			continue
		}
		if selected == nil || lessLoaded(candidate, selected) {
			selected = candidate
		}
	}
	return selected
}

func lessLoaded(left, right *entry) bool {
	leftLoad := float64(left.inFlight) / float64(left.profile.Capacity())
	rightLoad := float64(right.inFlight) / float64(right.profile.Capacity())
	if leftLoad != rightLoad {
		return leftLoad < rightLoad
	}
	return left.profile.ID() < right.profile.ID()
}

func bucket(key string) int {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(key))
	return int(hash.Sum32() % 100)
}

// Snapshot returns a consistent provider and profile view.
func (pool *Pool) Snapshot() ProviderSnapshot {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	snapshot := ProviderSnapshot{Provider: pool.provider, ProfileCount: len(pool.profiles), Profiles: make([]Snapshot, 0, len(pool.profiles))}
	for _, value := range pool.profiles {
		serving := pool.manifestHealthy && (value.state == StateProbation || value.state == StateTrusted)
		if serving {
			snapshot.ServingCount++
			snapshot.AvailableSlots += value.profile.Capacity() - value.inFlight
		}
		snapshot.InFlight += value.inFlight
		snapshot.Profiles = append(snapshot.Profiles, snapshotEntry(value))
	}
	return snapshot
}

// Close prevents new leases and closes idle profile resources.
func (pool *Pool) Close() error {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.closed = true
	var firstErr error
	for _, value := range pool.profiles {
		value.state = StateRetired
		if err := value.profile.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	pool.signal()
	return firstErr
}

func (pool *Pool) signal() {
	close(pool.notify)
	pool.notify = make(chan struct{})
}

func snapshotEntry(value *entry) Snapshot {
	return Snapshot{
		ID: value.profile.ID(), Provider: value.profile.Provider(), State: value.state,
		Capacity: value.profile.Capacity(), InFlight: value.inFlight,
		EffectiveSamples: value.effectiveSamples, SuccessWeight: value.successWeight,
		FailureWeight: value.failureWeight, RecentEWMA: value.recentEWMA,
		ConsecutiveFailures: value.consecutiveFailures, Generation: value.generation,
	}
}
