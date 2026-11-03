package main

import "time"

// Event is a validated, normalised event as stored.
type Event struct {
	EventID    string         `json:"event_id"`
	CampaignID string         `json:"campaign_id"`
	ContactID  string         `json:"contact_id"`
	Type       string         `json:"type"`
	Timestamp  time.Time      `json:"timestamp"` // UTC, when it happened at the provider
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// wireEvent is what we decode each array element into. Types are strict
// (a number where a string belongs fails that one element only).
type wireEvent struct {
	EventID    string         `json:"event_id"`
	CampaignID string         `json:"campaign_id"`
	ContactID  string         `json:"contact_id"`
	Type       string         `json:"type"`
	Timestamp  string         `json:"timestamp"`
	Metadata   map[string]any `json:"metadata"`
}

var validTypes = map[string]bool{"sent": true, "delivered": true, "opened": true, "clicked": true}
var typeOrder = []string{"sent", "delivered", "opened", "clicked"}
