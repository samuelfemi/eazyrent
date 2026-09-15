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

	ctx := context.Background()

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

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
