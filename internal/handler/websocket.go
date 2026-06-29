package handler

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"
)

type SSEMessage struct {
	Type     string `json:"type"`
	ResumeID string `json:"resume_id,omitempty"`
	Status   string `json:"status,omitempty"`
	Message  string `json:"message,omitempty"`
	PDFPath  string `json:"pdf_path,omitempty"`
}

type sseClient struct {
	send     chan []byte
	resumeID string
}

type SSEBroadcaster struct {
	mu      sync.RWMutex
	clients map[string]map[*sseClient]bool
}

var DefaultBroadcaster = &SSEBroadcaster{
	clients: make(map[string]map[*sseClient]bool),
}

func (b *SSEBroadcaster) subscribe(resumeID string, client *sseClient) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.clients[resumeID] == nil {
		b.clients[resumeID] = make(map[*sseClient]bool)
	}
	b.clients[resumeID][client] = true
}

func (b *SSEBroadcaster) unsubscribeAll(client *sseClient) {
	b.mu.Lock()
	defer b.mu.Unlock()
	close(client.send)
	for resumeID, conns := range b.clients {
		delete(conns, client)
		if len(conns) == 0 {
			delete(b.clients, resumeID)
		}
	}
}

func (b *SSEBroadcaster) Broadcast(resumeID string, msg SSEMessage) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	conns := b.clients[resumeID]
	if len(conns) == 0 {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	for client := range conns {
		select {
		case client.send <- data:
		default:
		}
	}
}

func (b *SSEBroadcaster) BroadcastStatus(resumeID, status, message, pdfPath string) {
	b.Broadcast(resumeID, SSEMessage{
		Type:     "status",
		ResumeID: resumeID,
		Status:   status,
		Message:  message,
		PDFPath:  pdfPath,
	})

	// Send "done" on terminal status so the client can stop gracefully
	if status == "completed" || status == "failed" {
		b.Broadcast(resumeID, SSEMessage{
			Type:     "done",
			ResumeID: resumeID,
			Status:   status,
		})
	}
}

func (b *SSEBroadcaster) CloseResume(resumeID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for client := range b.clients[resumeID] {
		close(client.send)
	}
	delete(b.clients, resumeID)
}

func HandleSSE() fiber.Handler {
	return func(c fiber.Ctx) error {
		resumeID := c.Query("resume_id")
		if resumeID == "" {
			return fiber.NewError(fiber.StatusBadRequest, "resume_id query param is required")
		}

		c.Set("Content-Type", "text/event-stream")
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")

		client := &sseClient{
			send:     make(chan []byte, 32),
			resumeID: resumeID,
		}
		DefaultBroadcaster.subscribe(resumeID, client)
		defer DefaultBroadcaster.unsubscribeAll(client)

		log.Printf("sse client connected for resume %s", resumeID)

		// Send connected confirmation
		ack, _ := json.Marshal(map[string]string{"type": "connected", "resume_id": resumeID})
		client.send <- ack

		return c.SendStreamWriter(func(w *bufio.Writer) {
			// Send keepalive every 15 seconds
			keepalive := time.NewTicker(15 * time.Second)
			defer keepalive.Stop()

			for {
				select {
				case data, ok := <-client.send:
					if !ok {
						return
					}
					fmt.Fprintf(w, "data: %s\n\n", string(data))
					w.Flush()

				case <-keepalive.C:
					fmt.Fprintf(w, ": keepalive\n\n")
					w.Flush()
				}
			}
		})
	}
}

func NotifyStatusChanged(resumeID, status, message, pdfPath string) {
	log.Printf("sse broadcast: resume=%s status=%s message=%s", resumeID, status, message)
	DefaultBroadcaster.BroadcastStatus(resumeID, status, message, pdfPath)
}
