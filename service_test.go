package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

func post(t *testing.T, h http.Handler, body string) (int, ingestResponse) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/events", strings.NewReader(body)))
	var resp ingestResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	return rec.Code, resp
}

func stats(t *testing.T, h http.Handler, id string) Stats {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/campaigns/"+id+"/stats", nil))
	if rec.Code != 200 {
		t.Fatalf("stats status %d", rec.Code)
	}
	var st Stats
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func ev(id, contact, typ string) string {
	return `{"event_id":"` + id + `","campaign_id":"c1","contact_id":"` + contact + `","type":"` + typ + `","timestamp":"2026-08-10T09:00:00Z"}`
}

func newH() http.Handler { return newMux(&server{store: NewStore()}) }

func TestDuplicateCountsOnce(t *testing.T) {
	h := newH()
	post(t, h, "["+ev("e1", "a", "sent")+"]")
	_, r := post(t, h, "["+ev("e1", "a", "sent")+","+ev("e1", "a", "sent")+"]")
	if r.Duplicates != 2 || r.Accepted != 0 {
		t.Fatalf("got %+v", r)
	}
	if got := stats(t, h, "c1").Totals["sent"]; got != 1 {
		t.Fatalf("sent=%d want 1", got)
	}
}

func TestMixedBatchKeepsGoodEvents(t *testing.T) {
	h := newH()
	body := "[" + ev("e1", "a", "sent") + `,{"event_id":5},"junk",null,` + ev("e2", "b", "bogus") + "," + ev("e3", "c", "delivered") + "]"
	code, r := post(t, h, body)
	if code != 200 || r.Accepted != 2 || len(r.Rejected) != 4 {
		t.Fatalf("code=%d resp=%+v", code, r)
	}
	st := stats(t, h, "c1")
	if st.Totals["sent"] != 1 || st.Totals["delivered"] != 1 {
		t.Fatalf("%+v", st.Totals)
	}
}

func TestNotAnArrayIs400(t *testing.T) {
	if code, _ := post(t, newH(), `{"event_id":"x"}`); code != 400 {
		t.Fatalf("code %d", code)
	}
}

func TestOpenedBeforeDeliveredIsCounted(t *testing.T) {
	h := newH()
	post(t, h, "["+ev("e1", "a", "opened")+"]")
	st := stats(t, h, "c1")
	if st.Totals["opened"] != 1 || st.Totals["delivered"] != 0 {
		t.Fatalf("%+v", st.Totals)
	}
	post(t, h, "["+ev("e2", "a", "delivered")+"]")
	if stats(t, h, "c1").Totals["opened"] != 1 {
		t.Fatal("late delivered changed opened")
	}
}

func TestUniqueVsTotalOpens(t *testing.T) {
	h := newH()
	post(t, h, "["+ev("e1", "a", "opened")+","+ev("e2", "a", "opened")+","+ev("e3", "b", "opened")+"]")
	st := stats(t, h, "c1")
	if st.Totals["opened"] != 3 || st.UniqueContacts["opened"] != 2 {
		t.Fatalf("%+v %+v", st.Totals, st.UniqueContacts)
	}
}

func TestConflictingReuseKeepsFirst(t *testing.T) {
	h := newH()
	post(t, h, "["+ev("e1", "a", "sent")+"]")
	_, r := post(t, h, "["+ev("e1", "a", "clicked")+"]")
	if r.Conflicts != 1 {
		t.Fatalf("%+v", r)
	}
	st := stats(t, h, "c1")
	if st.Totals["sent"] != 1 || st.Totals["clicked"] != 0 {
		t.Fatalf("%+v", st.Totals)
	}
}

func TestTypeCaseNormalisedAndTimezoneToUTC(t *testing.T) {
	h := newH()
	_, r := post(t, h, `[{"event_id":"e1","campaign_id":"c1","contact_id":"a","type":"OPENED","timestamp":"2026-08-10T12:00:00+05:30"}]`)
	if r.Accepted != 1 {
		t.Fatalf("%+v", r)
	}
	if st := stats(t, h, "c1"); st.UniqueContacts["opened"] != 1 || st.FirstEventAt.Hour() != 6 {
		t.Fatalf("%+v", st)
	}
}

// Run with -race. 50 goroutines all deliver the same 20 events.
func TestConcurrentDuplicatesCountOnce(t *testing.T) {
	h := newH()
	var parts []string
	for i := 0; i < 20; i++ {
		parts = append(parts, ev("e"+string(rune('A'+i)), "a"+string(rune('A'+i)), "sent"))
	}
	body := "[" + strings.Join(parts, ",") + "]"
	var wg sync.WaitGroup
	for g := 0; g < 50; g++ {
		wg.Add(1)
		go func() { defer wg.Done(); post(t, h, body) }()
	}
	wg.Wait()
	if got := stats(t, h, "c1").Totals["sent"]; got != 20 {
		t.Fatalf("sent=%d want 20", got)
	}
}

func TestSeedFileSurvives(t *testing.T) {
	data, err := os.ReadFile("seed/events.json")
	if err != nil {
		t.Skip("no seed")
	}
	h := newH()
	code, r := post(t, h, string(data))
	if code != 200 || r.Received != 235 {
		t.Fatalf("code=%d %+v", code, r)
	}
	t.Logf("seed: %+v", r)
	// loading it again must change nothing
	before := stats(t, h, "cmp_summer_sale").Totals
	_, r2 := post(t, h, string(data))
	if r2.Accepted != 0 {
		t.Fatalf("replay accepted %d", r2.Accepted)
	}
	after := stats(t, h, "cmp_summer_sale").Totals
	for k, v := range before {
		if after[k] != v {
			t.Fatalf("replay changed %s", k)
		}
	}
}
