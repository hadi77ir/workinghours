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
	"sort"
	"strconv"
	"strings"
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
type WorkingGroup struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"unique;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Rounds    []Round
}

type Round struct {
	ID             uint       `gorm:"primaryKey"`
	StartTime      time.Time  `gorm:"not null"`
	EndTime        *time.Time `gorm:"index"` // NULL means round is still in progress
	WorkingGroupID uint       `gorm:"index"`
	WorkingGroup   WorkingGroup
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

var db *gorm.DB

// AppState represents the current state of the application
type AppState struct {
	GroupID               uint
	GroupName             string
	LastStartTime         *time.Time
	LastStopTime          *time.Time
	IsRunning             bool
	LastStartStr          string
	LastStopStr           string
	CurrentRoundID        *uint
	TotalTodaySeconds     int64
	TotalTodayFormatted   string
	TotalOverallSeconds   int64
	TotalOverallFormatted string
}

type StatusGroupOption struct {
	ID       uint
	Name     string
	Selected bool
}

type StatusContext struct {
	GroupOptions            []StatusGroupOption
	SelectedGroupID         uint
	State                   AppState
	AllGroupsTotalSeconds   int64
	AllGroupsTotalFormatted string
}

type GroupTotal struct {
	GroupID        uint
	GroupName      string
	TotalSeconds   int64
	TotalFormatted string
}

// DailySummary represents the total hours worked for a specific day
type DailySummary struct {
	GroupID        uint
	GroupName      string
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
	err = db.AutoMigrate(&WorkingGroup{}, &Round{})
	if err != nil {
		log.Fatal("Failed to migrate database:", err)
	}

	// Ensure at least one working group exists and backfill existing rounds
	ensureDefaultWorkingGroup()

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
	app.Post("/groups/reset", resetWorkingGroupHandler)
	app.Get("/groups/manage", renderGroupManagement)
	app.Post("/groups", createWorkingGroupHandler)
	app.Post("/groups/:id/update", updateWorkingGroupHandler)
	app.Post("/groups/:id/delete", deleteWorkingGroupHandler)

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
	var requestedGroupID uint
	if groupParam := c.Query("group_id"); groupParam != "" {
		if parsed, err := parseGroupID(groupParam); err == nil {
			requestedGroupID = parsed
		}
	}

	context, err := buildStatusContext(requestedGroupID)
	if err != nil {
		log.Println("Error building status context:", err)
		return c.Status(500).SendString("Error rendering page")
	}

	return c.Render("index", fiber.Map{
		"GroupOptions":            context.GroupOptions,
		"SelectedGroupID":         context.SelectedGroupID,
		"State":                   context.State,
		"AllGroupsTotalSeconds":   context.AllGroupsTotalSeconds,
		"AllGroupsTotalFormatted": context.AllGroupsTotalFormatted,
	})
}

func getStatus(c *fiber.Ctx) error {
	var requestedGroupID uint
	if groupParam := c.Query("group_id"); groupParam != "" {
		if parsed, err := parseGroupID(groupParam); err == nil {
			requestedGroupID = parsed
		}
	}

	context, err := buildStatusContext(requestedGroupID)
	if err != nil {
		log.Println("Error building status context:", err)
		return c.Status(500).SendString("Error rendering status")
	}

	return renderStatusTemplate(c, context)
}

func renderStatusTemplate(c *fiber.Ctx, context StatusContext) error {
	return c.Render("status", fiber.Map{
		"GroupOptions":            context.GroupOptions,
		"SelectedGroupID":         context.SelectedGroupID,
		"State":                   context.State,
		"AllGroupsTotalSeconds":   context.AllGroupsTotalSeconds,
		"AllGroupsTotalFormatted": context.AllGroupsTotalFormatted,
	})
}

func handleStart(c *fiber.Ctx) error {
	groupIDStr := c.FormValue("group_id")
	if groupIDStr == "" {
		return c.Status(400).SendString("Working group is required")
	}

	groupID, err := parseGroupID(groupIDStr)
	if err != nil {
		return c.Status(400).SendString("Invalid working group")
	}

	var group WorkingGroup
	if err := db.First(&group, groupID).Error; err != nil {
		return c.Status(404).SendString("Working group not found")
	}

	// Ensure no active round for this group
	var activeRound Round
	if err := db.Where("working_group_id = ? AND end_time IS NULL", groupID).First(&activeRound).Error; err == nil {
		return c.Status(400).SendString("Cannot start: this working group already has a running round")
	}

	round := Round{
		StartTime:      time.Now(),
		WorkingGroupID: groupID,
	}

	if err := db.Create(&round).Error; err != nil {
		log.Println("Error creating round:", err)
		return c.Status(500).SendString("Error starting round")
	}

	log.Printf("Started new round #%d for group '%s' at %s", round.ID, group.Name, round.StartTime.Format("2006-01-02 15:04:05"))

	context, err := buildStatusContext(groupID)
	if err != nil {
		log.Println("Error building status context:", err)
		return c.Status(500).SendString("Error rendering status")
	}

	return renderStatusTemplate(c, context)
}

func handleStop(c *fiber.Ctx) error {
	groupIDStr := c.FormValue("group_id")
	if groupIDStr == "" {
		return c.Status(400).SendString("Working group is required")
	}

	groupID, err := parseGroupID(groupIDStr)
	if err != nil {
		return c.Status(400).SendString("Invalid working group")
	}

	var group WorkingGroup
	if err := db.First(&group, groupID).Error; err != nil {
		return c.Status(404).SendString("Working group not found")
	}

	var activeRound Round
	if err := db.Where("working_group_id = ? AND end_time IS NULL", groupID).First(&activeRound).Error; err != nil {
		return c.Status(400).SendString("Cannot stop: no round is running for this working group")
	}

	now := time.Now()
	activeRound.EndTime = &now
	if err := db.Save(&activeRound).Error; err != nil {
		log.Println("Error updating round:", err)
		return c.Status(500).SendString("Error stopping round")
	}

	duration := now.Sub(activeRound.StartTime)
	log.Printf("Stopped round #%d for group '%s' at %s (duration: %s)",
		activeRound.ID,
		group.Name,
		now.Format("2006-01-02 15:04:05"),
		duration.Round(time.Second))

	context, err := buildStatusContext(groupID)
	if err != nil {
		log.Println("Error building status context:", err)
		return c.Status(500).SendString("Error rendering status")
	}

	return renderStatusTemplate(c, context)
}

func resetWorkingGroupHandler(c *fiber.Ctx) error {
	groupIDStr := c.FormValue("group_id")
	if groupIDStr == "" {
		return c.Status(400).SendString("Working group is required for reset")
	}

	groupID, err := parseGroupID(groupIDStr)
	if err != nil {
		return c.Status(400).SendString("Invalid working group")
	}

	var group WorkingGroup
	if err := db.First(&group, groupID).Error; err != nil {
		return c.Status(404).SendString("Working group not found")
	}

	if err := db.Where("working_group_id = ?", groupID).Delete(&Round{}).Error; err != nil {
		log.Println("Error resetting working group rounds:", err)
		return c.Status(500).SendString("Error resetting working group")
	}

	log.Printf("Reset all rounds for working group '%s'", group.Name)

	context, err := buildStatusContext(groupID)
	if err != nil {
		log.Println("Error building status context:", err)
		return c.Status(500).SendString("Error rendering status")
	}

	return renderStatusTemplate(c, context)
}

func renderGroupManagement(c *fiber.Ctx) error {
	groups, err := getWorkingGroupsOrdered()
	if err != nil {
		log.Println("Error fetching working groups:", err)
		return c.Status(500).SendString("Error loading working group management")
	}

	if len(groups) == 0 {
		defaultGroup := ensureDefaultWorkingGroup()
		groups = []WorkingGroup{defaultGroup}
	}

	var groupViews []fiber.Map
	for _, group := range groups {
		_, total := calculateGroupTotals(group.ID)
		groupViews = append(groupViews, fiber.Map{
			"ID":             group.ID,
			"Name":           group.Name,
			"TotalFormatted": formatDuration(total),
			"HasRounds":      total > 0,
		})
	}

	return c.Render("groups", fiber.Map{
		"Groups": groupViews,
	})
}

func createWorkingGroupHandler(c *fiber.Ctx) error {
	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("Group name cannot be empty")
	}

	group := WorkingGroup{Name: name}
	if err := db.Create(&group).Error; err != nil {
		log.Println("Error creating working group:", err)
		return c.Status(500).SendString("Error creating working group")
	}

	return c.Redirect("/groups/manage", fiber.StatusSeeOther)
}

func updateWorkingGroupHandler(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := parseGroupID(idParam)
	if err != nil {
		return c.Status(400).SendString("Invalid working group")
	}

	name := strings.TrimSpace(c.FormValue("name"))
	if name == "" {
		return c.Status(400).SendString("Group name cannot be empty")
	}

	if err := db.Model(&WorkingGroup{}).Where("id = ?", id).Update("name", name).Error; err != nil {
		log.Println("Error updating working group:", err)
		return c.Status(500).SendString("Error updating working group")
	}

	return c.Redirect("/groups/manage", fiber.StatusSeeOther)
}

func deleteWorkingGroupHandler(c *fiber.Ctx) error {
	idParam := c.Params("id")
	id, err := parseGroupID(idParam)
	if err != nil {
		return c.Status(400).SendString("Invalid working group")
	}

	var totalGroups int64
	if err := db.Model(&WorkingGroup{}).Count(&totalGroups).Error; err != nil {
		log.Println("Error counting working groups:", err)
		return c.Status(500).SendString("Error deleting working group")
	}

	if totalGroups <= 1 {
		return c.Status(400).SendString("Cannot delete the last remaining working group")
	}

	var roundCount int64
	if err := db.Model(&Round{}).Where("working_group_id = ?", id).Count(&roundCount).Error; err != nil {
		log.Println("Error counting rounds for group:", err)
		return c.Status(500).SendString("Error deleting working group")
	}

	if roundCount > 0 {
		return c.Status(400).SendString("Cannot delete working group with recorded rounds. Reset the group first.")
	}

	if err := db.Delete(&WorkingGroup{}, id).Error; err != nil {
		log.Println("Error deleting working group:", err)
		return c.Status(500).SendString("Error deleting working group")
	}

	return c.Redirect("/groups/manage", fiber.StatusSeeOther)
}

func exportToCSV(c *fiber.Ctx) error {
	groupIDParam := c.Query("group_id")
	var groupFilter uint
	var groupName string

	query := db.Preload("WorkingGroup").Order("start_time ASC")
	if groupIDParam != "" {
		parsedID, err := parseGroupID(groupIDParam)
		if err != nil {
			return c.Status(400).SendString("Invalid working group")
		}
		groupFilter = parsedID

		var group WorkingGroup
		if err := db.First(&group, groupFilter).Error; err != nil {
			return c.Status(404).SendString("Working group not found")
		}
		groupName = group.Name
		if groupName == "" {
			groupName = fmt.Sprintf("Group-%d", groupFilter)
		}

		query = query.Where("working_group_id = ?", groupFilter)
	}

	var rounds []Round
	if err := query.Find(&rounds).Error; err != nil {
		log.Println("Error fetching rounds for CSV export:", err)
		return c.Status(500).SendString("Error exporting data")
	}

	if groupName == "" {
		groupName = "all-groups"
	}

	buf := new(bytes.Buffer)
	writer := csv.NewWriter(buf)

	header := []string{"Round ID", "Working Group", "Start Time", "End Time", "Duration (minutes)", "Status"}
	if err := writer.Write(header); err != nil {
		log.Println("Error writing CSV header:", err)
		return c.Status(500).SendString("Error generating CSV")
	}

	now := time.Now()

	for _, round := range rounds {
		endTimeStr := ""
		durationMinutes := 0.0
		status := "In Progress"

		if round.EndTime != nil {
			endTimeStr = round.EndTime.Format("2006-01-02 15:04:05")
			durationMinutes = round.EndTime.Sub(round.StartTime).Minutes()
			status = "Completed"
		} else {
			durationMinutes = now.Sub(round.StartTime).Minutes()
		}

		groupName := round.WorkingGroup.Name
		if groupName == "" {
			groupName = fmt.Sprintf("Group #%d", round.WorkingGroupID)
		}

		row := []string{
			fmt.Sprintf("%d", round.ID),
			groupName,
			round.StartTime.Format("2006-01-02 15:04:05"),
			endTimeStr,
			fmt.Sprintf("%.2f", durationMinutes),
			status,
		}

		if err := writer.Write(row); err != nil {
			log.Println("Error writing CSV row:", err)
			return c.Status(500).SendString("Error generating CSV")
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Println("Error flushing CSV writer:", err)
		return c.Status(500).SendString("Error generating CSV")
	}

	filename := fmt.Sprintf("workinghours-%s-%s.csv", groupName, time.Now().Format("2006-01-02-150405"))
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))

	return c.Send(buf.Bytes())
}

func renderStats(c *fiber.Ctx) error {
	groups, err := getWorkingGroupsOrdered()
	if err != nil {
		log.Println("Error fetching working groups:", err)
		return c.Status(500).SendString("Error rendering statistics")
	}

	if len(groups) == 0 {
		defaultGroup := ensureDefaultWorkingGroup()
		groups = []WorkingGroup{defaultGroup}
	}

	selectedGroupID := groups[0].ID
	if groupParam := c.Query("group_id"); groupParam != "" {
		if parsed, err := parseGroupID(groupParam); err == nil {
			if _, exists := findGroupByID(groups, parsed); exists {
				selectedGroupID = parsed
			}
		}
	}

	dailySummaries := getDailySummaries(selectedGroupID)
	groupTotals := getGroupTotalsSummary()
	todaySeconds, totalSeconds := calculateGroupTotals(selectedGroupID)
	allGroupsTotal := calculateAllGroupsTotalSeconds()

	selectedGroupName := fmt.Sprintf("Group #%d", selectedGroupID)
	var groupOptions []StatusGroupOption
	for _, group := range groups {
		if group.ID == selectedGroupID {
			selectedGroupName = group.Name
		}
		groupOptions = append(groupOptions, StatusGroupOption{
			ID:       group.ID,
			Name:     group.Name,
			Selected: group.ID == selectedGroupID,
		})
	}

	return c.Render("stats", fiber.Map{
		"GroupOptions":                groupOptions,
		"SelectedGroupID":             selectedGroupID,
		"SelectedGroupName":           selectedGroupName,
		"DailySummaries":              dailySummaries,
		"GroupTotals":                 groupTotals,
		"SelectedGroupTotalFormatted": formatDuration(totalSeconds),
		"SelectedGroupTodayFormatted": formatDuration(todaySeconds),
		"AllGroupsTotalFormatted":     formatDuration(allGroupsTotal),
	})
}

func getDailySummaries(groupID uint) []DailySummary {
	var rounds []Round
	if err := db.Where("working_group_id = ? AND end_time IS NOT NULL", groupID).
		Order("start_time DESC").Find(&rounds).Error; err != nil {
		return []DailySummary{}
	}

	var group WorkingGroup
	db.First(&group, groupID)
	groupName := group.Name
	if groupName == "" {
		groupName = fmt.Sprintf("Group #%d", groupID)
	}

	dailyMap := make(map[string]*DailySummary)

	for _, round := range rounds {
		dateKey := round.StartTime.Format("2006-01-02")
		duration := round.EndTime.Sub(round.StartTime)
		seconds := int64(duration.Seconds())

		if summary, exists := dailyMap[dateKey]; exists {
			summary.TotalSeconds += seconds
			summary.RoundCount++
		} else {
			dailyMap[dateKey] = &DailySummary{
				GroupID:        groupID,
				GroupName:      groupName,
				Date:           dateKey,
				DateDisplay:    round.StartTime.Format("Monday, January 2, 2006"),
				TotalSeconds:   seconds,
				TotalFormatted: "",
				RoundCount:     1,
			}
		}
	}

	var summaries []DailySummary
	for _, summary := range dailyMap {
		summary.TotalFormatted = formatDuration(summary.TotalSeconds)
		summaries = append(summaries, *summary)
	}

	// Sort by date descending
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].Date > summaries[j].Date
	})

	return summaries
}

func ensureDefaultWorkingGroup() WorkingGroup {
	var group WorkingGroup
	result := db.Order("id ASC").First(&group)
	if result.Error != nil {
		group = WorkingGroup{Name: "General"}
		if err := db.Create(&group).Error; err != nil {
			log.Fatal("Failed to create default working group:", err)
		}
	}

	// Backfill existing rounds without a working group
	if err := db.Model(&Round{}).
		Where("working_group_id IS NULL OR working_group_id = 0").
		Update("working_group_id", group.ID).Error; err != nil {
		log.Println("Warning: failed to backfill working group for existing rounds:", err)
	}

	return group
}

func getWorkingGroupsOrdered() ([]WorkingGroup, error) {
	var groups []WorkingGroup
	if err := db.Order("name ASC").Find(&groups).Error; err != nil {
		return nil, err
	}
	return groups, nil
}

func findGroupByID(groups []WorkingGroup, id uint) (*WorkingGroup, bool) {
	for i := range groups {
		if groups[i].ID == id {
			return &groups[i], true
		}
	}
	return nil, false
}

func parseGroupID(value string) (uint, error) {
	parsed, err := strconv.ParseUint(value, 10, 64)
	if err != nil {
		return 0, err
	}
	return uint(parsed), nil
}

func formatDuration(seconds int64) string {
	if seconds < 0 {
		seconds = 0
	}
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	secs := seconds % 60
	return fmt.Sprintf("%02d:%02d:%02d", hours, minutes, secs)
}

func calculateGroupTotals(groupID uint) (int64, int64) {
	var rounds []Round
	if err := db.Where("working_group_id = ?", groupID).Find(&rounds).Error; err != nil {
		return 0, 0
	}

	now := time.Now()
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	todayEnd := todayStart.Add(24 * time.Hour)

	var totalSeconds int64
	var todaySeconds int64

	for _, round := range rounds {
		endTime := round.EndTime
		if endTime == nil {
			// Still running - include time up to now
			duration := now.Sub(round.StartTime)
			seconds := int64(duration.Seconds())
			totalSeconds += seconds
			if !round.StartTime.Before(todayStart) && round.StartTime.Before(todayEnd) {
				todaySeconds += seconds
			}
			continue
		}
		duration := endTime.Sub(round.StartTime)
		seconds := int64(duration.Seconds())
		totalSeconds += seconds
		if !round.StartTime.Before(todayStart) && round.StartTime.Before(todayEnd) {
			todaySeconds += seconds
		}
	}

	return todaySeconds, totalSeconds
}

func calculateAllGroupsTotalSeconds() int64 {
	var rounds []Round
	if err := db.Find(&rounds).Error; err != nil {
		return 0
	}
	now := time.Now()
	var totalSeconds int64
	for _, round := range rounds {
		endTime := round.EndTime
		if endTime == nil {
			totalSeconds += int64(now.Sub(round.StartTime).Seconds())
			continue
		}
		totalSeconds += int64(endTime.Sub(round.StartTime).Seconds())
	}
	return totalSeconds
}

func getGroupTotalsSummary() []GroupTotal {
	groups, err := getWorkingGroupsOrdered()
	if err != nil {
		return []GroupTotal{}
	}

	var summaries []GroupTotal
	for _, group := range groups {
		_, total := calculateGroupTotals(group.ID)
		summaries = append(summaries, GroupTotal{
			GroupID:        group.ID,
			GroupName:      group.Name,
			TotalSeconds:   total,
			TotalFormatted: formatDuration(total),
		})
	}
	return summaries
}

func buildStatusContext(requestedGroupID uint) (StatusContext, error) {
	groups, err := getWorkingGroupsOrdered()
	if err != nil {
		return StatusContext{}, err
	}

	if len(groups) == 0 {
		defaultGroup := ensureDefaultWorkingGroup()
		groups = []WorkingGroup{defaultGroup}
	}

	selectedGroupID := requestedGroupID
	if selectedGroupID == 0 {
		selectedGroupID = groups[0].ID
	} else if _, exists := findGroupByID(groups, selectedGroupID); !exists {
		selectedGroupID = groups[0].ID
	}

	state := getCurrentState(selectedGroupID)
	allTotal := calculateAllGroupsTotalSeconds()

	var options []StatusGroupOption
	for _, group := range groups {
		options = append(options, StatusGroupOption{
			ID:       group.ID,
			Name:     group.Name,
			Selected: group.ID == selectedGroupID,
		})
	}

	return StatusContext{
		GroupOptions:            options,
		SelectedGroupID:         selectedGroupID,
		State:                   state,
		AllGroupsTotalSeconds:   allTotal,
		AllGroupsTotalFormatted: formatDuration(allTotal),
	}, nil
}

func getCurrentState(groupID uint) AppState {
	state := AppState{
		GroupID:               groupID,
		GroupName:             fmt.Sprintf("Group #%d", groupID),
		LastStartStr:          "Never",
		LastStopStr:           "Never",
		IsRunning:             false,
		TotalTodayFormatted:   "00:00:00",
		TotalOverallFormatted: "00:00:00",
	}

	if groupID != 0 {
		var group WorkingGroup
		if err := db.First(&group, groupID).Error; err == nil {
			state.GroupName = group.Name
		}
	}

	var activeRound Round
	if err := db.Where("working_group_id = ? AND end_time IS NULL", groupID).
		Order("start_time DESC").First(&activeRound).Error; err == nil {
		state.IsRunning = true
		state.CurrentRoundID = &activeRound.ID
		state.LastStartTime = &activeRound.StartTime
		state.LastStartStr = activeRound.StartTime.Format("2006-01-02 15:04:05")
		state.LastStopStr = "In progress..."
	} else {
		var lastRound Round
		if err := db.Where("working_group_id = ? AND end_time IS NOT NULL", groupID).
			Order("end_time DESC").First(&lastRound).Error; err == nil {
			state.LastStartTime = &lastRound.StartTime
			state.LastStopTime = lastRound.EndTime
			state.LastStartStr = lastRound.StartTime.Format("2006-01-02 15:04:05")
			state.LastStopStr = lastRound.EndTime.Format("2006-01-02 15:04:05")
		}
	}

	todaySeconds, totalSeconds := calculateGroupTotals(groupID)
	state.TotalTodaySeconds = todaySeconds
	state.TotalOverallSeconds = totalSeconds
	state.TotalTodayFormatted = formatDuration(todaySeconds)
	state.TotalOverallFormatted = formatDuration(totalSeconds)

	return state
}
