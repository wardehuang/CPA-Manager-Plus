package wxaiinspection

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/model"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/repository/sqlite"
	"github.com/seakee/cpa-manager-plus/apps/manager-server/internal/store"
)

func newRealtimeHealthyTestService(t *testing.T) *Service {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "usage.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return New(store.New(db), nil)
}

func seedRealtimeDegradationState(t *testing.T, service *Service, count int, cooldownUntilMS int64) RealtimeHealthyRequest {
	t.Helper()
	request := RealtimeHealthyRequest{
		AccountKey:      "xai-test.json|test@example.com|auth-index|",
		FileName:        "xai-test.json",
		AuthIndex:       "auth-index",
		CurrentPriority: intPointer(wxaiNormalizedPriorityValue),
	}
	if err := service.store.UpsertWxaiRealtimeDegradationState(context.Background(), model.WxaiRealtimeDegradationState{
		AccountKey:       request.AccountKey,
		FileName:         request.FileName,
		DisplayAccount:   "test@example.com",
		AuthIndex:        request.AuthIndex,
		DegradationCount: count,
		CooldownUntilMS:  cooldownUntilMS,
		CreatedAtMS:      1,
		UpdatedAtMS:      1,
	}); err != nil {
		t.Fatalf("seed degradation state: %v", err)
	}
	return request
}

func assertRealtimeDegradationExists(t *testing.T, service *Service, accountKey string, wantExists bool) {
	t.Helper()
	_, exists, err := service.store.GetWxaiRealtimeDegradationState(context.Background(), accountKey)
	if err != nil {
		t.Fatalf("get degradation state: %v", err)
	}
	if exists != wantExists {
		t.Fatalf("degradation state exists = %t, want %t", exists, wantExists)
	}
}

func TestRecordRealtimeHealthyClearsCountAfterCooldownWhenPriorityNormalized(t *testing.T) {
	service := newRealtimeHealthyTestService(t)
	request := seedRealtimeDegradationState(t, service, 2, time.Now().Add(-time.Minute).UnixMilli())

	cleared, err := service.RecordRealtimeHealthy(context.Background(), request)
	if err != nil {
		t.Fatalf("RecordRealtimeHealthy: %v", err)
	}
	if !cleared {
		t.Fatal("expected consecutive count to clear")
	}
	assertRealtimeDegradationExists(t, service, request.AccountKey, false)
}

func TestRecordRealtimeHealthyKeepsCountDuringCooldown(t *testing.T) {
	service := newRealtimeHealthyTestService(t)
	request := seedRealtimeDegradationState(t, service, 1, time.Now().Add(time.Hour).UnixMilli())

	cleared, err := service.RecordRealtimeHealthy(context.Background(), request)
	if err != nil {
		t.Fatalf("RecordRealtimeHealthy: %v", err)
	}
	if cleared {
		t.Fatal("expected consecutive count to stay during cooldown")
	}
	assertRealtimeDegradationExists(t, service, request.AccountKey, true)
}

func TestRecordRealtimeHealthyKeepsCountWhenPriorityNotNormalized(t *testing.T) {
	service := newRealtimeHealthyTestService(t)
	request := seedRealtimeDegradationState(t, service, 1, time.Now().Add(-time.Minute).UnixMilli())
	request.CurrentPriority = intPointer(wxaiPositionDegradedPriorityValue)

	cleared, err := service.RecordRealtimeHealthy(context.Background(), request)
	if err != nil {
		t.Fatalf("RecordRealtimeHealthy: %v", err)
	}
	if cleared {
		t.Fatal("expected consecutive count to stay while priority is not 1")
	}
	assertRealtimeDegradationExists(t, service, request.AccountKey, true)
}

func TestRecordRealtimeHealthyClearsTerminalCountWhenPriorityNormalized(t *testing.T) {
	service := newRealtimeHealthyTestService(t)
	request := seedRealtimeDegradationState(t, service, wxaiTerminalRealtimeDegradationCount, 0)

	cleared, err := service.RecordRealtimeHealthy(context.Background(), request)
	if err != nil {
		t.Fatalf("RecordRealtimeHealthy: %v", err)
	}
	if !cleared {
		t.Fatal("expected terminal count to clear after priority returns to 1")
	}
	assertRealtimeDegradationExists(t, service, request.AccountKey, false)
}

func TestRecordRealtimeHealthyNoStateDoesNotClear(t *testing.T) {
	service := newRealtimeHealthyTestService(t)
	cleared, err := service.RecordRealtimeHealthy(context.Background(), RealtimeHealthyRequest{
		AccountKey:      "missing-key",
		CurrentPriority: intPointer(wxaiNormalizedPriorityValue),
	})
	if err != nil {
		t.Fatalf("RecordRealtimeHealthy: %v", err)
	}
	if cleared {
		t.Fatal("expected missing state to leave cleared=false")
	}
}
