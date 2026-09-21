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

// Del drops a single cache entry.
func (c *PDFCache) Del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.cache, key)
}

// DeleteByResumeID drops every entry whose key mentions the resume ID — the same
// substring rule GetByResumeID/GetAnyCacheByResumeID match on, so a deleted
// resume can't be served from memory after the DB row is gone.
func (c *PDFCache) DeleteByResumeID(resumeID string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for key := range c.cache {
		if strings.Contains(key, resumeID) {
			delete(c.cache, key)
			n++
		}
	}
	return n
}

// PurgeResumeCache removes both the resume-keyed entry and any revision-keyed
// HTML/PDF entries for a resume, returning how many entries were dropped.
func PurgeResumeCache(resumeID string) int {
	pdfCache.Del(resumeID)
	return pdfCache.DeleteByResumeID(resumeID)
}
