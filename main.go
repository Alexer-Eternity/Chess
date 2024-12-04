package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/notnil/chess"
	"math/rand"
	"net/http"
	"time"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

type Message struct {
	Username string `json:"username"`
	Message  string `json:"move"`
	Outcome  string `json:"outcome"`
}
type Client struct {
	Conn     *websocket.Conn
	Username string
	Game     *chess.Game
}

var clients = make(map[*websocket.Conn]*Client) // Update the map to use the new struct
var broadcast = make(chan Message)

func main() {
	http.HandleFunc("/", homePage)
	http.HandleFunc("/invoice", invoicePage)

	http.HandleFunc("/ws", handleConnections)
	http.HandleFunc("/generate-invoice", generateInvoiceHandler)
	go handleMessages()

	fmt.Println("Server started on :8080")
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		panic("Error starting server: " + err.Error())
	}
}

func homePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "main.html") // Serve the main.html file
}

func generateUsername() string {
	adjectives := []string{"Quick", "Bright", "Bold", "Lucky", "Clever"}
	nouns := []string{"Tiger", "Hawk", "Panda", "Fox", "Eagle"}
	rand.Seed(time.Now().UnixNano())
	return adjectives[rand.Intn(len(adjectives))] + nouns[rand.Intn(len(nouns))]
}

func handleConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Println("Error upgrading connection:", err)
		return
	}
	defer conn.Close()

	// Create a new chess game and a new client
	game := chess.NewGame()
	username := generateUsername()
	client := &Client{Conn: conn, Username: username, Game: game}

	// Register new client
	clients[conn] = client

	// Generate and send a welcome message with the username and the chess position
	welcomeMsg := Message{
		Username: username,
		Message:  game.Position().Board().Draw(),
	}

	err = conn.WriteJSON(welcomeMsg)
	if err != nil {
		fmt.Println("Error sending welcome message:", err)
		delete(clients, conn)
		return
	}

	// Listen for incoming messages from the client
	for {
		var msg Message
		err := conn.ReadJSON(&msg)
		if err != nil {
			fmt.Println("Error reading JSON:", err)
			delete(clients, conn)
			return
		}

		// Use the generated username if the client hasn't provided one
		if msg.Username == "" {
			msg.Username = username
		}

		// Broadcast the message to all clients
		broadcast <- msg
	}
}

func handleMessages() {
	for {
		msg := <-broadcast

		clientFound := false
		for clientConn, client := range clients {
			if client.Username == msg.Username { // Check if the username matches
				clientFound = true
				game := client.Game

				move := msg.Message

				// Attempt to make the move on the client's game
				if err := game.MoveStr(move); err != nil {
					fmt.Printf("Invalid move attempted by %s: %s\n", msg.Username, move)
					continue // Skip if the move is invalid
				}

				// Check for the outcome of the game
				outcome := game.Outcome()
				if outcome != chess.NoOutcome { // Game has concluded
					// Send the outcome only to this client
					err := client.Conn.WriteJSON(Message{
						Username: msg.Username,
						Message:  game.Position().Board().Draw(),
						Outcome:  string(outcome),
					})
					if err != nil {
						fmt.Println(err)
						client.Conn.Close()
						delete(clients, clientConn)
					}
				} else {
					// If the game is still ongoing, send the updated board position to this client
					msg.Message = game.Position().Board().Draw() // Update the message to the new board position
					err := client.Conn.WriteJSON(msg)
					if err != nil {
						fmt.Println(err)
						client.Conn.Close()
						delete(clients, clientConn)
					}
				}
				break // Exit the for loop as we found the client
			}

		}
		if !clientFound {
			fmt.Printf("Client for username %s not found\n", msg.Username)
		}
	}
}
