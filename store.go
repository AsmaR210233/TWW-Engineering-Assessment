package main

import (
	"sort"
	"sync"
	"time"
)

type Outcome int

const (
	Accepted  Outcome = iota // new event stored and counted
	Duplicate                // same event_id, same content: ignored
	Conflict                 // same event_id, different content: first write wins
)

type campaign struct {
	counts   map[string]int                 // type -> number of distinct events
	contacts map[string]map[string]struct{} // type -> distinct contact_ids
	events   []*Event
	first    time.Time
	last     time.Time
}

// Store is an in-memory event store. One RWMutex guards everything; the
// dedup check and the counter update happen in the same critical section,
// so concurrent retries of the same event cannot both be counted.
type Store struct {
	mu        sync.RWMutex
	events    map[string]*Event // event_id -> event (global identity)
	campaigns map[string]*campaign
}

func NewStore() *Store {
	return &Store{events: map[string]*Event{}, campaigns: map[string]*campaign{}}
}

func sameContent(a, b *Event) bool {
	return a.CampaignID == b.CampaignID && a.ContactID == b.ContactID &&
		a.Type == b.Type && a.Timestamp.Equal(b.Timestamp)
}

// Add inserts e unless its event_id is already known.
func (s *Store) Add(e Event) Outcome {
	s.mu.Lock()
	defer s.mu.Unlock()
	if old, ok := s.events[e.EventID]; ok {
		if sameContent(old, &e) {
			return Duplicate
		}
		return Conflict
	}
	ev := e
	s.events[e.EventID] = &ev
	c := s.campaigns[e.CampaignID]
	if c == nil {
		c = &campaign{counts: map[string]int{}, contacts: map[string]map[string]struct{}{}}
		s.campaigns[e.CampaignID] = c
	}
	c.counts[e.Type]++
	if c.contacts[e.Type] == nil {
		c.contacts[e.Type] = map[string]struct{}{}
	}
	c.contacts[e.Type][e.ContactID] = struct{}{}
	c.events = append(c.events, &ev)
	if c.first.IsZero() || e.Timestamp.Before(c.first) {
		c.first = e.Timestamp
	}
	if e.Timestamp.After(c.last) {
		c.last = e.Timestamp
	}
	return Accepted
}

type Stats struct {
	CampaignID     string              `json:"campaign_id"`
	Totals         map[string]int      `json:"total_events"`
	UniqueContacts map[string]int      `json:"unique_contacts"`
	Rates          map[string]*float64 `json:"rates"`
	FirstEventAt   time.Time           `json:"first_event_at"`
	LastEventAt    time.Time           `json:"last_event_at"`
	AsOf           time.Time           `json:"as_of"`
}

func ratio(n, d int) *float64 {
	if d == 0 {
		return nil
	}
	r := float64(n) / float64(d)
	return &r
}

// GetStats returns false if the campaign has never been seen.
func (s *Store) GetStats(id string) (Stats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.campaigns[id]
	if c == nil {
		return Stats{}, false
	}
	st := Stats{CampaignID: id, Totals: map[string]int{}, UniqueContacts: map[string]int{},
		FirstEventAt: c.first, LastEventAt: c.last, AsOf: time.Now().UTC()}
	for _, t := range typeOrder {
		st.Totals[t] = c.counts[t]
		st.UniqueContacts[t] = len(c.contacts[t])
	}
	u := st.UniqueContacts
	st.Rates = map[string]*float64{
		"delivered_per_sent":    ratio(u["delivered"], u["sent"]),
		"opened_per_delivered":  ratio(u["opened"], u["delivered"]),
		"clicked_per_delivered": ratio(u["clicked"], u["delivered"]),
	}
	return st, true
}

// ListEvents returns events newest-first (by provider timestamp, then
// event_id), with offset/limit paging. The bool is false for unknown campaigns.
func (s *Store) ListEvents(id string, offset, limit int) ([]Event, int, bool) {
	s.mu.RLock()
	c := s.campaigns[id]
	if c == nil {
		s.mu.RUnlock()
		return nil, 0, false
	}
	all := make([]*Event, len(c.events))
	copy(all, c.events)
	s.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool {
		if !all[i].Timestamp.Equal(all[j].Timestamp) {
			return all[i].Timestamp.After(all[j].Timestamp)
		}
		return all[i].EventID < all[j].EventID
	})
	total := len(all)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	out := make([]Event, 0, end-offset)
	for _, e := range all[offset:end] {
		out = append(out, *e)
	}
	return out, total, true
}
