package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/hpcloud/tail"
)

var (
	clients   = make(map[*websocket.Conn]bool)
	clientsMu sync.Mutex
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
)

func main() {
	logFilePath := flag.String("file", "", "Path to log file to watch (required)")
	port := flag.Int("port", 1212, "Port to run WebSocket server on")
	flag.Parse()

	if *logFilePath == "" {
		log.Fatalf("Missing required --file flag")
	}

	go startLogTailer(*logFilePath)

	http.HandleFunc("/logs", handleWebSocket)

	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Logs WebSocket server running at ws://localhost%s/logs", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}
	log.Printf("Client connected: %s", conn.RemoteAddr())

	registerClient(conn)

	go func() {
		defer unregisterClient(conn)
		for {
			_, _, err := conn.ReadMessage()
			if err != nil {
				log.Printf("Client disconnected: %s", conn.RemoteAddr())
				break
			}
		}
	}()
}

func registerClient(conn *websocket.Conn) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	clients[conn] = true
}

func unregisterClient(conn *websocket.Conn) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	delete(clients, conn)
	conn.Close()
}

func broadcast(line string) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for conn := range clients {
		err := conn.WriteMessage(websocket.TextMessage, []byte(line))
		if err != nil {
			log.Printf("Error sending log to client: %v", err)
			conn.Close()
			delete(clients, conn)
		}
	}
}

func startLogTailer(path string) {
	t, err := tail.TailFile(path, tail.Config{
		Follow:    true,
		ReOpen:    true,
		MustExist: true,
		Poll:      true,
	})
	if err != nil {
		log.Fatalf("Failed to tail file %s: %v", path, err)
	}

	for line := range t.Lines {
		if line.Err != nil {
			log.Printf("Error reading line: %v", line.Err)
			continue
		}
		broadcast(line.Text)
	}
}
