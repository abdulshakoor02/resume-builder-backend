package store

import (
	"strings"
	"sync"
)

type PDFCache struct {
	mu    sync.RWMutex
	cache map[string][]byte
}

var pdfCache = &PDFCache{
	cache: make(map[string][]byte),
}

func (c *PDFCache) Put(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache[key] = data
}

func (c *PDFCache) Get(key string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	data, ok := c.cache[key]
	return data, ok
}

func (c *PDFCache) GetByResumeID(resumeID string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for key, data := range c.cache {
		if strings.Contains(key, resumeID) {
			return data, true
		}
	}
	return nil, false
}

func PutPDF(key string, data []byte) {
	pdfCache.Put(key, data)
}

func GetPDF(key string) ([]byte, bool) {
	return pdfCache.Get(key)
}

func GetPDFByResumeID(resumeID string) ([]byte, bool) {
	return pdfCache.GetByResumeID(resumeID)
}
