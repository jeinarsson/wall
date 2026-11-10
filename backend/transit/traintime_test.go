package transit

import (
	"testing"
	"time"
)

func TestParseArrivals(t *testing.T) {
	// Trimmed from https://backend-unified.mylirr.org/arrivals/FHL
	b := []byte(`{"banners":[],"alerts":[],"arrivals":[
		{"time":1794276840,"stops":["NYK"],"direction":"W","branch":"HH","track":"3",
		 "status":{"otp":-119,"otp_location":"KGN","held":false,"canceled":false},
		 "train_id":"LIRR_2026-11-09_1579","train_num":"1579","stop_status":"BERTHED"},
		{"time":1794278340,"stops":["KGN","JAM"],"direction":"E","track":"4",
		 "status":{"held":false,"canceled":true},
		 "train_id":"LIRR_2026-11-09_2786","train_num":"2786","stop_status":"EN_ROUTE"}]}`)
	got, err := parseArrivals(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d arrivals, want 2", len(got))
	}
	a := got[0]
	if a.TrainNum != "1579" || a.Direction != "W" || !a.Scheduled.Equal(time.Unix(1794276840, 0)) ||
		a.Status != "BERTHED" || a.Track != "3" || len(a.Stops) != 1 || a.Stops[0] != "NYK" || a.Canceled {
		t.Errorf("got %+v", a)
	}
	if a.Late == nil || *a.Late != 119*time.Second {
		t.Errorf("late = %v, want 1m59s", a.Late)
	}
	if b := got[1]; b.Late != nil || !b.Canceled {
		t.Errorf("got %+v, want canceled and not started", b)
	}
}
