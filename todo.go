package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/resend/resend-go/v2"
	"net/http"
	"time"
)

type Todo struct {
	Task      string    `json:"task"`
	Email     string    `json:"email"`
	Unit      string    `json:"unit"`
	AddedDate time.Time `json:"addedDate"`
	Completed bool      `json:"completed"`
}

var todos []Todo

func serveTodo(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "todo.html") // Serves the HTML file
}

func formatEmail(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var requestData struct {
		To      string `json:"to"`
		Subject string `json:"subject"`
		Message string `json:"message"`
	}

	// Parse the JSON body into the requestData struct
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&requestData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Use the provided email and tasks
	email := requestData.To
	message := requestData.Message

	// Send the email reminder
	if err := sendEmailReminder(email, message); err != nil {
		http.Error(w, "Failed to send email reminder", http.StatusInternalServerError)
		return
	}

	// Respond with success
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "Email sent"})
}

func sendEmailReminder(email string, message string) error {
	ctx := context.TODO()
	client := resend.NewClient("re_")

	params := &resend.SendEmailRequest{
		From:    "Todo App <reminder@alexer.dev>",
		To:      []string{email},
		Subject: "Todo Reminder",
		Html:    fmt.Sprintf("<p>%s</p>", message),
	}

	sent, err := client.Emails.SendWithContext(ctx, params)

	if err != nil {
		return err
	}

	fmt.Println(sent.Id)
	return nil
}
