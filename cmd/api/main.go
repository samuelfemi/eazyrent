// Command api is the composition root: load config, open the DB once,
// wire concrete dependencies explicitly, serve HTTP.
//
//	@title			EasyRent API
//	@version		1.0
//	@description	Rental listing API.
//	@host			localhost:8080
//	@BasePath		/
//
//	@securityDefinitions.apikey	Bearer
//	@in							header
//	@name						Authorization
//	@description				Type "Bearer" followed by a space and the access token.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/femi/golang-easyrent/internal/auth"
	"github.com/femi/golang-easyrent/internal/config"
	"github.com/femi/golang-easyrent/internal/db"
	"github.com/femi/golang-easyrent/internal/favorite"
	"github.com/femi/golang-easyrent/internal/listing"
	"github.com/femi/golang-easyrent/internal/ratelimit"
	"github.com/femi/golang-easyrent/internal/web"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	authSvc := auth.NewService(
		auth.NewStore(pool),
		auth.Tokens{Secret: []byte(cfg.AccessTokenSecret), AccessTTL: cfg.AccessTokenTTL},
		auth.EmailSender{APIKey: cfg.BrevoAPIKey, From: cfg.EmailFrom, AppURL: cfg.FrontendURL},
		cfg.RefreshTokenTTL,
	)

	handler := web.NewHandler(
		authSvc,
		listing.NewService(listing.NewStore(pool)),
		favorite.NewService(favorite.NewStore(pool)),
		ratelimit.DefaultLimits(),
	)
	if cfg.TrustProxy {
		handler.ClientIP = web.ForwardedIP
	}

	startTokenJanitor(ctx, authSvc)

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// ListenAndServe runs in the background; a bind failure must end the
	// process instead of leaving it waiting on a signal forever.
	serveErr := make(chan error, 1)
	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serveErr:
		return err
	case <-stop:
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	return srv.Shutdown(shutdownCtx)
}

// startTokenJanitor periodically deletes expired refresh and password-reset
// tokens so those tables do not grow without bound. It stops with ctx.
func startTokenJanitor(ctx context.Context, svc auth.Service) {
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refresh, reset, err := svc.Users.DeleteExpiredSessions(ctx)
				if err != nil {
					log.Printf("auth: token cleanup: %v", err)
					continue
				}
				if refresh+reset > 0 {
					log.Printf("auth: cleaned %d refresh and %d password-reset tokens", refresh, reset)
				}
			}
		}
	}()
}
