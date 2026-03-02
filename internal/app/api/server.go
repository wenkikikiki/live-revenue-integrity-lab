// Package apiserver hosts the HTTP ingress handlers.
package apiserver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mini_station/live-revenue-integrity-lab/internal/httpx"
	"github.com/mini_station/live-revenue-integrity-lab/internal/service"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
)

// Server is the HTTP API runtime.
type Server struct {
	httpServer *http.Server
	svc        *service.Service
	redis      *redis.Client
	reqCounter uint64
}

// New creates a new API server.
func New(addr string, db *sql.DB, redisClient *redis.Client) *Server {
	registerMetrics()

	svc := service.New(db)
	mux := http.NewServeMux()
	s := &Server{
		httpServer: &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		svc:   svc,
		redis: redisClient,
	}
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/healthz", s.wrap("healthz", s.handleHealth))
	mux.HandleFunc("/v1/wallets/recharges", s.wrap("wallet_recharge", s.handleRecharge))
	mux.HandleFunc("/v1/gifts", s.wrap("gift_send", s.handleGift))
	mux.HandleFunc("/v1/internal/comments", s.wrap("internal_comments", s.handleInternalComments))
	mux.HandleFunc("/v1/internal/watch-minutes", s.wrap("internal_watch_minutes", s.handleInternalWatchMinutes))
	mux.HandleFunc("/v1/lives/", s.wrap("lives_prefix", s.handleLivesPrefix))
	mux.HandleFunc("/v1/campaigns/", s.wrap("campaigns_prefix", s.handleCampaignPrefix))
	mux.HandleFunc("/v1/settlements/", s.wrap("settlement_get", s.handleSettlementGet))
	return s
}

// Start runs the HTTP server.
func (s *Server) Start() error {
	log.Printf("api listening on %s", s.httpServer.Addr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleRecharge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var req service.RechargeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON body")
		return
	}

	res, appErr, err := s.svc.Recharge(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if appErr != nil {
		httpx.WriteError(w, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}

	httpx.WriteJSON(w, http.StatusCreated, res)
}

func (s *Server) handleGift(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var req service.GiftRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON body")
		return
	}

	res, appErr, err := s.svc.SendGift(r.Context(), req)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if appErr != nil {
		httpx.WriteError(w, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	httpx.WriteJSON(w, http.StatusCreated, res)
}

func (s *Server) handleLivesPrefix(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "lives" {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "route not found")
		return
	}
	liveSessionID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_PATH", "invalid live session id")
		return
	}

	switch {
	case r.Method == http.MethodPost && parts[3] == "close":
		appErr := s.svc.CloseLiveSession(r.Context(), liveSessionID)
		if appErr != nil {
			httpx.WriteError(w, appErr.HTTPStatus, appErr.Code, appErr.Message)
			return
		}
		httpx.WriteJSON(w, http.StatusAccepted, map[string]any{
			"live_session_id": liveSessionID,
			"status":          "CLOSED",
		})
		return
	case r.Method == http.MethodGet && parts[3] == "contributors":
		limit := parseLimit(r.URL.Query().Get("limit"))
		rows, err := s.getLiveContributors(r.Context(), liveSessionID, limit)
		if err != nil {
			httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		httpx.WriteJSON(w, http.StatusOK, map[string]any{
			"live_session_id": liveSessionID,
			"contributors":    rows,
		})
		return
	default:
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "route not found")
	}
}

func (s *Server) handleCampaignPrefix(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 4 || parts[0] != "v1" || parts[1] != "campaigns" || parts[3] != "leaderboard" {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "route not found")
		return
	}
	campaignID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_PATH", "invalid campaign id")
		return
	}
	limit := parseLimit(r.URL.Query().Get("limit"))
	rows, err := s.getCampaignLeaderboard(r.Context(), campaignID, limit)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{
		"campaign_id": campaignID,
		"leaderboard": rows,
	})
}

func (s *Server) handleSettlementGet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only GET is allowed")
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] != "settlements" {
		httpx.WriteError(w, http.StatusNotFound, "NOT_FOUND", "route not found")
		return
	}
	liveSessionID, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_PATH", "invalid live session id")
		return
	}

	res, appErr, err := s.svc.GetSettlement(r.Context(), liveSessionID)
	if err != nil {
		httpx.WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}
	if appErr != nil {
		httpx.WriteError(w, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	httpx.WriteJSON(w, http.StatusOK, res)
}

func (s *Server) handleInternalComments(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var req service.InternalCommentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON body")
		return
	}
	if appErr := s.svc.EnqueueCommentEvent(r.Context(), req); appErr != nil {
		httpx.WriteError(w, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"queued": 1})
}

func (s *Server) handleInternalWatchMinutes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		httpx.WriteError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "only POST is allowed")
		return
	}
	defer func() {
		_ = r.Body.Close()
	}()

	var req service.InternalWatchMinutesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, "BAD_JSON", "invalid JSON body")
		return
	}
	queued, appErr := s.svc.EnqueueWatchMinuteEvents(r.Context(), req)
	if appErr != nil {
		httpx.WriteError(w, appErr.HTTPStatus, appErr.Code, appErr.Message)
		return
	}
	httpx.WriteJSON(w, http.StatusAccepted, map[string]any{"queued": queued})
}

// WithTimeout is used by tests and shutdown hooks.
func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}

func parseLimit(raw string) int {
	if strings.TrimSpace(raw) == "" {
		return 20
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 20
	}
	if n > 100 {
		return 100
	}
	return n
}

func (s *Server) getLiveContributors(ctx context.Context, liveSessionID uint64, limit int) ([]service.ContributorSummary, error) {
	if s.redis != nil {
		key := fmt.Sprintf("lb:contributors:%d", liveSessionID)
		zs, err := s.redis.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
		if err == nil {
			out := make([]service.ContributorSummary, 0, len(zs))
			for _, z := range zs {
				member, ok := z.Member.(string)
				if !ok {
					continue
				}
				viewerID, err := strconv.ParseUint(member, 10, 64)
				if err != nil {
					continue
				}
				out = append(out, service.ContributorSummary{ViewerID: viewerID, Coins: int64(z.Score)})
			}
			return out, nil
		}
	}
	return s.svc.GetLiveContributors(ctx, liveSessionID, limit)
}

func (s *Server) getCampaignLeaderboard(ctx context.Context, campaignID uint64, limit int) ([]service.CampaignRankRow, error) {
	if s.redis != nil {
		key := fmt.Sprintf("lb:campaign:%d", campaignID)
		zs, err := s.redis.ZRevRangeWithScores(ctx, key, 0, int64(limit-1)).Result()
		if err == nil {
			out := make([]service.CampaignRankRow, 0, len(zs))
			for _, z := range zs {
				member, ok := z.Member.(string)
				if !ok {
					continue
				}
				creatorID, err := strconv.ParseUint(member, 10, 64)
				if err != nil {
					continue
				}
				out = append(out, service.CampaignRankRow{CreatorID: creatorID, Points: int64(z.Score)})
			}
			return out, nil
		}
	}
	return s.svc.GetCampaignLeaderboard(ctx, campaignID, limit)
}

func (s *Server) wrap(route string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			requestID = s.newRequestID()
		}
		w.Header().Set("X-Request-ID", requestID)

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()
		next(sw, r)
		durationSec := time.Since(start).Seconds()

		statusCode := strconv.Itoa(sw.status)
		apiRequestsTotal.WithLabelValues(route, r.Method, statusCode).Inc()
		apiRequestDurationSeconds.WithLabelValues(route, r.Method).Observe(durationSec)
		if sw.status >= 400 {
			apiRejectsTotal.WithLabelValues(route, statusCode).Inc()
		}

		log.Printf(
			`{"route":%q,"status":%d,"duration_ms":%.2f}`,
			route,
			sw.status,
			durationSec*1000,
		)
	}
}

func (s *Server) newRequestID() string {
	v := atomic.AddUint64(&s.reqCounter, 1)
	return fmt.Sprintf("req-%d", v)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

var (
	metricsOnce = sync.Once{}

	apiRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "live_revenue_api_requests_total",
			Help: "Total API requests by route/method/status.",
		},
		[]string{"route", "method", "status"},
	)
	apiRequestDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "live_revenue_api_request_duration_seconds",
			Help:    "API request latency in seconds by route and method.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"route", "method"},
	)
	apiRejectsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "live_revenue_api_rejects_total",
			Help: "Total rejected API requests by route/status.",
		},
		[]string{"route", "status"},
	)
)

func registerMetrics() {
	metricsOnce.Do(func() {
		prometheus.MustRegister(apiRequestsTotal, apiRequestDurationSeconds, apiRejectsTotal)
	})
}
