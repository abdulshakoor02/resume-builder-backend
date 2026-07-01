package store

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/studio-b12/gowebdav"
)

type NextcloudStore struct {
	client    *gowebdav.Client
	basePath  string
	shareBase string
}

func NewNextcloudStore(baseURL, username, password, shareBaseURL string) *NextcloudStore {
	// Ensure the base URL includes the WebDAV path prefix.
	if baseURL != "" && !strings.Contains(baseURL, "/remote.php/dav/files/") {
		log := defaultLogger()
		log.Printf("WARNING: NEXTCLOUD_BASE_URL is missing /remote.php/dav/files/ prefix. Appending /remote.php/dav/files/%s. Fix your .env to avoid this warning.", username)
		baseURL = strings.TrimRight(baseURL, "/") + "/remote.php/dav/files/" + username
	}

	// Split into host root + WebDAV path prefix.
	// gowebdav.NewClient takes a plain host root; the WebDAV path prefix
	// goes into basePath so fullPath returns URLs like:
	//   https://host/remote.php/dav/files/admin/photos/file.jpg
	parts := strings.SplitN(baseURL, "/remote.php/dav/files/", 2)
	root := strings.TrimRight(parts[0], "/")
	basePath := ""
	if len(parts) == 2 {
		basePath = "remote.php/dav/files/" + parts[1]
	}

	client := gowebdav.NewClient(root, username, password)
	return &NextcloudStore{
		client:    client,
		basePath:  basePath,
		shareBase: strings.TrimRight(shareBaseURL, "/"),
	}
}

var nclog = log.New(os.Stdout, "[nextcloud] ", log.LstdFlags)

func defaultLogger() *log.Logger { return nclog }

func (n *NextcloudStore) fullPath(remotePath string) string {
	remotePath = strings.TrimLeft(remotePath, "/")
	if n.basePath == "" {
		return remotePath
	}
	return n.basePath + "/" + remotePath
}

func (n *NextcloudStore) UploadFile(remotePath string, data []byte) error {
	path := n.fullPath(remotePath)

	dir := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		dir = path[:idx]
	}
	if err := n.client.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	return n.client.Write(path, data, 0644)
}

func (n *NextcloudStore) DownloadFile(remotePath string) ([]byte, error) {
	return n.client.Read(n.fullPath(remotePath))
}

func (n *NextcloudStore) DeleteFile(remotePath string) error {
	return n.client.Remove(n.fullPath(remotePath))
}

func (n *NextcloudStore) FileExists(remotePath string) (bool, error) {
	_, err := n.client.Stat(n.fullPath(remotePath))
	if err != nil {
		if gowebdav.IsErrCode(err, 404) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (n *NextcloudStore) BasePath() string {
	return n.basePath
}
