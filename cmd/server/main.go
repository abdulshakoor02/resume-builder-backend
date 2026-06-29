package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"
	"github.com/resume-builder/backend/internal/agent"
	"github.com/resume-builder/backend/internal/config"
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

	providerFactory, err := llm.NewProviderFactory(cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMBaseURL)
	if err != nil {
		log.Printf("WARNING: LLM provider not configured: %v", err)
		log.Printf("Set LLM_API_KEY in your .env file. Resume generation will fail until configured.")
	} else {
		log.Printf("LLM provider configured: model=%s base_url=%s", cfg.LLMModel, cfg.LLMBaseURL)
	}

	resumeAgent := agent.NewResumeAgent(providerFactory, ncStore)

	authH := handler.NewAuthHandler(userStore, cfg.JWTSecret)
	resumeH := handler.NewResumeHandler(resumeStore, uploadStore, ncStore, resumeAgent)
	uploadH := handler.NewUploadHandler(ncStore, uploadStore)
	exportH := handler.NewExportHandler(resumeStore, ncStore)

	app := server.New(cfg)
	server.RegisterRoutes(app, authH, resumeH, uploadH, exportH, cfg.JWTSecret)

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
