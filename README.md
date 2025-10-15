# Hours Tracker Web Application

A simple and elegant web application built with **Go Fiber v2**, **Handlebars**, **GORM**, **Bulma**, and **HTMX** to track work rounds (sessions with start and end times) with automatic status updates.

## ✨ Features

- 🎯 **Round-Based Tracking**: Track work sessions as rounds with start and end times
- 🚫 **Prevents Invalid States**: Cannot start multiple consecutive rounds or stop when nothing is running
- 📊 **Real-time Status**: View the current round state (in progress or not started) with visual indicators
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

1. **Starting a Round**:
   - Click the green **Start Round** button to begin tracking a work session
   - A new round is created in the database with the current timestamp
   - The status indicator will turn green and animate
   - The Start button becomes disabled (prevents multiple consecutive starts)
   - The Stop button becomes active

2. **Ending a Round**:
   - Click the red **End Round** button to finish the current session
   - The round is updated with the end timestamp
   - Duration is automatically calculated
   - The status indicator turns red
   - The Stop button becomes disabled (prevents multiple consecutive stops)
   - The Start button becomes active again

3. **Viewing Status**:
   - **Round Status**: Shows "In Progress" or "Not Started" with a color-coded indicator
   - **Current/Last Round Started**: Shows when the current or last round started
   - **Current Round Status/Last Round Ended**: Shows "In progress..." or the end timestamp
   - The page automatically refreshes every 30 seconds

4. **Viewing Statistics**:
   - Click the **View Statistics** button to see daily summaries
   - Table shows each day with:
     - Date in readable format
     - Number of rounds completed
     - Total time worked in HH:mm:ss format
   - Days are sorted from most recent to oldest
   - Perfect for tracking productivity patterns

5. **Exporting Data**:
   - Click the **Export Rounds to CSV** button to download all your tracking data
   - The CSV file includes: Round ID, Start Time, End Time, Duration (minutes), Status
   - Filename format: `hours-tracker-rounds-YYYY-MM-DD-HHMMSS.csv`
   - Durations are automatically calculated for completed rounds
   - Perfect for importing into Excel, Google Sheets, or other tools

## 💾 Database

The application creates a `hours.db` SQLite database file in the project root directory on first run. This file contains:

- **Rounds Table**: Stores all work rounds with start and end times

### Database Schema

```go
type Round struct {
    ID        uint       // Primary key
    StartTime time.Time  // When the round started
    EndTime   *time.Time // When the round ended (NULL = in progress)
    CreatedAt time.Time  // Record creation timestamp
    UpdatedAt time.Time  // Record update timestamp
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
   - `GET /export/csv` - Exports all rounds with durations to CSV file

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
