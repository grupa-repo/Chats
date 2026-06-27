package main

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	secret  = "71871847e4548334f720bf055f30829e28f58a52bb4aae7319d5d775622682cf6ba54671a2c270110be13ffb3fea16b3563e2109a4d24612ac5c5469d9cbc9e5"
	baseURL = "http://localhost:8070"
)

type envelope struct {
	Type    string          `json:"type"`
	ChatID  string          `json:"chat_id,omitempty"`
	Payload json.RawMessage `json:"payload"`
}

type messagePayload struct {
	ID          string    `json:"id"`
	SenderID    string    `json:"sender_id"`
	Content     string    `json:"content"`
	MessageType string    `json:"message_type"`
	CreatedAt   time.Time `json:"created_at"`
}

type messageDeletedPayload struct {
	ID        string    `json:"id"`
	DeletedAt time.Time `json:"deleted_at"`
}

type readyPayload struct {
	ChatIDs []string `json:"chat_ids"`
}

type errorPayload struct {
	Error string `json:"error"`
}

func newUUID() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:]))
}

func b64(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func generateToken(userID string) string {
	header, _ := json.Marshal(map[string]string{"alg": "HS512", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]interface{}{
		"nameid": userID,
		"exp":    time.Now().Add(24 * time.Hour).Unix(),
	})
	signingInput := b64(header) + "." + b64(payload)
	mac := hmac.New(sha512.New, []byte(secret))
	mac.Write([]byte(signingInput))
	return signingInput + "." + b64(mac.Sum(nil))
}

func createChat(token string) string {
	body, _ := json.Marshal(map[string]string{"type": "private"})
	req, _ := http.NewRequest(http.MethodPost, baseURL+"/api/chats", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatalf("create chat: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		log.Fatalf("create chat returned %d: %s", resp.StatusCode, b)
	}

	var chat struct {
		ID string `json:"id"`
	}
	json.NewDecoder(resp.Body).Decode(&chat)
	return chat.ID
}

func prompt(scanner *bufio.Scanner, label string) string {
	fmt.Print(label)
	scanner.Scan()
	return strings.TrimSpace(scanner.Text())
}

func main() {
	scanner := bufio.NewScanner(os.Stdin)

	username := prompt(scanner, "Enter your username: ")
	if username == "" {
		fmt.Println("Username cannot be empty")
		return
	}

	tokenInput := prompt(scanner, "JWT token (leave blank to auto-generate): ")
	var token string
	if tokenInput == "" {
		userID := newUUID()
		token = generateToken(userID)
		fmt.Printf("Generated token for user ID: %s\n", userID)
	} else {
		token = tokenInput
	}

	chatID := prompt(scanner, "Chat ID (leave blank to create a new chat): ")
	if chatID == "" {
		chatID = createChat(token)
		fmt.Printf("Created chat: %s\n", chatID)
	}

	u := url.URL{
		Scheme:   "ws",
		Host:     "localhost:8070",
		Path:     "/api/ws",
		RawQuery: "token=" + token,
	}

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("Failed to connect:", err)
	}
	defer conn.Close()

	fmt.Printf("Connected as %s, will send to chat %s\n", username, chatID)
	fmt.Println("Type a message and press Enter to send. Type 'quit' or Ctrl+C to exit.")

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for {
			var env envelope
			if err := conn.ReadJSON(&env); err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					log.Println("Read error:", err)
				}
				return
			}
			if env.ChatID != "" && env.ChatID != chatID {
				continue
			}
			switch env.Type {
			case "ready":
				var p readyPayload
				_ = json.Unmarshal(env.Payload, &p)
				fmt.Printf("\rReady — subscribed to %d chat(s)\n> ", len(p.ChatIDs))
			case "message.created":
				var p messagePayload
				_ = json.Unmarshal(env.Payload, &p)
				fmt.Printf("\r[%s] %s: %s\n> ", p.CreatedAt.Format("15:04:05"), p.SenderID, p.Content)
			case "message.deleted":
				var p messageDeletedPayload
				_ = json.Unmarshal(env.Payload, &p)
				fmt.Printf("\r[%s] <message %s deleted>\n> ", p.DeletedAt.Format("15:04:05"), p.ID)
			case "error":
				var p errorPayload
				_ = json.Unmarshal(env.Payload, &p)
				fmt.Printf("\rserver error: %s\n> ", p.Error)
			}
		}
	}()

	go func() {
		inputScanner := bufio.NewScanner(os.Stdin)
		fmt.Print("> ")
		for inputScanner.Scan() {
			text := strings.TrimSpace(inputScanner.Text())
			if text == "quit" {
				conn.WriteMessage(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if text == "" {
				fmt.Print("> ")
				continue
			}
			frame := map[string]string{
				"action":  "send_message",
				"chat_id": chatID,
				"content": text,
			}
			if err := conn.WriteJSON(frame); err != nil {
				log.Println("Write error:", err)
				return
			}
			fmt.Print("> ")
		}
	}()

	select {
	case <-done:
	case <-interrupt:
		fmt.Println("\nDisconnecting...")
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
}
