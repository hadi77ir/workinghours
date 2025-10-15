package main

import (
	"bytes"
	"embed"
	"encoding/csv"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/handlebars/v2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

//go:embed views/* public/*
var embeddedFS embed.FS

// Round represents a work session with start and end times
type Round struct {
	ID        uint       `gorm:"primaryKey"`
	StartTime time.Time  `gorm:"not null"`
	EndTime   *time.Time `gorm:"index"` // NULL means round is still in progress
	CreatedAt time.Time
	UpdatedAt time.Time
}

var db *gorm.DB

// AppState represents the current state of the application
type AppState struct {
	LastStartTime  *time.Time
	LastStopTime   *time.Time
	IsRunning      bool
	LastStartStr   string
	LastStopStr    string
	CurrentRoundID *uint
}

// DailySummary represents the total hours worked for a specific day
type DailySummary struct {
	Date           string // Date in YYYY-MM-DD format
	DateDisplay    string // Date in readable format
	TotalSeconds   int64  // Total seconds worked
	TotalFormatted string // Formatted as HH:mm:ss
	RoundCount     int    // Number of rounds completed
}

func main() {
	// Initialize database with custom logger config
	// Suppress "record not found" errors as they're expected in our logic
	var err error
	db, err = gorm.Open(sqlite.Open("hours.db"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatal("Failed to connect to database:", err)
	}

	// Auto migrate the schema
	err = db.AutoMigrate(&Round{})
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Initialize Handlebars template engine with embedded filesystem
	// Wrap the embedded FS with http.FS for compatibility
	viewsSubFS, err := fs.Sub(embeddedFS, "views")
	if err != nil {
		log.Fatal("Failed to create sub filesystem:", err)
	}
	engine := handlebars.NewFileSystem(http.FS(viewsSubFS), ".hbs")

	// Create Fiber app with template engine
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	// Serve embedded static files
	publicSubFS, err := fs.Sub(embeddedFS, "public")
	if err != nil {
		log.Fatal("Failed to create public sub filesystem:", err)
	}
	app.Get("/static/*", func(c *fiber.Ctx) error {
		// Get the requested file path
		filePath := c.Params("*")

		// Read file from embedded filesystem
		fileData, err := fs.ReadFile(publicSubFS, filePath)
		if err != nil {
			return c.Status(404).SendString("File not found")
		}

		// Set appropriate content type
		if len(filePath) > 4 && filePath[len(filePath)-4:] == ".css" {
			c.Set("Content-Type", "text/css")
		} else if len(filePath) > 3 && filePath[len(filePath)-3:] == ".js" {
			c.Set("Content-Type", "application/javascript")
		}

		return c.Send(fileData)
	})

	// Routes
	app.Get("/", renderIndex)
	app.Get("/status", getStatus)
	app.Get("/stats", renderStats)
	app.Post("/start", handleStart)
	app.Post("/stop", handleStop)
	app.Get("/export/csv", exportToCSV)

	// Get server address from environment variable or use default
	serverAddr := os.Getenv("SERVER_ADDR")
	if serverAddr == "" {
		serverAddr = ":3000"
	}

	// Start server
	log.Printf("Server starting on %s", serverAddr)
	log.Fatal(app.Listen(serverAddr))
}

func renderIndex(c *fiber.Ctx) error {
	state := getCurrentState()
	return c.Render("index", fiber.Map{
		"LastStartStr": state.LastStartStr,
		"LastStopStr":  state.LastStopStr,
		"IsRunning":    state.IsRunning,
	})
}

func getStatus(c *fiber.Ctx) error {
	state := getCurrentState()
	return c.Render("status", fiber.Map{
		"LastStartStr": state.LastStartStr,
		"LastStopStr":  state.LastStopStr,
		"IsRunning":    state.IsRunning,
	})
}

func handleStart(c *fiber.Ctx) error {
	// Check if there's already an unfinished round
	var unfinishedRound Round
	result := db.Where("end_time IS NULL").First(&unfinishedRound)
	if result.Error == nil {
		// There's already an unfinished round - this shouldn't happen with proper UI
		log.Println("Attempted to start a new round while one is already in progress")
		return c.Status(400).SendString("Cannot start: a round is already in progress")
	}

	// Create a new round
	round := Round{
		StartTime: time.Now(),
		EndTime:   nil,
	}

	result = db.Create(&round)
	if result.Error != nil {
		log.Println("Error creating round:", result.Error)
		return c.Status(500).SendString("Error starting round")
	}

	log.Printf("Started new round #%d at %s", round.ID, round.StartTime.Format("2006-01-02 15:04:05"))

	state := getCurrentState()
	return c.Render("status", fiber.Map{
		"LastStartStr": state.LastStartStr,
		"LastStopStr":  state.LastStopStr,
		"IsRunning":    state.IsRunning,
	})
}

func handleStop(c *fiber.Ctx) error {
	// Find the unfinished round
	var unfinishedRound Round
	result := db.Where("end_time IS NULL").First(&unfinishedRound)
	if result.Error != nil {
		// No unfinished round found - this shouldn't happen with proper UI
		log.Println("Attempted to stop a round but none is in progress")
		return c.Status(400).SendString("Cannot stop: no round is in progress")
	}

	// Update the round with end time
	now := time.Now()
	unfinishedRound.EndTime = &now

	result = db.Save(&unfinishedRound)
	if result.Error != nil {
		log.Println("Error updating round:", result.Error)
		return c.Status(500).SendString("Error stopping round")
	}

	duration := now.Sub(unfinishedRound.StartTime)
	log.Printf("Stopped round #%d at %s (duration: %s)",
		unfinishedRound.ID,
		now.Format("2006-01-02 15:04:05"),
		duration.Round(time.Second))

	state := getCurrentState()
	return c.Render("status", fiber.Map{
		"LastStartStr": state.LastStartStr,
		"LastStopStr":  state.LastStopStr,
		"IsRunning":    state.IsRunning,
	})
}

func exportToCSV(c *fiber.Ctx) error {
	// Get all rounds from database, ordered by start time
	var rounds []Round
	result := db.Order("start_time ASC").Find(&rounds)
	if result.Error != nil {
		log.Println("Error fetching rounds for CSV export:", result.Error)
		return c.Status(500).SendString("Error exporting data")
	}

	// Create a buffer to write CSV data
	buf := new(bytes.Buffer)
	writer := csv.NewWriter(buf)

	// Write CSV header
	header := []string{"Round ID", "Start Time", "End Time", "Duration (minutes)", "Status"}
	if err := writer.Write(header); err != nil {
		log.Println("Error writing CSV header:", err)
		return c.Status(500).SendString("Error generating CSV")
	}

	// Write data rows
	for _, round := range rounds {
		endTimeStr := ""
		durationStr := ""
		status := "In Progress"

		if round.EndTime != nil {
			endTimeStr = round.EndTime.Format("2006-01-02 15:04:05")
			duration := round.EndTime.Sub(round.StartTime)
			durationStr = fmt.Sprintf("%.2f", duration.Minutes())
			status = "Completed"
		}

		row := []string{
			fmt.Sprintf("%d", round.ID),
			round.StartTime.Format("2006-01-02 15:04:05"),
			endTimeStr,
			durationStr,
			status,
		}
		if err := writer.Write(row); err != nil {
			log.Println("Error writing CSV row:", err)
			return c.Status(500).SendString("Error generating CSV")
		}
	}

	// Flush the writer
	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Println("Error flushing CSV writer:", err)
		return c.Status(500).SendString("Error generating CSV")
	}

	// Set headers for file download
	filename := fmt.Sprintf("hours-tracker-rounds-%s.csv", time.Now().Format("2006-01-02-150405"))
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	// Send the CSV data
	return c.Send(buf.Bytes())
}

func renderStats(c *fiber.Ctx) error {
	summaries := getDailySummaries()
	return c.Render("stats", fiber.Map{
		"Summaries": summaries,
	})
}

func getDailySummaries() []DailySummary {
	// Get all completed rounds
	var rounds []Round
	db.Where("end_time IS NOT NULL").Order("start_time DESC").Find(&rounds)

	// Group by date and calculate totals
	dailyMap := make(map[string]*DailySummary)

	for _, round := range rounds {
		// Get the date in YYYY-MM-DD format
		dateKey := round.StartTime.Format("2006-01-02")

		// Calculate duration
		duration := round.EndTime.Sub(round.StartTime)
		seconds := int64(duration.Seconds())

		// Update or create summary for this date
		if summary, exists := dailyMap[dateKey]; exists {
			summary.TotalSeconds += seconds
			summary.RoundCount++
		} else {
			dailyMap[dateKey] = &DailySummary{
				Date:         dateKey,
				DateDisplay:  round.StartTime.Format("Monday, January 2, 2006"),
				TotalSeconds: seconds,
				RoundCount:   1,
			}
		}
	}

	// Convert map to sorted slice
	var summaries []DailySummary
	for _, summary := range dailyMap {
		// Format total time as HH:mm:ss
		hours := summary.TotalSeconds / 3600
		minutes := (summary.TotalSeconds % 3600) / 60
		seconds := summary.TotalSeconds % 60
		summary.TotalFormatted = fmt.Sprintf("%02d:%02d:%02d", hours, minutes, seconds)

		summaries = append(summaries, *summary)
	}

	// Sort by date (most recent first)
	// Since we're iterating from a map, we need to sort
	for i := 0; i < len(summaries); i++ {
		for j := i + 1; j < len(summaries); j++ {
			if summaries[i].Date < summaries[j].Date {
				summaries[i], summaries[j] = summaries[j], summaries[i]
			}
		}
	}

	return summaries
}

func getCurrentState() AppState {
	state := AppState{
		LastStartStr: "Never",
		LastStopStr:  "Never",
		IsRunning:    false,
	}

	// Check for an unfinished round (this determines if we're currently running)
	var unfinishedRound Round
	result := db.Where("end_time IS NULL").Order("start_time DESC").First(&unfinishedRound)
	if result.Error == nil {
		// We have an unfinished round - we're currently running
		state.IsRunning = true
		state.CurrentRoundID = &unfinishedRound.ID
		state.LastStartTime = &unfinishedRound.StartTime
		state.LastStartStr = unfinishedRound.StartTime.Format("2006-01-02 15:04:05")
		state.LastStopStr = "In progress..."
	} else {
		// No unfinished round - find the most recent completed round
		var lastRound Round
		result = db.Where("end_time IS NOT NULL").Order("end_time DESC").First(&lastRound)
		if result.Error == nil {
			state.LastStartTime = &lastRound.StartTime
			state.LastStartStr = lastRound.StartTime.Format("2006-01-02 15:04:05")
			state.LastStopTime = lastRound.EndTime
			state.LastStopStr = lastRound.EndTime.Format("2006-01-02 15:04:05")
		}
	}

	return state
}
