package main

import (
	"net/http"
	"strconv"
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
			"amount":       demoGetParamInt(r, "amount", 500),
			"message":      demoGetParamStr(r, "message", "ShowLove500 Thanks for the Stream! myuserHype"),
			"total_amount": demoGetParamInt(r, "total_amount", 1337),
		}

	case msgTypeDonation:
		data = map[string]interface{}{
			"name":    demoIssuer,
			"amount":  demoGetParamFloat(r, "amount", 6.66),
			"message": demoGetParamStr(r, "message", "You rock!"),
		}

	case msgTypeFollow:
		data = map[string]interface{}{
			"from":        demoIssuer,
			"followed_at": time.Now(),
		}

	case msgTypeHost:
		data = map[string]interface{}{
			"from":        demoIssuer,
			"viewerCount": demoGetParamInt(r, "viewerCount", 5),
		}

	case msgTypeRaid:
		data = map[string]interface{}{
			"from":        demoIssuer,
			"viewerCount": demoGetParamInt(r, "viewerCount", 5),
		}

	case msgTypeSub:
		data = map[string]interface{}{
			"from":     demoIssuer,
			"is_resub": false,
			"message":  "",
			"paid_for": demoGetParamInt(r, "paid_for", 1),
			"streak":   demoGetParamInt(r, "streak", 1),
			"tier":     demoGetParamStr(r, "tier", "1000"),
			"total":    1,
		}

	case "resub": // Execption to the known types to trigger resubs
		event = msgTypeSub
		data = map[string]interface{}{
			"from":     demoIssuer,
			"is_resub": true,
			"message":  demoGetParamStr(r, "message", "Already 12 months! PogChamp"),
			"paid_for": demoGetParamInt(r, "paid_for", 1),
			"streak":   demoGetParamInt(r, "streak", 12),
			"tier":     demoGetParamStr(r, "tier", "1000"),
			"total":    demoGetParamInt(r, "total", 12),
		}

	case msgTypeSubGift:
		data = map[string]interface{}{
			"from":     demoIssuer,
			"is_anon":  demoGetParamStr(r, "is_anon", "false") == "true",
			"gift_to":  demoGetParamStr(r, "gift_to", "Tester"),
			"paid_for": demoGetParamInt(r, "paid_for", 1),
			"streak":   demoGetParamInt(r, "streak", 12),
			"tier":     demoGetParamStr(r, "tier", "1000"),
			"total":    demoGetParamInt(r, "total", 12),
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

func demoGetParamFloat(r *http.Request, key string, fallback float64) float64 {
	v := r.FormValue(key)
	if v == "" {
		return fallback
	}

	vi, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}

	return vi
}

func demoGetParamInt(r *http.Request, key string, fallback int) int {
	v := r.FormValue(key)
	if v == "" {
		return fallback
	}

	vi, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}

	return vi
}

func demoGetParamStr(r *http.Request, key, fallback string) string {
	v := r.FormValue(key)
	if v == "" {
		return fallback
	}

	return v
}
