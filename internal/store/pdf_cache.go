package store

import (
	"regexp"
	"strconv"
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

// revisionKeyPattern matches the revision-keyed cache entries the agent writes
// (`html/<user>/<resume>/v<N>.html`).
var revisionKeyPattern = regexp.MustCompile(`v(\d+)\.html$`)

// GetByResumeID returns the CURRENT document for a resume.
//
// It must never return an arbitrary match. Several entries for one resume
// coexist in this map — every revision (`…/v1.html`, `…/v2.html`, …) plus the
// bare resume-ID key — and Go randomises map iteration, so a first-match scan
// served a random revision: the dashboard preview (which renders this through
// /pdf) flickered between an old and the latest document, and a change made by
// a later refine (e.g. an added profile photo) appeared to have had no effect.
//
// Order of preference: the bare resume-ID key (rewritten by every store, so it
// holds the newest bytes), else the highest revision number, else the
// lexicographically last key — deterministic in every case.
func (c *PDFCache) GetByResumeID(resumeID string) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if data, ok := c.cache[resumeID]; ok {
		return data, true
	}

	bestKey, bestRev := "", -1
	for key := range c.cache {
		if !strings.Contains(key, resumeID) {
			continue
		}
		rev := -1
		if m := revisionKeyPattern.FindStringSubmatch(key); m != nil {
			if n, convErr := strconv.Atoi(m[1]); convErr == nil {
				rev = n
			}
		}
		if rev > bestRev || (rev == bestRev && key > bestKey) {
			bestKey, bestRev = key, rev
		}
	}
	if bestKey == "" {
		return nil, false
	}
	data, ok := c.cache[bestKey]
	return data, ok
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
