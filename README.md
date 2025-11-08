# AirTraffic-Monitor ✈️ 

Real-time aircraft monitoring system for North London airspace, featuring live data acquisition, enrichment, and a stylized web interface with airport-style flip board animations.

## Features

- **Live Aircraft Tracking**: Automatically fetches real-time aircraft positions over North London from OpenSky Network
- **Rich Metadata**: Enriches each aircraft with registration, owner, manufacturer, type, and flight route information via adsbdb API
- **Web Dashboard**: Airport-style departure board interface with animated flip display at `http://localhost:4545`
- **JSON API**: RESTful API endpoint for programmatic access at `http://localhost:4545/api`
- **Automatic Updates**: Refreshes aircraft data every 5 minutes
- **Console Output**: Real-time formatted output in terminal

## Prerequisites

- **Go 1.22+** installed (verify with `go version`). On macOS: `brew install go`
- Internet connection for API access (OpenSky Network and adsbdb)

## Installation & Usage

Clone or navigate to the project directory and run:

```bash
cd AirTraffic-Monitor
go run .
```

The application will:
1. Start a web server on port 4545
2. Fetch initial aircraft data over North London
3. Display results in console
4. Update every 5 minutes automatically

### Accessing the Dashboard

- **Web Interface**: Open `http://localhost:4545` in your browser for the visual dashboard
- **JSON API**: Access `http://localhost:4545/api` for raw JSON data
- **Console**: View live updates in the terminal

## Coverage Area

**North London Bounding Box**: 
- Latitude: 51.50°N to 51.80°N
- Longitude: 0.50°W to 0.20°E

This covers a large area of North London including major flight paths. To adjust the coverage area, modify the parameters in the `fetchOpenSkyNorthLondon` function in `main.go`.

## Output Format

### Console Output
```text
Found 8 aircraft over North London area. Enriching via adsbdb...

Reg: G-EZBB | Owner: EASYJET AIRLINE COMPANY LIMITED | Manufacturer: Airbus | Type: A319-111 | Origin: Edinburgh Airport (EGPH) | Destination: London Gatwick Airport (EGKK)
Reg: G-EUUU | Owner: BRITISH AIRWAYS PLC | Manufacturer: Airbus | Type: A320-232 | Origin: Charles de Gaulle (LFPG) | Destination: London Heathrow (EGLL)
...
```

### JSON API Response
```json
{
  "aircraft": [
    {
      "Registration": "G-EZBB",
      "Owner": "EASYJET AIRLINE COMPANY LIMITED",
      "Manufacturer": "Airbus",
      "Type": "A319-111",
      "Origin": "Edinburgh Airport (EGPH)",
      "Destination": "London Gatwick Airport (EGKK)",
      "LastUpdated": "2025-11-08 14:23:15"
    }
  ],
  "last_update": "2025-11-08 14:23:15",
  "count": 8
}
```

## Technical Details

### Data Sources

1. **OpenSky Network** (`opensky-network.org`)
   - Provides real-time aircraft positions and callsigns
   - Anonymous public API with rate limits
   - Updates aircraft locations in real-time

2. **adsbdb** (`api.adsbdb.com`)
   - Aircraft metadata (registration, owner, manufacturer, type)
   - Flight route information (origin/destination airports)
   - Returns 404 for aircraft not in database

### Architecture

- **HTTP Client**: 10-second timeout for API requests
- **Concurrent Safe**: Uses mutex-protected global state for web data
- **Auto-refresh**: Web page refreshes every 60 seconds via meta tag
- **Background Updates**: Console checks run every 5 minutes via ticker

## Rate Limits & Reliability

- **OpenSky Network**: Anonymous requests are rate-limited. If you see empty results, wait 10-15 seconds between requests. For higher limits, create a free account and add authentication.
- **adsbdb**: May return 404 for aircraft not in their database (military, private, or newly registered aircraft)
- **Network Issues**: The application will log errors but continue running and retry on the next cycle

## Customization

### Adjust Coverage Area

Edit the bounding box in `main.go`:

```go
func fetchOpenSkyNorthLondon(ctx context.Context, client *http.Client) ([]AircraftState, error) {
    // Modify these coordinates:
    url := "https://opensky-network.org/api/states/all?lamin=51.50&lomin=-0.50&lamax=51.80&lomax=0.20"
    // ...
}
```

### Change Update Frequency

Modify the ticker interval in `main.go`:

```go
// Change from 5 minutes to your preferred interval:
ticker := time.NewTicker(5 * time.Minute)
```

### Change Web Server Port

Update the port in `main.go`:

```go
http.ListenAndServe(":4545", nil) // Change 4545 to your preferred port
```

## Future Enhancements

Potential improvements:
- Parallel enrichment with worker pool for faster data fetching
- In-memory caching to reduce API calls for recently seen aircraft
- Include altitude, speed, and heading from OpenSky data
- Export to CSV or other formats
- Historical tracking and flight path visualization
- WebSocket support for real-time updates without page refresh
- Database persistence for analytics

## Troubleshooting

**No aircraft showing:**
- Check internet connection
- Verify OpenSky Network is accessible: `https://opensky-network.org/api/states/all`
- Wait 10-15 seconds between requests if rate-limited
- Try during peak flight times (6am-11pm local time)

**Web server not starting:**
- Ensure port 4545 is not in use: `lsof -i :4545`
- Try a different port (see customization above)

**Missing flight route information:**
- Routes require valid callsigns from OpenSky
- Not all flights have route data in adsbdb
- Military and private flights often lack public route information

## License

Demo project for educational purposes. Respects API terms of service for OpenSky Network and adsbdb.
