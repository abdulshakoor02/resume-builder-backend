package store

// Thin accessors over the shared HTML/PDF cache. Every entry for a resume lives
// in one map keyed by string, so lookups by resume ID must go through
// pdfCache.GetByResumeID, which picks the *current* document deterministically.

func PutHTML(key string, data []byte) {
	pdfCache.Put(key, data)
}

func GetHTML(key string) ([]byte, bool) {
	return pdfCache.Get(key)
}

func GetHTMLByResumeID(resumeID string) ([]byte, bool) {
	return pdfCache.GetByResumeID(resumeID)
}

// GetAnyCacheByResumeID returns the resume's current document. It delegates to
// the cache's deterministic lookup rather than scanning for a first match: with
// one entry per revision present, a map scan returns a random revision (see
// PDFCache.GetByResumeID).
func GetAnyCacheByResumeID(resumeID string) ([]byte, bool) {
	return pdfCache.GetByResumeID(resumeID)
}
