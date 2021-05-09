package main

import (
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

const demoIssuer = "Twitch-Manager"

func handleDemoAlert(w http.ResponseWriter, r *http.Request) {
	var (
		vars  = mux.Vars(r)
		event = vars["event"]
		data  interface{}
	)

	switch event {
	case msgTypeBits:
		data = map[string]interface{}{
			"from":         demoIssuer,
			"amount":       5,
			"total_amount": 1337,
		}

	case msgTypeDonation:
		data = map[string]interface{}{
			"name":    demoIssuer,
			"amount":  6.66,
			"message": "You rock!",
		}

	case msgTypeFollow:
		data = map[string]interface{}{
			"from":        demoIssuer,
			"followed_at": time.Now(),
		}

	case msgTypeHost:
		data = map[string]interface{}{
			"from":        demoIssuer,
			"viewerCount": 5,
		}

	case msgTypeRaid:
		data = map[string]interface{}{
			"from":        demoIssuer,
			"viewerCount": 5,
		}

	case msgTypeSub:
		data = map[string]interface{}{
			"from":     demoIssuer,
			"is_resub": false,
			"paid_for": "1",
			"streak":   "1",
			"tier":     "1000",
			"total":    "1",
		}

	case "resub": // Execption to the known types to trigger resubs
		event = msgTypeSub
		data = map[string]interface{}{
			"from":     demoIssuer,
			"is_resub": true,
			"paid_for": "1",
			"streak":   "12",
			"tier":     "1000",
			"total":    "12",
		}

	case msgTypeSubGift:
		data = map[string]interface{}{
			"from":     demoIssuer,
			"is_anon":  false,
			"gift_to":  "Tester",
			"paid_for": 1,
			"streak":   1,
			"tier":     "1000",
			"total":    1,
		}

	default:
		http.Error(w, "Event not found", http.StatusNotFound)
		return
	}

	if err := subscriptions.SendAllSockets(event, data, false, false); err != nil {
		http.Error(w, errors.Wrap(err, "send to sockets").Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}
