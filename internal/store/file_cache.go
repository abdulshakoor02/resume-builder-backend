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
