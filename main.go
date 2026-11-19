package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/suanova/medrouter/internal/config"
	"github.com/suanova/medrouter/internal/controller"
	"github.com/suanova/medrouter/internal/kube"
	"github.com/suanova/medrouter/internal/model"
	"github.com/suanova/medrouter/internal/router"
	"github.com/suanova/medrouter/internal/s3"
	"github.com/suanova/medrouter/internal/service"
)

//go:embed all:web/dist
var webDist embed.FS

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := model.InitDB(cfg); err != nil {
		log.Fatalf("database: %v", err)
	}
	defer func() {
		if err := model.Close(); err != nil {
			log.Printf("close database: %v", err)
		}
	}()

	sessions := service.NewSessionService(cfg)
	email := service.NewEmailService(cfg)
	verification := service.NewVerificationService(cfg, email)
	users := service.NewUserService(cfg, sessions, verification)
	oauth := service.NewOAuthService(cfg, users, sessions)
	catalog := service.NewModelCatalog(cfg, newCatalogSource(cfg))
	datasetCatalog := service.NewDatasetCatalog(cfg, newDatasetSource(cfg))
	objectStore := newObjectStore(cfg)
	modelFiles := service.NewFilesService(cfg, catalog, objectStore)
	datasetFiles := service.NewFilesService(cfg, datasetCatalog, objectStore)

	userController := controller.NewUserController(cfg, users, sessions)
	verificationController := controller.NewVerificationController(verification)
	oauthController := controller.NewOAuthController(cfg, oauth, sessions)
	statusController := controller.NewStatusController(cfg, oauth)
	modelController := controller.NewModelController(catalog, modelFiles)
	datasetController := controller.NewDatasetController(datasetCatalog, datasetFiles)

	engine := router.New(router.Deps{
		Config:       cfg,
		Users:        userController,
		Verification: verificationController,
		OAuth:        oauthController,
		Status:       statusController,
		Models:       modelController,
		Datasets:     datasetController,
		Sessions:     sessions,
		Static:       webDist,
	})

	if cfg.SessionSecret == config.DefaultDevSessionSecret {
		log.Printf("WARN using the development SESSION_SECRET; set SESSION_SECRET before deploying")
	}
	if !cfg.EmailVerificationEnabled {
		log.Printf("WARN email verification is disabled")
	}
	if providers := oauth.Providers(); len(providers) == 0 {
		log.Printf("oauth: no providers configured")
	} else {
		log.Printf("oauth: %d provider(s) configured", len(providers))
	}
	if cfg.ModelLibraryEnabled {
		if cfg.ModelLibraryAPIURL != "" {
			log.Printf("model library: using Kubernetes API at %s", cfg.ModelLibraryAPIURL)
		} else {
			log.Printf("model library: using in-cluster service account")
		}
	} else {
		log.Printf("model library: disabled")
	}
	if cfg.DatasetLibraryEnabled {
		if url := datasetAPIURL(cfg); url != "" {
			log.Printf("dataset library: using Kubernetes API at %s", url)
		} else {
			log.Printf("dataset library: using in-cluster service account")
		}
	} else {
		log.Printf("dataset library: disabled")
	}
	if objectStore != nil {
		if cfg.S3Endpoint != "" {
			log.Printf("file browser: object store at %s", cfg.S3Endpoint)
		} else {
			log.Printf("file browser: using the default AWS S3 endpoint (region %s)", cfg.S3Region)
		}
	} else {
		log.Printf("file browser: browsing disabled")
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           engine,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	go func() {
		log.Printf("%s %s listening on http://localhost:%s%s", cfg.SystemName, cfg.Version, cfg.Port, cfg.BasePath)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown: %v\n", err)
	}
}

// newObjectStore builds the S3-backed file browser store used by the dataset
// and model libraries. It returns nil (S3_DISABLED in the API) when the feature
// is off or misconfigured; a bad configuration must never keep MedRouter from
// starting.
func newObjectStore(cfg *config.Config) service.ObjectStore {
	if !cfg.S3Enabled {
		return nil
	}
	if cfg.S3Endpoint == "" {
		// Empty means the standard AWS S3 endpoint for S3_REGION. This is the
		// normal configuration on real AWS; custom stores set S3_ENDPOINT.
		log.Printf("file browser: no S3_ENDPOINT; using the default AWS endpoint (region %s)", cfg.S3Region)
	}
	credentials, err := cfg.LoadS3Credentials()
	if err != nil {
		log.Printf("WARN dataset files: %v", err)
		return nil
	}
	client, err := s3.New(s3.Options{
		Endpoint:        cfg.S3Endpoint,
		Region:          cfg.S3Region,
		PathStyle:       cfg.S3PathStyle,
		AccessKeyID:     credentials.AccessKeyID,
		SecretAccessKey: credentials.SecretAccessKey,
		SessionToken:    credentials.SessionToken,
		HTTPClient:      &http.Client{Timeout: cfg.S3Timeout},
	})
	if err != nil {
		log.Printf("WARN dataset files: %v", err)
		return nil
	}
	return client
}

// newCatalogSource chooses the model-library data source. No configuration
// error is fatal: the page degrades to an unavailable state instead.
func newCatalogSource(cfg *config.Config) kube.Source {
	if !cfg.ModelLibraryEnabled {
		return kube.DisabledSource
	}
	if cfg.ModelLibraryAPIURL != "" {
		client, err := kube.NewAPIClient(cfg.ModelLibraryAPIURL, cfg.ModelLibraryTimeout)
		if err != nil {
			log.Printf("WARN model library: %v", err)
			return kube.NoClusterSource
		}
		client.SetOverviewNamespace(cfg.ModelLibraryOverviewNamespace)
		return client
	}
	client, err := kube.NewInClusterClient(cfg.ModelLibraryTimeout)
	if err != nil {
		log.Printf("WARN model library: %v", err)
		return kube.NoClusterSource
	}
	client.SetOverviewNamespace(cfg.ModelLibraryOverviewNamespace)
	return client
}

// datasetAPIURL returns the explicit dataset-library API URL, falling back to
// the model-library URL so one local `kubectl proxy` serves both catalogs.
func datasetAPIURL(cfg *config.Config) string {
	if cfg.DatasetLibraryAPIURL != "" {
		return cfg.DatasetLibraryAPIURL
	}
	return cfg.ModelLibraryAPIURL
}

// newDatasetSource chooses the dataset-library data source. No configuration
// error is fatal: the page degrades to an unavailable state instead.
func newDatasetSource(cfg *config.Config) kube.Source {
	if !cfg.DatasetLibraryEnabled {
		return kube.UnavailableSource{Reason: service.CatalogDisabled, Retryable: false}
	}
	if url := datasetAPIURL(cfg); url != "" {
		client, err := kube.NewAPIClient(url, cfg.DatasetLibraryTimeout)
		if err != nil {
			log.Printf("WARN dataset library: %v", err)
			return kube.NoClusterSource
		}
		client.SetOverviewNamespace(cfg.ModelLibraryOverviewNamespace)
		return client
	}
	client, err := kube.NewInClusterClient(cfg.DatasetLibraryTimeout)
	if err != nil {
		log.Printf("WARN dataset library: %v", err)
		return kube.NoClusterSource
	}
	client.SetOverviewNamespace(cfg.ModelLibraryOverviewNamespace)
	return client
}
