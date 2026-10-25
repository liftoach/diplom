package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"diplom/internal/storage"
	"diplom/internal/telemetry"
)

type API struct{ DB *storage.Store }

func (a *API) Mux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/devices", a.register)
	mux.HandleFunc("GET /api/v1/devices", a.list)
	mux.HandleFunc("GET /api/v1/devices/{id}", a.device)
	mux.HandleFunc("GET /api/v1/devices/{id}/latest", a.latest)
	mux.HandleFunc("GET /api/v1/devices/{id}/measurements", a.history)
	return mux
}

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func failure(w http.ResponseWriter, r *http.Request, status int, code string) {
	var b [12]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	w.Header().Set("X-Request-ID", id)
	slog.Warn("api request failed", "request_id", id, "method", r.Method, "path", r.URL.Path, "code", code)
	JSON(w, status, map[string]string{"code": code, "request_id": id})
}

func dbError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, storage.ErrNotFound) {
		failure(w, r, 404, "not_found")
		return
	}
	slog.Error("database request failed", "error", err)
	failure(w, r, 503, "database_unavailable")
}

func requestContext(r *http.Request) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), 5*time.Second)
}

func (a *API) register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Interval int    `json:"expected_interval_seconds"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(&input); err != nil {
		failure(w, r, 400, "invalid_json")
		return
	}
	if err := d.Decode(new(any)); err != io.EOF {
		failure(w, r, 400, "trailing_json")
		return
	}
	if !telemetry.ValidID(input.ID) || strings.TrimSpace(input.Name) == "" || len(input.Name) > 200 || input.Interval < 1 || input.Interval > 86400 {
		failure(w, r, 400, "invalid_device")
		return
	}
	ctx, cancel := requestContext(r)
	defer cancel()
	device, err := a.DB.Register(ctx, storage.Device{ID: input.ID, Name: input.Name, ExpectedIntervalSeconds: input.Interval})
	if errors.Is(err, storage.ErrConflict) {
		failure(w, r, 409, "device_already_exists")
		return
	}
	if err != nil {
		dbError(w, r, err)
		return
	}
	w.Header().Set("Location", "/api/v1/devices/"+device.ID)
	JSON(w, 201, device)
}

func limitParam(r *http.Request) (int, error) {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return 100, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 || n > 1000 {
		return 0, errors.New("invalid_limit")
	}
	return n, nil
}

func validPath(w http.ResponseWriter, r *http.Request) bool {
	if !telemetry.ValidID(r.PathValue("id")) {
		failure(w, r, 400, "invalid_device_id")
		return false
	}
	return true
}

func (a *API) list(w http.ResponseWriter, r *http.Request) {
	limit, err := limitParam(r)
	if err != nil {
		failure(w, r, 400, "invalid_limit")
		return
	}
	after := r.URL.Query().Get("after")
	if after != "" && !telemetry.ValidID(after) {
		failure(w, r, 400, "invalid_cursor")
		return
	}
	ctx, cancel := requestContext(r)
	defer cancel()
	items, err := a.DB.Devices(ctx, after, limit+1)
	if err != nil {
		dbError(w, r, err)
		return
	}
	next := ""
	if len(items) > limit {
		items = items[:limit]
		next = items[len(items)-1].ID
	}
	JSON(w, 200, map[string]any{"items": items, "next_after": next})
}

func (a *API) device(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	ctx, cancel := requestContext(r)
	defer cancel()
	d, err := a.DB.Device(ctx, r.PathValue("id"))
	if err != nil {
		dbError(w, r, err)
		return
	}
	JSON(w, 200, d)
}

func (a *API) latest(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	ctx, cancel := requestContext(r)
	defer cancel()
	e, err := a.DB.Latest(ctx, r.PathValue("id"))
	if err != nil {
		dbError(w, r, err)
		return
	}
	JSON(w, 200, e)
}

func (a *API) history(w http.ResponseWriter, r *http.Request) {
	if !validPath(w, r) {
		return
	}
	limit, err := limitParam(r)
	if err != nil {
		failure(w, r, 400, "invalid_limit")
		return
	}
	q := r.URL.Query()
	to := time.Now().UTC()
	from := to.Add(-time.Hour)
	if q.Get("to") != "" {
		to, err = time.Parse(time.RFC3339Nano, q.Get("to"))
		if err != nil {
			failure(w, r, 400, "invalid_to")
			return
		}
	}
	if q.Get("from") != "" {
		from, err = time.Parse(time.RFC3339Nano, q.Get("from"))
		if err != nil {
			failure(w, r, 400, "invalid_from")
			return
		}
	} else {
		from = to.Add(-time.Hour)
	}
	if !from.Before(to) || to.Sub(from) > 7*24*time.Hour {
		failure(w, r, 400, "invalid_range_max_7_days")
		return
	}
	before := to
	beforeID := ""
	if q.Get("before") != "" {
		before, err = time.Parse(time.RFC3339Nano, q.Get("before"))
		if err != nil {
			failure(w, r, 400, "invalid_cursor")
			return
		}
		beforeID = q.Get("before_message_id")
		if !telemetry.ValidID(beforeID) {
			failure(w, r, 400, "invalid_cursor")
			return
		}
	} else if q.Get("before_message_id") != "" {
		failure(w, r, 400, "invalid_cursor")
		return
	}
	ctx, cancel := requestContext(r)
	defer cancel()
	exists, err := a.DB.Exists(ctx, r.PathValue("id"))
	if err != nil {
		dbError(w, r, err)
		return
	}
	if !exists {
		failure(w, r, 404, "not_found")
		return
	}
	items, err := a.DB.History(ctx, r.PathValue("id"), from, to, before, beforeID, limit+1)
	if err != nil {
		dbError(w, r, err)
		return
	}
	var next any
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1].Measurement
		next = map[string]string{"before": last.Timestamp.Format(time.RFC3339Nano), "before_message_id": last.MessageID}
	}
	JSON(w, 200, map[string]any{"items": items, "from": from, "to": to, "next": next})
}
