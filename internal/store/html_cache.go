package store

import "strings"

func PutHTML(key string, data []byte) {
	pdfCache.Put(key, data)
}

func GetHTML(key string) ([]byte, bool) {
	return pdfCache.Get(key)
}

func GetHTMLByResumeID(resumeID string) ([]byte, bool) {
	return pdfCache.GetByResumeID(resumeID)
}

func GetAnyCacheByResumeID(resumeID string) ([]byte, bool) {
	pdfCache.mu.RLock()
	defer pdfCache.mu.RUnlock()
	for key, data := range pdfCache.cache {
		if strings.Contains(key, resumeID) {
			return data, true
		}
	}
	return nil, false
}
