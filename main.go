package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
)

const (
	maxBodyBytes = 5 << 20
	maxBatch     = 1000
)

type server struct{ store *Store }

func newMux(s *server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /events", s.handlePostEvents)
	mux.HandleFunc("GET /campaigns/{campaignID}/stats", s.handleGetStats)
	mux.HandleFunc("GET /campaigns/{campaignID}/events", s.handleListEvents)
	return mux
}

func main() {
	addr := ":8080"
	fmt.Printf("listening on %s\n", addr)
	log.Fatal(http.ListenAndServe(addr, newMux(&server{store: NewStore()})))
}

type rejection struct {
	Index   int    `json:"index"`
	EventID string `json:"event_id,omitempty"`
	Reason  string `json:"reason"`
}

type ingestResponse struct {
	Received   int         `json:"received"`
	Accepted   int         `json:"accepted"`
	Duplicates int         `json:"duplicates"`
	Conflicts  int         `json:"conflicts"`
	Rejected   []rejection `json:"rejected"`
}

// handlePostEvents: 200 whenever the body was a JSON array, with a
// per-event report. Providers retry on non-2xx, so a permanently bad event
// must not cause a 4xx for the whole batch (it would be retried forever and
// would re-send the good events too). 4xx only if the request itself is unusable.
func (s *server) handlePostEvents(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var raws []json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raws); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "body too large"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be a JSON array of events"})
		return
	}
	if len(raws) > maxBatch {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": fmt.Sprintf("max %d events per request", maxBatch)})
		return
	}
	resp := ingestResponse{Received: len(raws), Rejected: []rejection{}}
	for i, raw := range raws {
		ev, err := parseEvent(raw)
		if err != nil {
			rj := rejection{Index: i, Reason: err.Error()}
			var probe struct {
				EventID string `json:"event_id"`
			}
			if json.Unmarshal(raw, &probe) == nil {
				rj.EventID = probe.EventID
			}
			resp.Rejected = append(resp.Rejected, rj)
			continue
		}
		switch s.store.Add(ev) {
		case Accepted:
			resp.Accepted++
		case Duplicate:
			resp.Duplicates++
		case Conflict:
			resp.Conflicts++
			resp.Rejected = append(resp.Rejected, rejection{Index: i, EventID: ev.EventID,
				Reason: "event_id already stored with different content; first write kept"})
		}
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) handleGetStats(w http.ResponseWriter, r *http.Request) {
	st, ok := s.store.GetStats(r.PathValue("campaignID"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown campaign"})
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *server) handleListEvents(w http.ResponseWriter, r *http.Request) {
	limit, offset := 50, 0
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 500 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be 1..500"})
			return
		}
		limit = n
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "offset must be >= 0"})
			return
		}
		offset = n
	}
	evs, total, ok := s.store.ListEvents(r.PathValue("campaignID"), offset, limit)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unknown campaign"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"total": total, "offset": offset, "limit": limit, "events": evs})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON: %v", err)
	}
}
