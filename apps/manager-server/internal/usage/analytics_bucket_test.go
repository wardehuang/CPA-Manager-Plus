package usage

import (
	"testing"
	"time"
)

func TestAnalyticsBucketMSAcrossDST(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	tests := []struct {
		name        string
		timestampMS int64
		granularity string
		wantMS      int64
	}{
		{
			name:        "hour before spring transition",
			timestampMS: time.Date(2026, time.March, 8, 6, 30, 0, 0, time.UTC).UnixMilli(),
			granularity: "hour",
			wantMS:      time.Date(2026, time.March, 8, 6, 0, 0, 0, time.UTC).UnixMilli(),
		},
		{
			name:        "hour after spring transition",
			timestampMS: time.Date(2026, time.March, 8, 7, 30, 0, 0, time.UTC).UnixMilli(),
			granularity: "hour",
			wantMS:      time.Date(2026, time.March, 8, 7, 0, 0, 0, time.UTC).UnixMilli(),
		},
		{
			name:        "local day",
			timestampMS: time.Date(2026, time.March, 8, 18, 0, 0, 0, time.UTC).UnixMilli(),
			granularity: "day",
			wantMS:      time.Date(2026, time.March, 8, 5, 0, 0, 0, time.UTC).UnixMilli(),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AnalyticsBucketMS(test.timestampMS, test.granularity, location); got != test.wantMS {
				t.Fatalf("bucket = %d, want %d", got, test.wantMS)
			}
		})
	}
}
