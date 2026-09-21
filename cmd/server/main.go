package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/resume-builder/backend/internal/agent"
	"github.com/resume-builder/backend/internal/config"
	"github.com/resume-builder/backend/internal/converter"
	"github.com/resume-builder/backend/internal/handler"
	"github.com/resume-builder/backend/internal/server"
	"github.com/resume-builder/backend/internal/store"
	"github.com/resume-builder/backend/pkg/llm"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file found, using system env vars")
	}

	cfg := config.Load()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mongoStore, err := store.NewMongoStore(ctx, cfg.MongoURI, cfg.MongoDB)
	if err != nil {
		log.Fatalf("failed to connect to MongoDB: %v", err)
	}
	defer mongoStore.Close(ctx)

	ncStore := store.NewNextcloudStore(
		cfg.NextcloudBaseURL,
		cfg.NextcloudUser,
		cfg.NextcloudPass,
		cfg.NextcloudShareBase,
	)

	userStore := store.NewUserStore(mongoStore.DB)
	resumeStore := store.NewResumeStore(mongoStore.DB)
	uploadStore := store.NewUploadStore(mongoStore.DB)
	designRefStore := store.NewDesignRefStore(mongoStore.DB)
	photoStore := store.NewPhotoStore(mongoStore.DB)

	providerFactory, err := llm.NewProviderFactory(cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMBaseURL)
	if err != nil {
		log.Printf("WARNING: LLM provider not configured: %v", err)
		log.Printf("Set LLM_API_KEY in your .env file. Resume generation will fail until configured.")
	} else {
		log.Printf("LLM provider configured: model=%s base_url=%s", cfg.LLMModel, cfg.LLMBaseURL)
	}

	anydocClient := converter.NewClient(cfg.AnydocURL, cfg.AnydocToken)
	resumeAgent := agent.NewResumeAgent(providerFactory, ncStore, anydocClient)

	authH := handler.NewAuthHandler(userStore, cfg.JWTSecret)
	resumeH := handler.NewResumeHandler(
		resumeStore,
		uploadStore,
		ncStore,
		designRefStore,
		photoStore,
		resumeAgent,
		anydocClient,
		cfg.FreeResumeLimit,
		cfg.FreeRevisionLimit,
	)
	uploadH := handler.NewUploadHandler(ncStore, uploadStore, anydocClient)
	exportH := handler.NewExportHandler(resumeStore, ncStore, photoStore)
	usageH := handler.NewUsageHandler(resumeStore, cfg.FreeResumeLimit, cfg.FreeRevisionLimit)

	app := server.New(cfg)
	server.RegisterRoutes(app, authH, resumeH, uploadH, exportH, usageH, cfg.JWTSecret)

	// Generation runs in-process, so a restart mid-build orphans the resume in
	// "generating" and the dashboard polls it forever. Reconcile at boot (with a
	// grace window for a generation the old container may still be finishing) and
	// keep sweeping for workers that died on their own.
	go func() {
		sweep := func(cutoff time.Time) {
			if n, sweepErr := resumeStore.FailStaleGenerating(ctx, cutoff); sweepErr != nil {
				log.Printf("stale-generating sweep failed: %v", sweepErr)
			} else if n > 0 {
				log.Printf("marked %d interrupted generation(s) as failed", n)
			}
		}
		sweep(time.Now().Add(-3 * time.Minute))
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sweep(time.Now().Add(-10 * time.Minute))
			}
		}
	}()

	go func() {
		if err := app.Listen(":" + cfg.Port); err != nil {
			log.Fatalf("server error: %v", err)
		}
	}()

	log.Printf("Resume Builder API running on :%s", cfg.Port)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("shutting down...")
	if err := app.Shutdown(); err != nil {
		log.Printf("shutdown error: %v", err)
	}
}
