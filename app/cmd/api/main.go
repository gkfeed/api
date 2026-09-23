package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"gkfeed/api/internal/auth"
	"gkfeed/api/internal/config"
	"gkfeed/api/internal/db"
	"gkfeed/api/internal/handlers"
	"gkfeed/api/internal/library"
	"gkfeed/api/internal/services"
	storage "gkfeed/api/internal/storage/postgres"

	_ "gkfeed/api/cmd/api/docs"

	"github.com/gorilla/mux"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/rs/cors"
	httpSwagger "github.com/swaggo/http-swagger/v2"
)

//go:embed test_passkey.html
var testPasskeyHTML string

// @title           GKFeed API
// @version         2.0
// @description     RSS feed aggregator with passkey authentication.

// @contact.name   GKFeed
// @contact.url    https://github.com/gkfeed/api-go

// @securityDefinitions.basic  BasicAuth

// @securityDefinitions.apikey BearerAuth
// @in                         header
// @name                       Authorization

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configuration, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	database, err := sql.Open("pgx", configuration.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	database.SetMaxOpenConns(10)
	defer database.Close()
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	if err := database.PingContext(startupCtx); err != nil {
		return fmt.Errorf("connect database: %w", err)
	}

	// TODO: replace legacy auth package configuration with constructor-injected repositories.
	db.Configure(database)
	defer db.Configure(nil)

	if err := storage.CheckSchema(startupCtx, database); err != nil {
		return err
	}
	libraryHandler := buildLibraryHandler(database)

	server := &http.Server{
		Addr:              configuration.Address,
		Handler:           newHandler(configuration, libraryHandler),
		ReadHeaderTimeout: configuration.ReadHeaderTimeout,
	}
	log.Printf("listening on %s", configuration.Address)

	serverErrors := make(chan error, 1)
	go func() { serverErrors <- server.ListenAndServe() }()
	shutdownSignal, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("serve HTTP: %w", err)
	case <-shutdownSignal.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown HTTP server: %w", err)
		}
		if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP during shutdown: %w", err)
		}
		return nil
	}
}

func buildLibraryHandler(database *sql.DB) *handlers.LibraryHandler {
	service := library.NewService(storage.NewFeedRepository(database), storage.NewItemRepository(database))
	return handlers.NewLibraryHandler(service, services.FeedResolver{})
}

func newHandler(configuration config.Config, provided ...*handlers.LibraryHandler) http.Handler {
	var libraryHandler *handlers.LibraryHandler
	if len(provided) > 0 {
		libraryHandler = provided[0]
	} else if database, err := db.ConfiguredDB(); err == nil {
		libraryHandler = buildLibraryHandler(database)
	} else {
		libraryHandler = handlers.NewLibraryHandler(unavailableLibraryService{}, services.FeedResolver{})
	}
	router := mux.NewRouter()
	authenticate := auth.Authenticate(configuration)

	router.PathPrefix("/api/swagger/").Handler(httpSwagger.Handler(
		httpSwagger.URL("doc.json"),
	))

	router.HandleFunc("/test_passkey", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(testPasskeyHTML))
	}).Methods(http.MethodGet)

	api := router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/list", authenticate(libraryHandler.HandleListOfFeeds)).Methods(http.MethodGet)
	api.HandleFunc("/feed_types", handlers.HandleListFeedTypes).Methods(http.MethodGet)
	api.HandleFunc("/feed", authenticate(libraryHandler.HandleRSSFeed)).Methods(http.MethodGet)
	api.HandleFunc("/add", authenticate(libraryHandler.HandleAddFeed)).Methods(http.MethodPost)
	api.HandleFunc("/delete", authenticate(libraryHandler.HandleDeleteFeed)).Methods(http.MethodDelete)
	api.HandleFunc("/add_lazy", authenticate(libraryHandler.HandleAddFeedLazy)).Methods(http.MethodPost)
	api.HandleFunc("/add_deleted_items", authenticate(libraryHandler.HandleAddDeletedItems)).Methods(http.MethodPost)
	api.HandleFunc("/item", authenticate(libraryHandler.HandleGetItemByID)).Methods(http.MethodGet)
	api.HandleFunc("/get_items", authenticate(libraryHandler.HandleGetItems)).Methods(http.MethodGet)
	api.HandleFunc("/items/{id}", authenticate(libraryHandler.HandleDeleteItem)).Methods(http.MethodDelete)

	v2 := router.PathPrefix("/api/v2").Subrouter()
	v2.HandleFunc("/feeds", authenticate(libraryHandler.HandleListOfFeeds)).Methods(http.MethodGet)
	v2.HandleFunc("/feeds", authenticate(libraryHandler.HandleCreateFeed)).Methods(http.MethodPost)
	v2.HandleFunc("/feeds/{id}", authenticate(libraryHandler.HandleGetFeed)).Methods(http.MethodGet)
	v2.HandleFunc("/feeds/{id}", authenticate(libraryHandler.HandleDeleteFeedByID)).Methods(http.MethodDelete)
	v2.HandleFunc("/items", authenticate(libraryHandler.HandleGetItems)).Methods(http.MethodGet)
	v2.HandleFunc("/items/{id}", authenticate(libraryHandler.HandleGetItemByPathID)).Methods(http.MethodGet)
	v2.HandleFunc("/items/{id}", authenticate(libraryHandler.HandleDeleteItem)).Methods(http.MethodDelete)

	authRouter := api.PathPrefix("/auth").Subrouter()
	authRouter.HandleFunc("/me", authenticate(handlers.HandleMe)).Methods(http.MethodGet)

	if webAuthnService, err := auth.NewWebAuthnService(configuration); err == nil {
		authHandler := handlers.NewAuthHandler(configuration, webAuthnService)

		authRouter.HandleFunc("/register/begin", authenticate(authHandler.BeginRegistration)).Methods(http.MethodPost)
		authRouter.HandleFunc("/register/finish", authenticate(authHandler.FinishRegistration)).Methods(http.MethodPost)
		authRouter.HandleFunc("/login/begin", authHandler.BeginLogin).Methods(http.MethodPost)
		authRouter.HandleFunc("/login/finish", authHandler.FinishLogin).Methods(http.MethodPost)
		authRouter.HandleFunc("/refresh", authHandler.Refresh).Methods(http.MethodPost)
		authRouter.HandleFunc("/logout", authenticate(authHandler.Logout)).Methods(http.MethodPost)
		authRouter.HandleFunc("/credentials", authenticate(authHandler.ListCredentials)).Methods(http.MethodGet)
		authRouter.HandleFunc("/credentials/{id}", authenticate(authHandler.DeleteCredential)).Methods(http.MethodDelete)
	}

	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   configuration.AllowedOrigins,
		AllowedHeaders:   []string{"*"},
		AllowCredentials: true,
	}).Handler(router)

	return corsHandler
}

type unavailableLibraryService struct{}

func (unavailableLibraryService) ListFeeds(context.Context, int) ([]library.Feed, error) {
	return nil, errors.New("library storage is unavailable")
}
func (unavailableLibraryService) GetFeed(context.Context, int, int) (library.Feed, error) {
	return library.Feed{}, errors.New("library storage is unavailable")
}
func (unavailableLibraryService) AddFeed(context.Context, int, library.CreateFeedInput) (library.AddFeedResult, error) {
	return library.AddFeedResult{}, errors.New("library storage is unavailable")
}
func (unavailableLibraryService) DeleteFeed(context.Context, int, int) error {
	return errors.New("library storage is unavailable")
}
func (unavailableLibraryService) GetItem(context.Context, int, int) (library.ItemDetails, error) {
	return library.ItemDetails{}, errors.New("library storage is unavailable")
}
func (unavailableLibraryService) ListItems(context.Context, int) ([]library.Item, error) {
	return nil, errors.New("library storage is unavailable")
}
func (unavailableLibraryService) ListItemsPage(context.Context, int, *int, int) (library.Page, error) {
	return library.Page{}, errors.New("library storage is unavailable")
}
func (unavailableLibraryService) DeleteItem(context.Context, int, int) error {
	return errors.New("library storage is unavailable")
}
