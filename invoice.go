package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/jung-kurt/gofpdf"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

type Invoice struct {
	InvoiceID   string    `json:"invoice_id"`
	Date        time.Time `json:"date"`
	Customer    string    `json:"customer"`
	Items       []Item    `json:"items"`
	TotalAmount float64   `json:"total_amount"`
}

type Item struct {
	Description string  `json:"description"`
	Quantity    int     `json:"quantity"`
	Price       float64 `json:"price"`
	Amount      float64 `json:"amount"`
}

func invoicePage(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "invoice.html") // Serve the main.html file
}

func generateInvoiceHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqData struct {
		Customer   string `json:"customer"`
		Items      []Item `json:"items"`
		LogoURL    string `json:"logoURL"`
		ColorTheme string `json:"colorTheme"`
	}

	if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if len(reqData.Items) == 0 {
		http.Error(w, "At least one item is required", http.StatusBadRequest)
		return
	}

	totalAmount := 0.0
	for i, item := range reqData.Items {
		if item.Quantity <= 0 || item.Price <= 0 {
			http.Error(w, fmt.Sprintf("Invalid quantity or price for item %d", i+1), http.StatusBadRequest)
			return
		}
		item.Amount = float64(item.Quantity) * item.Price
		reqData.Items[i].Amount = item.Amount
		totalAmount += item.Amount
	}

	invoice := Invoice{
		InvoiceID:   fmt.Sprintf("INV-%d", time.Now().Unix()),
		Date:        time.Now(),
		Customer:    reqData.Customer,
		Items:       reqData.Items,
		TotalAmount: totalAmount,
	}

	// Generate PDF
	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.AddPage()

	pdf.SetTextColor(parseColor(reqData.ColorTheme))
	// Add Logo if provided
	if reqData.LogoURL != "" {
		response, err := http.Get(reqData.LogoURL)
		if err != nil {
			log.Printf("Failed to download logo: %v", err)
		} else {
			defer response.Body.Close()
			imgFilePath := "/temp/logo.png" // Path to save the logo temporarily
			file, err := os.Create(imgFilePath)
			if err != nil {
				log.Printf("Failed to create temp file for logo: %v", err)
			} else {
				defer file.Close()
				_, err := io.Copy(file, response.Body)
				if err != nil {
					log.Printf("Failed to save logo to temp file: %v", err)
				} else {

					pdf.Image(imgFilePath, 170, 10, 30, 0, false, "", 0, "")
				}
			}
		}
	}

	pdf.SetFont("Arial", "B", 16)
	pdf.Cell(0, 10, "Invoice")
	pdf.Ln(20)

	pdf.SetFont("Arial", "", 12)
	pdf.Cell(0, 10, fmt.Sprintf("Invoice ID: %s", invoice.InvoiceID))
	pdf.Ln(6)
	pdf.Cell(0, 10, fmt.Sprintf("Date: %s", invoice.Date.Format("2006-01-02")))
	pdf.Ln(6)
	pdf.Cell(0, 10, fmt.Sprintf("Customer: %s", invoice.Customer))
	pdf.Ln(10)

	// Table Header
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(60, 10, "Item")
	pdf.Cell(30, 10, "Quantity")
	pdf.Cell(30, 10, "Price")
	pdf.Cell(30, 10, "Amount")
	pdf.Ln(10)

	// Table Content
	pdf.SetFont("Arial", "", 12)
	for _, item := range invoice.Items {
		pdf.Cell(60, 10, item.Description)
		pdf.Cell(30, 10, fmt.Sprintf("%d", item.Quantity))
		pdf.Cell(30, 10, fmt.Sprintf("%.2f", item.Price))
		pdf.Cell(30, 10, fmt.Sprintf("%.2f", item.Amount))
		pdf.Ln(10)
	}

	// Total Amount
	pdf.SetFont("Arial", "B", 12)
	pdf.Cell(60, 10, "")
	pdf.Cell(30, 10, "")
	pdf.Cell(30, 10, "Total")
	pdf.Cell(30, 10, fmt.Sprintf("%.2f", invoice.TotalAmount))
	pdf.Ln(10)

	// Generate PDF bytes
	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		http.Error(w, "Failed to generate PDF", http.StatusInternalServerError)
		return
	}

	// Send the PDF as a response
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=invoice_%s.pdf", invoice.InvoiceID))
	w.WriteHeader(http.StatusOK)
	w.Write(buf.Bytes())
}
func parseColor(hexColor string) (int, int, int) {
	// Ensure the hex string starts with '#'
	if len(hexColor) != 7 || hexColor[0] != '#' {
		return 0, 0, 0 // Invalid hex color
	}

	// Parse the hex color string to integers
	r, err := strconv.ParseInt(hexColor[1:3], 16, 0)
	if err != nil {
		return 0, 0, 0 // Error parsing red component
	}

	g, err := strconv.ParseInt(hexColor[3:5], 16, 0)
	if err != nil {
		return 0, 0, 0 // Error parsing green component
	}

	b, err := strconv.ParseInt(hexColor[5:7], 16, 0)
	if err != nil {
		return 0, 0, 0 // Error parsing blue component
	}

	return int(r), int(g), int(b)
}
