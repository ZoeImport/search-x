package profilepool

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"web-search-backend/internal/domain"
)

type testProfile struct {
	id       string
	provider domain.ProviderName
	capacity int
	closed   atomic.Bool
}

func (profile *testProfile) ID() string                    { return profile.id }
func (profile *testProfile) Provider() domain.ProviderName { return profile.provider }
func (profile *testProfile) Capacity() int                 { return profile.capacity }
func (profile *testProfile) Close() error                  { profile.closed.Store(true); return nil }
func (profile *testProfile) Search(context.Context, domain.SearchRequest) (domain.SearchResponse, error) {
	return domain.SearchResponse{Provider: profile.provider}, nil
}

func TestPoolEnforcesCapacityAndReleasesExactlyOnce(t *testing.T) {
	pool := mustPool(t, &testProfile{id: "baidu-1", provider: domain.ProviderNameBaidu, capacity: 1})
	lease, ok := pool.TryAcquire("request-1")
	if !ok {
		t.Fatal("expected first lease")
	}
	if _, ok := pool.TryAcquire("request-2"); ok {
		t.Fatal("capacity exceeded")
	}
	lease.Release(Result{Succeeded: true})
	lease.Release(Result{Succeeded: true})
	if got := pool.Snapshot(); got.InFlight != 0 || got.Profiles[0].EffectiveSamples != 1 {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestPoolAcquireWakesAfterRelease(t *testing.T) {
	pool := mustPool(t, &testProfile{id: "bing-1", provider: domain.ProviderNameBing, capacity: 1})
	first, _ := pool.TryAcquire("first")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan *Lease, 1)
	go func() {
		lease, _ := pool.Acquire(ctx, "second")
		result <- lease
	}()
	first.Release(Result{Succeeded: true})
	select {
	case lease := <-result:
		if lease == nil {
			t.Fatal("expected waiting lease")
		}
		lease.Release(Result{Succeeded: true})
	case <-time.After(time.Second):
		t.Fatal("waiting acquire did not wake")
	}
}

func TestPoolPromotesProbationProfileToTrusted(t *testing.T) {
	pool, err := New([]Profile{&testProfile{id: "ddg-1", provider: domain.ProviderNameDuckDuckGo, capacity: 1}}, Config{
		Trust: TrustConfig{MinSamples: 2, MinSuccessRate: 0.70, MinRecentEWMA: 0.70, RecentAlpha: 0.2},
	})
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		lease, ok := pool.TryAcquire("request")
		if !ok {
			t.Fatal("expected lease")
		}
		lease.Release(Result{Succeeded: true})
	}
	if got := pool.Snapshot().Profiles[0].State; got != StateTrusted {
		t.Fatalf("state=%s", got)
	}
}

func TestPoolQuarantinesCaptchaProfile(t *testing.T) {
	pool := mustPool(t, &testProfile{id: "brave-1", provider: domain.ProviderNameBrave, capacity: 1})
	lease, _ := pool.TryAcquire("request")
	lease.Release(Result{Classification: domain.ClassificationCaptcha})
	if got := pool.Snapshot(); got.Profiles[0].State != StateQuarantined || got.AvailableSlots != 0 {
		t.Fatalf("snapshot=%+v", got)
	}
}

func TestPoolDoesNotChargeProviderMarkupChangeToProfileTrust(t *testing.T) {
	pool := mustPool(t, &testProfile{id: "bing-1", provider: domain.ProviderNameBing, capacity: 1})
	lease, _ := pool.TryAcquire("request")
	lease.Release(Result{Classification: domain.ClassificationParseChanged})
	got := pool.Snapshot().Profiles[0]
	if got.EffectiveSamples != 0 || got.ConsecutiveFailures != 0 || got.State != StateProbation {
		t.Fatalf("profile=%+v", got)
	}
}

func TestPoolRejectsMixedProviders(t *testing.T) {
	_, err := New([]Profile{
		&testProfile{id: "one", provider: domain.ProviderNameBaidu, capacity: 1},
		&testProfile{id: "two", provider: domain.ProviderNameBing, capacity: 1},
	}, Config{})
	if err == nil {
		t.Fatal("expected mixed provider error")
	}
}

func TestPoolManifestRestoresTrustedOnlyAfterActivate(t *testing.T) {
	store, err := NewManifestStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	profile := &testProfile{id: "bing-persisted", provider: domain.ProviderNameBing, capacity: 1}
	first, err := New([]Profile{profile}, Config{Manifest: store, Trust: TrustConfig{MinSamples: 2, MinSuccessRate: 0.7, MinRecentEWMA: 0.7}})
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Activate(); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 2; index++ {
		lease, ok := first.TryAcquire("promote")
		if !ok {
			t.Fatal("expected lease")
		}
		lease.Release(Result{Succeeded: true})
	}
	second, err := New([]Profile{profile}, Config{Manifest: store})
	if err != nil {
		t.Fatal(err)
	}
	if state := second.Snapshot().Profiles[0].State; state != StateWarming {
		t.Fatalf("before activate=%s", state)
	}
	if _, ok := second.TryAcquire("warming"); ok {
		t.Fatal("warming profile served traffic")
	}
	if err := second.Activate(); err != nil {
		t.Fatal(err)
	}
	got := second.Snapshot().Profiles[0]
	if got.State != StateTrusted || got.EffectiveSamples != 2 || got.Generation != 2 {
		t.Fatalf("restored=%+v", got)
	}
}

func TestQuarantinedProfileReturnsToProbationAfterCooldown(t *testing.T) {
	now := time.Unix(100, 0)
	pool, err := New([]Profile{&testProfile{id: "baidu-cooldown", provider: domain.ProviderNameBaidu, capacity: 1}}, Config{Now: func() time.Time { return now }, QuarantineCooldown: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	lease, _ := pool.TryAcquire("captcha")
	lease.Release(Result{Classification: domain.ClassificationCaptcha})
	if _, ok := pool.TryAcquire("during-cooldown"); ok {
		t.Fatal("quarantined profile acquired")
	}
	now = now.Add(time.Minute)
	recovered, ok := pool.TryAcquire("after-cooldown")
	if !ok {
		t.Fatal("profile did not recover")
	}
	recovered.Release(Result{Succeeded: true})
}

func TestManifestTrustDecaysAcrossRestart(t *testing.T) {
	root := t.TempDir()
	store, err := NewManifestStore(root)
	if err != nil {
		t.Fatal(err)
	}
	updated := time.Unix(100, 0)
	if err := store.Save(Manifest{ProfileID: "aged", Provider: string(domain.ProviderNameBing), DesiredState: StateTrusted, EffectiveSamples: 100, SuccessWeight: 90, FailureWeight: 10, RecentEWMA: 1, Generation: 1, UpdatedAt: updated}); err != nil {
		t.Fatal(err)
	}
	pool, err := New([]Profile{&testProfile{id: "aged", provider: domain.ProviderNameBing, capacity: 1}}, Config{Manifest: store, Now: func() time.Time { return updated.Add(7 * 24 * time.Hour) }, Trust: TrustConfig{DecayHalfLife: 7 * 24 * time.Hour}})
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.Activate(); err != nil {
		t.Fatal(err)
	}
	got := pool.Snapshot().Profiles[0]
	if got.EffectiveSamples != 50 || got.SuccessWeight < 44.9 || got.SuccessWeight > 45.1 || got.RecentEWMA < 0.74 || got.RecentEWMA > 0.76 {
		t.Fatalf("decayed=%+v", got)
	}
}

func TestPoolYieldsToAutoAfterThreeExplicitLeases(t *testing.T) {
	pool := mustPool(t, &testProfile{id: "fair", provider: domain.ProviderNameBaidu, capacity: 1})
	for index := 0; index < 3; index++ {
		lease, err := pool.Acquire(context.Background(), "explicit")
		if err != nil {
			t.Fatal(err)
		}
		lease.Release(Result{Succeeded: true})
	}
	held, _ := pool.TryAcquire("held")
	autoGot := make(chan *Lease, 1)
	go func() { lease, _ := pool.AcquireAuto(context.Background(), "auto"); autoGot <- lease }()
	deadline := time.Now().Add(time.Second)
	for pool.AutoWaiters() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	explicitGot := make(chan *Lease, 1)
	go func() { lease, _ := pool.Acquire(context.Background(), "explicit-four"); explicitGot <- lease }()
	held.Release(Result{Succeeded: true})
	select {
	case lease := <-autoGot:
		if lease == nil {
			t.Fatal("auto lease nil")
		}
		lease.Release(Result{Succeeded: true})
	case <-explicitGot:
		t.Fatal("fourth explicit lease bypassed waiting auto")
	case <-time.After(time.Second):
		t.Fatal("fairness wait timed out")
	}
	select {
	case lease := <-explicitGot:
		lease.Release(Result{Succeeded: true})
	case <-time.After(time.Second):
		t.Fatal("explicit did not resume")
	}
}

func TestManagerDrainsInflightProfileAndCreatesReplacement(t *testing.T) {
	original := &testProfile{id: "replaceable", provider: domain.ProviderNameBing, capacity: 1}
	pool := mustPool(t, original)
	lease, _ := pool.TryAcquire("inflight")
	if err := pool.Drain("replaceable"); err != nil {
		t.Fatal(err)
	}
	if _, ok := pool.TryAcquire("new-work"); ok {
		t.Fatal("draining profile accepted work")
	}
	lease.Release(Result{Succeeded: true})
	manager := NewManager(pool, func(retired Snapshot) (Profile, error) {
		return &testProfile{id: retired.ID, provider: retired.Provider, capacity: retired.Capacity}, nil
	})
	if err := manager.Reconcile(); err != nil {
		t.Fatal(err)
	}
	got := pool.Snapshot().Profiles[0]
	if got.State != StateProbation || got.Generation != 2 || !original.closed.Load() {
		t.Fatalf("replacement=%+v closed=%v", got, original.closed.Load())
	}
}

func TestSameRequestKeepsProfileForLeaseLifetime(t *testing.T) {
	pool := mustPool(t,
		&testProfile{id: "sticky-a", provider: domain.ProviderNameBing, capacity: 1},
		&testProfile{id: "sticky-b", provider: domain.ProviderNameBing, capacity: 1},
	)
	lease, ok := pool.TryAcquire("same-request")
	if !ok {
		t.Fatal("expected lease")
	}
	before := lease.ProfileID()
	_, err := lease.Search(context.Background(), domain.SearchRequest{RequestID: "same-request"})
	if err != nil {
		t.Fatal(err)
	}
	if lease.ProfileID() != before {
		t.Fatalf("profile changed from %s to %s", before, lease.ProfileID())
	}
	lease.Release(Result{Succeeded: true})
}

func TestSnapshotAndTryAcquireAreRaceSafe(t *testing.T) {
	pool := mustPool(t,
		&testProfile{id: "race-a", provider: domain.ProviderNameBrave, capacity: 2},
		&testProfile{id: "race-b", provider: domain.ProviderNameBrave, capacity: 2},
	)
	done := make(chan struct{})
	for worker := 0; worker < 8; worker++ {
		go func() {
			for index := 0; index < 500; index++ {
				if lease, ok := pool.TryAcquire("race"); ok {
					lease.Release(Result{Succeeded: true})
				}
				_ = pool.Snapshot()
			}
			done <- struct{}{}
		}()
	}
	for range 8 {
		<-done
	}
	if snapshot := pool.Snapshot(); snapshot.InFlight != 0 || snapshot.AvailableSlots != 4 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
}

func TestTrustedProfileDegradesAfterConsecutiveFailures(t *testing.T) {
	pool, err := New([]Profile{&testProfile{id: "trust", provider: domain.ProviderNameDuckDuckGo, capacity: 1}}, Config{Trust: TrustConfig{MinSamples: 2, MinSuccessRate: 0.7, MinRecentEWMA: 0.7, MaxConsecutiveFailures: 3}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		lease, _ := pool.TryAcquire("success")
		lease.Release(Result{Succeeded: true})
	}
	for range 3 {
		lease, ok := pool.TryAcquire("failure")
		if !ok {
			t.Fatal("lease unavailable before degradation")
		}
		lease.Release(Result{Classification: domain.ClassificationNetworkError})
	}
	if state := pool.Snapshot().Profiles[0].State; state != StateDegraded {
		t.Fatalf("state=%s", state)
	}
}

func TestTrustedAndProbationProfilesReceiveConfiguredTrafficSplit(t *testing.T) {
	pool, err := New([]Profile{
		&testProfile{id: "a-trusted", provider: domain.ProviderNameDuckDuckGo, capacity: 1},
		&testProfile{id: "b-probation", provider: domain.ProviderNameDuckDuckGo, capacity: 1},
	}, Config{TrustedTrafficPercent: 80, Trust: TrustConfig{MinSamples: 2, MinSuccessRate: 0.7, MinRecentEWMA: 0.7}})
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		lease, _ := pool.TryAcquire("promote")
		lease.Release(Result{Succeeded: true})
	}
	counts := map[string]int{}
	for index := 0; index < 1000; index++ {
		lease, ok := pool.TryAcquire(fmt.Sprintf("traffic-%d", index))
		if !ok {
			t.Fatal("expected lease")
		}
		counts[lease.ProfileID()]++
		lease.Release(Result{Classification: domain.ClassificationParseChanged})
	}
	if counts["a-trusted"] < 750 || counts["a-trusted"] > 850 {
		t.Fatalf("counts=%v", counts)
	}
}

func mustPool(t *testing.T, profiles ...Profile) *Pool {
	t.Helper()
	pool, err := New(profiles, Config{})
	if err != nil {
		t.Fatal(err)
	}
	return pool
}
