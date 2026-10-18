package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/plugin/opentelemetry/tracing"
)

type Video struct {
	ID        uuid.UUID `gorm:"type:uuid;primaryKey"`
	Title     string
	CreatedAt time.Time
}

type VideoLike struct {
	VideoID   uuid.UUID `gorm:"type:uuid;primaryKey"`
	ViewerID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time
}

type videoResponse struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title"`
	LikeCount     int64     `json:"likeCount"`
	LikedByViewer bool      `json:"likedByViewer"`
}

type server struct {
	db     *gorm.DB
	logger *slog.Logger
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdownTelemetry, err := setupTelemetry(ctx)
	if err != nil {
		logger.Error("configure tracing", "error", err)
		os.Exit(1)
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownTelemetry(shutdownCtx); err != nil {
			logger.Error("stop tracing", "error", err)
		}
	}()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		logger.Error("DATABASE_URL is required")
		return
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		logger.Error("connect to database", "error", err)
		return
	}
	if err := db.Use(tracing.NewPlugin(tracing.WithoutMetrics())); err != nil {
		logger.Error("instrument database", "error", err)
		return
	}
	sqlDB, err := db.DB()
	if err != nil {
		logger.Error("get database pool", "error", err)
		return
	}
	defer sqlDB.Close()
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(10)
	if err := sqlDB.PingContext(ctx); err != nil {
		logger.Error("ping database", "error", err)
		return
	}

	requestCount := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "likes_http_requests_total", Help: "HTTP requests by route, method, and status."}, []string{"route", "method", "status"})
	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "likes_http_request_duration_seconds", Help: "HTTP request duration by route and method.", Buckets: prometheus.DefBuckets}, []string{"route", "method"})
	prometheus.MustRegister(requestCount, requestDuration)
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "likes_db_connections_in_use", Help: "Database connections currently in use."}, func() float64 { return float64(sqlDB.Stats().InUse) }))
	prometheus.MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "likes_db_connections_open", Help: "Open database connections."}, func() float64 { return float64(sqlDB.Stats().OpenConnections) }))
	prometheus.MustRegister(prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "likes_db_wait_count_total", Help: "Times a request waited for a database connection."}, func() float64 { return float64(sqlDB.Stats().WaitCount) }))

	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery(), otelgin.Middleware("likes-api"), metricsMiddleware(requestCount, requestDuration))
	router.GET("/healthz", func(c *gin.Context) { c.Status(http.StatusOK) })
	router.GET("/metrics", gin.WrapH(promhttp.Handler()))
	api := &server{db: db, logger: logger}
	router.GET("/api/videos/:videoId", api.getVideo)
	router.PUT("/api/videos/:videoId/like", api.like)
	router.DELETE("/api/videos/:videoId/like", api.unlike)

	httpServer := &http.Server{Addr: ":8080", Handler: router, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		logger.Info("api listening", "address", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve api", "error", err)
			stop()
		}
	}()
	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("stop api", "error", err)
	}
}

func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	exporter, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName("likes-api"), attribute.String("deployment.environment", "local")))
	if err != nil {
		return nil, err
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return provider.Shutdown, nil
}

func metricsMiddleware(count *prometheus.CounterVec, duration *prometheus.HistogramVec) gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		method := c.Request.Method
		count.WithLabelValues(route, method, strconv.Itoa(c.Writer.Status())).Inc()
		duration.WithLabelValues(route, method).Observe(time.Since(started).Seconds())
	}
}

func parseIDs(c *gin.Context) (uuid.UUID, uuid.UUID, bool) {
	videoID, err := uuid.Parse(c.Param("videoId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid video ID"})
		return uuid.Nil, uuid.Nil, false
	}
	viewerID, err := uuid.Parse(c.GetHeader("X-Demo-Viewer-Id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "X-Demo-Viewer-Id must be a UUID"})
		return uuid.Nil, uuid.Nil, false
	}
	return videoID, viewerID, true
}

func (s *server) getVideo(c *gin.Context) {
	videoID, viewerID, ok := parseIDs(c)
	if !ok {
		return
	}
	s.respondWithVideo(c, videoID, viewerID)
}

func (s *server) like(c *gin.Context) {
	videoID, viewerID, ok := parseIDs(c)
	if !ok {
		return
	}
	if !s.videoExists(c, videoID) {
		return
	}
	like := VideoLike{VideoID: videoID, ViewerID: viewerID}
	if err := s.db.WithContext(c.Request.Context()).Clauses(clause.OnConflict{DoNothing: true}).Create(&like).Error; err != nil {
		s.fail(c, err)
		return
	}
	s.respondWithVideo(c, videoID, viewerID)
}

func (s *server) unlike(c *gin.Context) {
	videoID, viewerID, ok := parseIDs(c)
	if !ok {
		return
	}
	if !s.videoExists(c, videoID) {
		return
	}
	if err := s.db.WithContext(c.Request.Context()).Where("video_id = ? AND viewer_id = ?", videoID, viewerID).Delete(&VideoLike{}).Error; err != nil {
		s.fail(c, err)
		return
	}
	s.respondWithVideo(c, videoID, viewerID)
}

func (s *server) videoExists(c *gin.Context, videoID uuid.UUID) bool {
	var video Video
	err := s.db.WithContext(c.Request.Context()).Select("id").First(&video, "id = ?", videoID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return false
	}
	if err != nil {
		s.fail(c, err)
		return false
	}
	return true
}

func (s *server) respondWithVideo(c *gin.Context, videoID, viewerID uuid.UUID) {
	ctx := c.Request.Context()
	var video Video
	if err := s.db.WithContext(ctx).First(&video, "id = ?", videoID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		} else {
			s.fail(c, err)
		}
		return
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&VideoLike{}).Where("video_id = ?", videoID).Count(&count).Error; err != nil {
		s.fail(c, err)
		return
	}
	var liked int64
	if err := s.db.WithContext(ctx).Model(&VideoLike{}).Where("video_id = ? AND viewer_id = ?", videoID, viewerID).Count(&liked).Error; err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, videoResponse{ID: video.ID, Title: video.Title, LikeCount: count, LikedByViewer: liked > 0})
}

func (s *server) fail(c *gin.Context, err error) {
	s.logger.Error("database request failed", "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "request failed"})
}
