package config

import "os"

type Config struct {
	Port               string
	MongoURI           string
	MongoDB            string
	NextcloudBaseURL   string
	NextcloudUser      string
	NextcloudPass      string
	NextcloudShareBase string
	LLMAPIKey          string
	LLMModel           string
	LLMBaseURL         string
	JWTSecret          string
	AllowedOrigins     string
}

func Load() *Config {
	return &Config{
		Port:              getEnv("PORT", "8080"),
		MongoURI:          getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDB:           getEnv("MONGO_DB", "resume_builder"),
		NextcloudBaseURL:  getEnv("NEXTCLOUD_BASE_URL", ""),
		NextcloudUser:     getEnv("NEXTCLOUD_USERNAME", ""),
		NextcloudPass:     getEnv("NEXTCLOUD_PASSWORD", ""),
		NextcloudShareBase: getEnv("NEXTCLOUD_SHARE_BASE_URL", ""),
		LLMAPIKey:         getEnv("LLM_API_KEY", ""),
		LLMModel:          getEnv("LLM_MODEL", "gpt-4o"),
		LLMBaseURL:        getEnv("LLM_BASE_URL", ""),
		JWTSecret:      getEnv("JWT_SECRET", "dev-secret-change-in-production"),
		AllowedOrigins: getEnv("ALLOWED_ORIGINS", "http://localhost:3000,http://localhost:1101,https://resume.wethinkdigital.solutions,http://resume.wethinkdigital.solutions"),
	}
}

func getEnv(key, fallback string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return fallback
}
