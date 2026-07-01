package store

import (
	"strings"
	"sync"
)

type fileCache struct {
	mu    sync.RWMutex
	cache map[string][]byte
}

var uploadedFiles = &fileCache{
	cache: make(map[string][]byte),
}

func PutUploadedFile(key string, data []byte) {
	uploadedFiles.mu.Lock()
	defer uploadedFiles.mu.Unlock()
	uploadedFiles.cache[key] = data
}

func GetUploadedFile(key string) ([]byte, bool) {
	uploadedFiles.mu.RLock()
	defer uploadedFiles.mu.RUnlock()
	data, ok := uploadedFiles.cache[key]
	return data, ok
}

func ClearUploadedFile(key string) {
	uploadedFiles.mu.Lock()
	defer uploadedFiles.mu.Unlock()
	delete(uploadedFiles.cache, key)
}

func HasUploadedFile(resumeID string) bool {
	uploadedFiles.mu.RLock()
	defer uploadedFiles.mu.RUnlock()
	for key := range uploadedFiles.cache {
		if strings.Contains(key, resumeID) {
			return true
		}
	}
	return false
}

// ---- Photo cache (in-memory) ----

var photoCache = &fileCache{
	cache: make(map[string][]byte),
}

func PutPhoto(key string, data []byte) {
	photoCache.mu.Lock()
	defer photoCache.mu.Unlock()
	photoCache.cache[key] = data
}

func GetPhoto(key string) ([]byte, bool) {
	photoCache.mu.RLock()
	defer photoCache.mu.RUnlock()
	data, ok := photoCache.cache[key]
	return data, ok
}
