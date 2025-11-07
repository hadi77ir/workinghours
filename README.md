# Hours Tracker Web Application

A simple and elegant web application built with **Go Fiber v2**, **Handlebars**, **GORM**, **Bulma**, and **HTMX** to track work rounds (sessions with start and end times) with automatic status updates.

## ✨ Features

- 🎯 **Round-Based Tracking**: Track work sessions as rounds with start and end times
- 🚫 **Prevents Invalid States**: Cannot start multiple consecutive rounds or stop when nothing is running
- 📊 **Real-time Status**: View the current round state (in progress or not started) with visual indicators
- 🧩 **Working Groups**: Organize rounds by customizable working groups with per-group dashboards
- 📈 **Daily Statistics**: View daily summaries with total hours in HH:mm:ss format
- 🔄 **Auto-refresh**: Status automatically refreshes every 30 seconds using HTMX
- 💾 **Persistent Storage**: All rounds stored in SQLite database using GORM
- 📥 **CSV Export**: Download all your rounds with durations as a CSV file
- 📦 **Self-Contained Binary**: Templates and static assets embedded using go:embed - just copy and run!
- 🔌 **Works Offline**: All CSS/JS embedded - no CDN dependencies
- 🎨 **Modern UI**: Beautiful, responsive interface built with Bulma CSS framework
- ⚡ **HTMX Integration**: Dynamic updates without page reloads or complex JavaScript
- 🚦 **Smart Buttons**: Buttons automatically enable/disable based on current round state

## 🛠️ Technologies Used

| Category | Technology |
|----------|------------|
| **Backend** | Go with Fiber v2 framework |
| **Templating** | Handlebars |
| **Database** | SQLite with GORM ORM |
| **Frontend CSS** | Bulma CSS Framework |
| **Frontend JS** | HTMX for dynamic updates |

## 🚀 Quick Start

### Prerequisites

- Go 1.22 or higher
- Git (optional)

### Installation & Running

1. **Clone or navigate to the project directory**:
```bash
git clone github.com/hadi77ir/workinghours
```

2. **Install Go dependencies**:
```bash
go mod download
```

3. **Run the application** (choose one):
  **Option A: Using go run**
   ```bash
   go run main.go
   ```

   **Option B: Build and run (recommended for deployment)**
   ```bash
   go build -o workinghours
   ./workinghours
   ```
   
   The binary is **self-contained** with embedded templates - you can copy it anywhere!

4. **Open your browser** and navigate to:
   ```
   http://localhost:3000
   ```


## ⚙️ Configuration

The application can be configured using environment variables:

### SERVER_ADDR

Controls the server listening address and port.

**Default:** `:3000` (listens on all interfaces, port 3000)

**Examples:**
```bash
# Listen on all interfaces, port 3000 (default)
./workinghours

# Listen on all interfaces, port 8080
SERVER_ADDR=:8080 ./workinghours

# Listen on localhost only, port 3000
SERVER_ADDR=localhost:3000 ./workinghours

# Listen on specific IP, port 80
SERVER_ADDR=192.168.1.100:80 ./workinghours

# Listen on all interfaces, port 80 (requires root)
SERVER_ADDR=:80 ./workinghours
```

## 📖 Usage

1. **Choose a Working Group**:
   - Use the selector at the top of the home page to pick a working group
   - The dashboard instantly updates totals and status for the selected group
   - Click **Manage Groups** to add, rename, or delete working groups

2. **Starting a Round**:
   - Click the green **Start Round** button to begin tracking a work session
   - A new round is created for the selected working group with the current timestamp
   - The status indicator turns green and animates while running

3. **Ending a Round**:
   - Click the red **End Round** button to finish the current session
   - The round is stamped with an end time and the duration is calculated automatically
   - Buttons toggle states to prevent starting or stopping twice in a row

4. **Viewing Totals**:
   - Cards show **Total Today** and **Total (All Time)** for the selected working group
   - **Total (All Working Groups)** aggregates every group, including active rounds
   - Switch groups from the selector to compare totals instantly

5. **Viewing Statistics**:
   - Visit the **Daily Statistics** page to browse by working group and see daily breakdowns
   - Review overall totals per group and export data for further analysis

6. **Resetting a Working Group**:
   - Use the **Reset Working Group** button to delete all rounds for the selected group
   - A confirmation dialog prevents accidental resets (this action cannot be undone)

7. **Exporting Data**:
   - Click **Export Rounds to CSV** (home or stats page) to download only the currently selected working group
   - The CSV includes Round ID, Working Group, Start/End times, duration in minutes, and status
   - Filename format: `workinghours-<group>-YYYY-MM-DD-HHMMSS.csv`
   - Perfect for importing into spreadsheets or reporting tools

## 🧩 Working Groups

- Visit `/groups/manage` to add, rename, or delete working groups
- Each group displays its cumulative total time for quick comparisons
- Groups with recorded rounds must be reset before they can be deleted
- The last remaining working group cannot be removed to ensure valid tracking
- Use the reset button on the home page to clear all rounds for a specific group

## 💾 Database

The application creates a `hours.db` SQLite database file in the project root directory on first run. This file contains:

- **Rounds Table**: Stores all work rounds with start and end times

### Database Schema

```go
type WorkingGroup struct {
    ID        uint      // Primary key
    Name      string    // Unique name for the group
    CreatedAt time.Time
    UpdatedAt time.Time
}

type Round struct {
    ID             uint       // Primary key
    StartTime      time.Time  // When the round started
    EndTime        *time.Time // When the round ended (NULL = in progress)
    WorkingGroupID uint       // Associated working group
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

**Note:** Both `views/` and `public/` directories are embedded into the binary at compile time using `go:embed`. 
The compiled `workinghours` binary can run standalone without any external files - completely offline capable!

## 🔧 How It Works

### Backend (Go + Fiber + GORM)

1. **Fiber** serves the web application on port 3000
2. **GORM** manages the SQLite database with auto-migrations
3. **Logger** configured to Silent mode for clean console output (no "record not found" spam)
4. **go:embed** embeds templates into the binary for deployment
5. Routes handle:
   - `GET /` - Renders the main page
   - `GET /status` - Returns current status HTML partial
   - `GET /stats` - Renders daily statistics page with totals
   - `POST /start` - Creates a new round (validates no unfinished round exists)
   - `POST /stop` - Ends the current round (validates an unfinished round exists)
   - `POST /groups/reset` - Clears all rounds for a working group (with confirmation)
   - `GET /groups/manage` - Working group management UI (add/edit/remove)
   - `POST /groups` - Creates a new working group
   - `POST /groups/:id/update` - Renames an existing working group
   - `POST /groups/:id/delete` - Deletes a working group with no recorded rounds
   - `GET /export/csv` - Exports all rounds with durations and working group names to CSV

### Frontend (Handlebars + Bulma + HTMX)

1. **Handlebars** templates render the UI
2. **Bulma** provides responsive, modern styling
3. **HTMX** handles:
   - Button clicks to send POST requests
   - Automatic polling every 30 seconds
   - Partial HTML updates without full page reloads

### State Management

The application determines the current state by:
- Checking for any round with `EndTime = NULL` (unfinished round)
  - If found: Status is "In Progress" - Start button disabled, Stop button enabled
  - If not found: Status is "Not Started" - Start button enabled, Stop button disabled
- This simple check prevents all invalid states:
  - Cannot start when already running (would create multiple unfinished rounds)
  - Cannot stop when not running (no unfinished round to update)

## 🎨 UI Features

- **Gradient Hero Section**: Eye-catching purple gradient header
- **Status Box**: Clean, card-based layout for status information
- **Animated Indicator**: Pulsing green dot when running, static red dot when stopped
- **Notification Boxes**: Color-coded info boxes for start/stop times
- **Responsive Design**: Works on desktop and mobile devices
- **Disabled States**: Buttons automatically disable when not applicable

## 🤝 Contributing

Feel free to fork this project and submit pull requests for any improvements!

## 📝 License

MIT

> **Note:** This project and its source code are completely AI generated.
