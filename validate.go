package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const maxIDLen = 200

// parseEvent validates and normalises one raw array element.
// Normalisation: IDs are whitespace-trimmed, type is lower-cased and trimmed,
// timestamp is parsed as RFC 3339 and converted to UTC.
func parseEvent(raw json.RawMessage) (Event, error) {
	var w wireEvent
	if err := json.Unmarshal(raw, &w); err != nil {
		return Event{}, fmt.Errorf("malformed event: %v", err)
	}
	e := Event{
		EventID:    strings.TrimSpace(w.EventID),
		CampaignID: strings.TrimSpace(w.CampaignID),
		ContactID:  strings.TrimSpace(w.ContactID),
		Type:       strings.ToLower(strings.TrimSpace(w.Type)),
		Metadata:   w.Metadata,
	}
	for name, v := range map[string]string{"event_id": e.EventID, "campaign_id": e.CampaignID, "contact_id": e.ContactID} {
		if v == "" {
			return Event{}, fmt.Errorf("missing or empty %s", name)
		}
		if len(v) > maxIDLen {
			return Event{}, fmt.Errorf("%s too long", name)
		}
	}
	if !validTypes[e.Type] {
		return Event{}, fmt.Errorf("unknown type %q", w.Type)
	}
	if w.Timestamp == "" {
		return Event{}, fmt.Errorf("missing timestamp")
	}
	ts, err := time.Parse(time.RFC3339, w.Timestamp)
	if err != nil {
		return Event{}, fmt.Errorf("invalid timestamp %q (want RFC 3339)", w.Timestamp)
	}
	e.Timestamp = ts.UTC()
	return e, nil
}
