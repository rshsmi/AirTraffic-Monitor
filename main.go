package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// NOTE: The adsbdb public API does not (currently) expose a direct "live aircraft by geo bounding box" endpoint.
// It focuses on aircraft metadata, flight routes and conversions for identifiers. To approximate "planes over London"
// with JUST this API we would need callsigns or mode-s codes sourced elsewhere (e.g. a feed like ADS-B Exchange,
// OpenSky Network, etc.) and then enrich with adsbdb. For now, this program demonstrates querying a set of example
// Mode S hex codes (you can replace with real-time hex codes captured from a receiver) and printing aircraft details.
//
// If in future adsbdb provides a geographic query endpoint, you can adapt by hitting that endpoint instead.

// AircraftResponse models a successful aircraft lookup response from adsbdb
type AircraftResponse struct {
	Response struct {
		Aircraft struct {
			Type                          string `json:"type"`
			ICAOType                      string `json:"icao_type"`
			Manufacturer                  string `json:"manufacturer"`
			ModeS                        string `json:"mode_s"`
			Registration                  string `json:"registration"`
			RegisteredOwnerCountryISOName string `json:"registered_owner_country_iso_name"`
			RegisteredOwnerCountryName    string `json:"registered_owner_country_name"`
			RegisteredOwnerOperatorFlag   *string `json:"registered_owner_operator_flag_code"`
			RegisteredOwner               string  `json:"registered_owner"`
			URLPhoto                      *string `json:"url_photo"`
			URLPhotoThumbnail             *string `json:"url_photo_thumbnail"`
		} `json:"aircraft"`
	} `json:"response"`
}

// UnknownResponse is returned for 404 cases
type UnknownResponse struct {
	Response string `json:"response"`
}

// FlightRouteResponse models a callsign lookup response from adsbdb
type FlightRouteResponse struct {
	Response struct {
		FlightRoute struct {
			Callsign     string `json:"callsign"`
			CallsignICAO *string `json:"callsign_icao"`
			CallsignIATA *string `json:"callsign_iata"`
			Origin struct {
				CountryISOName string  `json:"country_iso_name"`
				CountryName    string  `json:"country_name"`
				Elevation      float64 `json:"elevation"`
				IATACode       string  `json:"iata_code"`
				ICAOCode       string  `json:"icao_code"`
				Latitude       float64 `json:"latitude"`
				Longitude      float64 `json:"longitude"`
				Municipality   string  `json:"municipality"`
				Name           string  `json:"name"`
			} `json:"origin"`
			Destination struct {
				CountryISOName string  `json:"country_iso_name"`
				CountryName    string  `json:"country_name"`
				Elevation      float64 `json:"elevation"`
				IATACode       string  `json:"iata_code"`
				ICAOCode       string  `json:"icao_code"`
				Latitude       float64 `json:"latitude"`
				Longitude      float64 `json:"longitude"`
				Municipality   string  `json:"municipality"`
				Name           string  `json:"name"`
			} `json:"destination"`
		} `json:"flightroute"`
	} `json:"response"`
}

// CombinedFlightInfo holds both aircraft and route data
type CombinedFlightInfo struct {
	Aircraft    *AircraftResponse
	FlightRoute *FlightRouteResponse
}

// WebAircraftInfo holds display-ready aircraft information
type WebAircraftInfo struct {
	Registration string
	Owner        string
	Manufacturer string
	Type         string
	Origin       string
	Destination  string
	LastUpdated  string
}

// Global state for web server
var (
	currentAircraft []WebAircraftInfo
	lastUpdate      string
	aircraftMutex   sync.RWMutex
)

// fetchAircraft queries adsbdb for a single Mode S or registration string.
func fetchAircraft(ctx context.Context, client *http.Client, id string) (*AircraftResponse, error) {
	// Using major version v0 from current release examples.
	url := fmt.Sprintf("https://api.adsbdb.com/v0/aircraft/%s", id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		var unknown UnknownResponse
		_ = json.NewDecoder(res.Body).Decode(&unknown) // best-effort
		return nil, fmt.Errorf("unknown aircraft (%s): %s", id, unknown.Response)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for %s", res.StatusCode, id)
	}
	var aircraft AircraftResponse
	if err := json.NewDecoder(res.Body).Decode(&aircraft); err != nil {
		return nil, err
	}
	// Basic validation
	if aircraft.Response.Aircraft.ModeS == "" && aircraft.Response.Aircraft.Registration == "" {
		return nil, errors.New("empty aircraft payload")
	}
	return &aircraft, nil
}

// fetchFlightRoute queries adsbdb for flight route info using aircraft Mode S + callsign
func fetchFlightRoute(ctx context.Context, client *http.Client, modeS, callsign string) (*FlightRouteResponse, error) {
	if callsign == "" {
		return nil, fmt.Errorf("no callsign available for route lookup")
	}
	
	// Try aircraft endpoint with callsign query parameter first
	url := fmt.Sprintf("https://api.adsbdb.com/v0/aircraft/%s?callsign=%s", modeS, callsign)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("flight route not found for callsign %s", callsign)
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d for callsign %s", res.StatusCode, callsign)
	}

	// This endpoint returns both aircraft and flightroute data
	var combined struct {
		Response struct {
			Aircraft    AircraftResponse `json:"aircraft"`
			FlightRoute struct {
				Callsign     string `json:"callsign"`
				CallsignICAO *string `json:"callsign_icao"`
				CallsignIATA *string `json:"callsign_iata"`
				Origin struct {
					CountryISOName string  `json:"country_iso_name"`
					CountryName    string  `json:"country_name"`
					Elevation      float64 `json:"elevation"`
					IATACode       string  `json:"iata_code"`
					ICAOCode       string  `json:"icao_code"`
					Latitude       float64 `json:"latitude"`
					Longitude      float64 `json:"longitude"`
					Municipality   string  `json:"municipality"`
					Name           string  `json:"name"`
				} `json:"origin"`
				Destination struct {
					CountryISOName string  `json:"country_iso_name"`
					CountryName    string  `json:"country_name"`
					Elevation      float64 `json:"elevation"`
					IATACode       string  `json:"iata_code"`
					ICAOCode       string  `json:"icao_code"`
					Latitude       float64 `json:"latitude"`
					Longitude      float64 `json:"longitude"`
					Municipality   string  `json:"municipality"`
					Name           string  `json:"name"`
				} `json:"destination"`
			} `json:"flightroute"`
		} `json:"response"`
	}
	
	if err := json.NewDecoder(res.Body).Decode(&combined); err != nil {
		return nil, err
	}
	
	return &FlightRouteResponse{Response: struct {
		FlightRoute struct {
			Callsign     string `json:"callsign"`
			CallsignICAO *string `json:"callsign_icao"`
			CallsignIATA *string `json:"callsign_iata"`
			Origin struct {
				CountryISOName string  `json:"country_iso_name"`
				CountryName    string  `json:"country_name"`
				Elevation      float64 `json:"elevation"`
				IATACode       string  `json:"iata_code"`
				ICAOCode       string  `json:"icao_code"`
				Latitude       float64 `json:"latitude"`
				Longitude      float64 `json:"longitude"`
				Municipality   string  `json:"municipality"`
				Name           string  `json:"name"`
			} `json:"origin"`
			Destination struct {
				CountryISOName string  `json:"country_iso_name"`
				CountryName    string  `json:"country_name"`
				Elevation      float64 `json:"elevation"`
				IATACode       string  `json:"iata_code"`
				ICAOCode       string  `json:"icao_code"`
				Latitude       float64 `json:"latitude"`
				Longitude      float64 `json:"longitude"`
				Municipality   string  `json:"municipality"`
				Name           string  `json:"name"`
			} `json:"destination"`
		} `json:"flightroute"`
	}{FlightRoute: combined.Response.FlightRoute}}, nil
}

// OpenSky states endpoint shape we'll use (public, anonymous) for a bounding box.
// Reference: https://opensky-network.org/apidoc/rest.html#flights-in-a-bounding-box
// We will call: https://opensky-network.org/api/states/all?lamin=51.30&lomin=-0.50&lamax=51.70&lomax=0.30
// This returns JSON with field "states": [[icao24, callsign, origin_country, time_position, last_contact, longitude, latitude, baro_altitude, on_ground, velocity, true_track, vertical_rate, sensors, geo_altitude, squawk, spi, position_source, category]]

type openSkyStates struct {
	Time   int64           `json:"time"`
	States [][]interface{} `json:"states"`
}

// AircraftState holds both ICAO24 and callsign from OpenSky
type AircraftState struct {
	ICAO24   string
	Callsign string
}

// extractAircraftStates parses states array pulling both icao24 (index 0) and callsign (index 1) when present.
func extractAircraftStates(data *openSkyStates) []AircraftState {
	if data == nil || len(data.States) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	var states []AircraftState
	
	for _, row := range data.States {
		if len(row) < 2 {
			continue
		}
		hex, _ := row[0].(string)
		if hex == "" {
			continue
		}
		// OpenSky returns lowercase; adsbdb expects uppercase for Mode S. Convert.
		hex = strings.ToUpper(hex)
		
		// Avoid duplicates
		if _, exists := seen[hex]; exists {
			continue
		}
		seen[hex] = struct{}{}
		
		callsign, _ := row[1].(string)
		callsign = strings.TrimSpace(callsign)
		
		states = append(states, AircraftState{
			ICAO24:   hex,
			Callsign: callsign,
		})
	}
	
	// Sort by ICAO24 for consistent output
	sort.Slice(states, func(i, j int) bool {
		return states[i].ICAO24 < states[j].ICAO24
	})
	
	return states
}

func fetchOpenSkyNorthLondon(ctx context.Context, client *http.Client) ([]AircraftState, error) {
	// Much larger North London area: lat 51.50-51.80, lon -0.50 to 0.20 (covers all of North London and beyond)
	url := "https://opensky-network.org/api/states/all?lamin=51.50&lomin=-0.50&lamax=51.80&lomax=0.20"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("opensky unexpected status %d", res.StatusCode)
	}
	var payload openSkyStates
	if err := json.NewDecoder(res.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return extractAircraftStates(&payload), nil
}

func checkAircraftInArea(ctx context.Context, client *http.Client) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Printf("\n=== Aircraft Check at %s ===\n", timestamp)
	
	// Step 1: Get live aircraft with both ICAO24 and callsigns over North London area via OpenSky.
	aircraftStates, err := fetchOpenSkyNorthLondon(ctx, client)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch OpenSky states: %v\n", err)
		updateWebData([]WebAircraftInfo{}, timestamp+" (Error fetching data)")
		return
	}
	if len(aircraftStates) == 0 {
		fmt.Println("No aircraft currently reported over North London area - OpenSky.")
		updateWebData([]WebAircraftInfo{}, timestamp)
		return
	}

	fmt.Printf("Found %d aircraft over North London area. Enriching via adsbdb...\n\n", len(aircraftStates))

	var webAircraftList []WebAircraftInfo

	// Step 2: Enrich each aircraft using adsbdb for both aircraft info and route info.
	for _, state := range aircraftStates {
		aircraft, aErr := fetchAircraft(ctx, client, state.ICAO24)
		if aErr != nil {
			fmt.Fprintf(os.Stderr, "%s -> adsbdb aircraft error: %v\n", state.ICAO24, aErr)
			continue
		}

		a := aircraft.Response.Aircraft
		
		// Try to get route information if we have a callsign
		var origin, destination string = "Unknown", "Unknown"
		if state.Callsign != "" {
			route, rErr := fetchFlightRoute(ctx, client, state.ICAO24, state.Callsign)
			if rErr == nil && route != nil {
				r := route.Response.FlightRoute
				origin = fmt.Sprintf("%s (%s)", r.Origin.Name, r.Origin.ICAOCode)
				destination = fmt.Sprintf("%s (%s)", r.Destination.Name, r.Destination.ICAOCode)
			}
		}

		// Output in requested format: Reg, Owner, Manufacturer, Type, Origin, Destination
		fmt.Printf("Reg: %s | Owner: %s | Manufacturer: %s | Type: %s | Origin: %s | Destination: %s\n",
			a.Registration, a.RegisteredOwner, a.Manufacturer, a.Type, origin, destination)

		// Add to web data
		webAircraftList = append(webAircraftList, WebAircraftInfo{
			Registration: a.Registration,
			Owner:        a.RegisteredOwner,
			Manufacturer: a.Manufacturer,
			Type:         a.Type,
			Origin:       origin,
			Destination:  destination,
			LastUpdated:  timestamp,
		})
	}

	// Update web data
	updateWebData(webAircraftList, timestamp)

	fmt.Println("\nData sources: OpenSky Network (live positions) + adsbdb (aircraft metadata + routes).")
}

// HTML template for the web page
const htmlTemplate = `
<!DOCTYPE html>
<html>
<head>
    <title>Aircraft Over North London</title>
    <meta http-equiv="refresh" content="60">
    <style>
        body { 
            font-family: 'Courier New', monospace; 
            margin: 0; 
            padding: 20px;
            background-color: #000000;
            color: #FFFF00;
        }
        h1 { 
            color: #FFFF00; 
            text-align: center;
            font-size: 2.5em;
            margin-bottom: 10px;
            text-transform: uppercase;
            letter-spacing: 2px;
        }
        .header { 
            background-color: #1a1a1a; 
            padding: 15px; 
            border: 2px solid #FFFF00;
            margin-bottom: 20px;
            text-align: center;
        }
        table { 
            border-collapse: collapse; 
            width: 100%; 
            margin-top: 20px;
            background-color: #000000;
            border: 2px solid #FFFF00;
        }
        th, td { 
            border: 1px solid #FFFF00; 
            padding: 15px; 
            text-align: left;
            font-family: 'Courier New', monospace;
            font-size: 0.9em;
        }
        th { 
            background-color: #FFFF00; 
            color: #000000;
            font-weight: bold;
            text-transform: uppercase;
            letter-spacing: 1px;
        }
        td {
            background-color: #000000;
            color: #FFFFFF;
        }
        tr:nth-child(even) td { 
            background-color: #1a1a1a;
        }
        tr:hover td {
            background-color: #333333;
        }
        .no-aircraft { 
            color: #FFFF00; 
            font-style: italic; 
            text-align: center; 
            padding: 40px;
            font-size: 1.2em;
            background-color: #1a1a1a;
            border: 2px solid #FFFF00;
            margin: 20px 0;
        }
        .update-time { 
            color: #FFFF00; 
            font-size: 1em;
            margin: 5px 0;
        }
        .footer {
            margin-top: 30px; 
            padding-top: 20px; 
            border-top: 2px solid #FFFF00; 
            color: #FFFF00; 
            font-size: 0.9em;
            text-align: center;
        }
        
        /* Flip animation for airport board effect */
        @keyframes flipIn {
            0% {
                transform: rotateX(-90deg);
                opacity: 0;
            }
            50% {
                transform: rotateX(-45deg);
                opacity: 0.5;
            }
            100% {
                transform: rotateX(0deg);
                opacity: 1;
            }
        }
        
        @keyframes flipOut {
            0% {
                transform: rotateX(0deg);
                opacity: 1;
            }
            50% {
                transform: rotateX(45deg);
                opacity: 0.5;
            }
            100% {
                transform: rotateX(90deg);
                opacity: 0;
            }
        }
        
        .flip-char {
            display: inline-block;
            animation: flipIn 0.8s ease-in-out;
            transform-origin: center;
        }
        
        .flip-update {
            animation: flipOut 0.4s ease-in-out, flipIn 0.4s ease-in-out 0.4s;
        }
        
        /* Stagger animation delays for wave effect */
        .flip-char:nth-child(1) { animation-delay: 0.1s; }
        .flip-char:nth-child(2) { animation-delay: 0.2s; }
        .flip-char:nth-child(3) { animation-delay: 0.3s; }
        .flip-char:nth-child(4) { animation-delay: 0.4s; }
        .flip-char:nth-child(5) { animation-delay: 0.5s; }
        .flip-char:nth-child(6) { animation-delay: 0.6s; }
        .flip-char:nth-child(7) { animation-delay: 0.7s; }
        .flip-char:nth-child(8) { animation-delay: 0.8s; }
        .flip-char:nth-child(9) { animation-delay: 0.9s; }
        .flip-char:nth-child(10) { animation-delay: 1.0s; }
        .flip-char:nth-child(11) { animation-delay: 1.1s; }
        .flip-char:nth-child(12) { animation-delay: 1.2s; }
        .flip-char:nth-child(13) { animation-delay: 1.3s; }
        .flip-char:nth-child(14) { animation-delay: 1.4s; }
        .flip-char:nth-child(15) { animation-delay: 1.5s; }
        
        /* Table rows animate in sequence */
        tbody tr {
            animation: flipIn 1.2s ease-in-out;
        }
        
        tbody tr:nth-child(1) { animation-delay: 0.2s; }
        tbody tr:nth-child(2) { animation-delay: 0.4s; }
        tbody tr:nth-child(3) { animation-delay: 0.6s; }
        tbody tr:nth-child(4) { animation-delay: 0.8s; }
        tbody tr:nth-child(5) { animation-delay: 1.0s; }
        tbody tr:nth-child(6) { animation-delay: 1.2s; }
        tbody tr:nth-child(7) { animation-delay: 1.4s; }
        tbody tr:nth-child(8) { animation-delay: 1.6s; }
        
        /* Header flip animation */
        h1 .flip-char {
            animation-duration: 1.5s;
            animation-delay: calc(0.1s * var(--char-index));
        }
    </style>
</head>
<body>
    <h1>✈ DEPARTURES - NORTH LONDON ✈</h1>
    
    <div class="header">
        <p><strong>Coverage Area:</strong> North London (Lat: 51.50-51.80, Lon: -0.50 to 0.20)</p>
        <p class="update-time"><strong>Last Updated:</strong> {{.LastUpdate}}</p>
        <p class="update-time"><strong>Total Aircraft:</strong> {{len .Aircraft}}</p>
        <p><em>Page auto-refreshes every 60 seconds</em></p>
    </div>

    {{if .Aircraft}}
    <table>
        <thead>
            <tr>
                <th>Registration</th>
                <th>Owner</th>
                <th>Manufacturer</th>
                <th>Aircraft Type</th>
                <th>Origin</th>
                <th>Destination</th>
            </tr>
        </thead>
        <tbody>
            {{range .Aircraft}}
            <tr>
                <td>{{.Registration}}</td>
                <td>{{.Owner}}</td>
                <td>{{.Manufacturer}}</td>
                <td>{{.Type}}</td>
                <td>{{.Origin}}</td>
                <td>{{.Destination}}</td>
            </tr>
            {{end}}
        </tbody>
    </table>
    {{else}}
    <div class="no-aircraft">
        <p>No aircraft currently detected over North London area.</p>
        <p>Data will refresh automatically every 5 minutes.</p>
    </div>
    {{end}}

    <div class="footer">
        <p><strong>DATA SOURCES</strong></p>
        <p>LIVE POSITIONS: OPENSKY NETWORK | AIRCRAFT DATA: ADSBDB.COM</p>
        <p>MONITORING AREA: 51.50°N-51.80°N, 0.50°W-0.20°E</p>
    </div>

    <script>
        function wrapTextInFlipChars(element) {
            const text = element.textContent;
            element.innerHTML = '';
            
            for (let i = 0; i < text.length; i++) {
                const char = text[i];
                const span = document.createElement('span');
                span.className = 'flip-char';
                span.style.setProperty('--char-index', i);
                span.textContent = char === ' ' ? '\u00A0' : char; // Non-breaking space
                element.appendChild(span);
            }
        }
        
        function animateTableUpdate() {
            const rows = document.querySelectorAll('tbody tr');
            rows.forEach((row, index) => {
                row.style.animation = 'none';
                row.offsetHeight; // Trigger reflow
                row.style.animation = 'flipIn 1.2s ease-in-out ' + (index * 0.2) + 's';
            });
        }
        
        // Initialize flip animations when page loads
        window.addEventListener('load', function() {
            // Animate the main title
            const title = document.querySelector('h1');
            if (title) {
                wrapTextInFlipChars(title);
            }
            
            // Animate table headers
            const headers = document.querySelectorAll('th');
            headers.forEach(header => {
                wrapTextInFlipChars(header);
            });
            
            // Set up periodic flip animation for visual effect
            setInterval(() => {
                const randomRowNum = Math.floor(Math.random() * 5) + 1;
                const randomRow = document.querySelector('tbody tr:nth-child(' + randomRowNum + ')');
                if (randomRow) {
                    const cells = randomRow.querySelectorAll('td');
                    cells.forEach(cell => {
                        cell.style.animation = 'flipUpdate 0.8s ease-in-out';
                        setTimeout(() => {
                            cell.style.animation = '';
                        }, 800);
                    });
                }
            }, 8000); // Random flip every 8 seconds
            
            // Add subtle continuous flip to title
            setInterval(() => {
                const titleChars = document.querySelectorAll('h1 .flip-char');
                titleChars.forEach((char, index) => {
                    setTimeout(() => {
                        char.style.animation = 'flipIn 0.6s ease-in-out';
                        setTimeout(() => {
                            char.style.animation = '';
                        }, 600);
                    }, index * 100);
                });
            }, 15000); // Title flip every 15 seconds
        });
        
        // Re-animate when page refreshes with new data
        let lastUpdateTime = '{{.LastUpdate}}';
        setInterval(() => {
            // This would normally check for updates via AJAX, 
            // but since we're using meta refresh, we'll just add visual flair
            const updateTimeElement = document.querySelector('.update-time');
            if (updateTimeElement) {
                updateTimeElement.style.animation = 'flipUpdate 0.8s ease-in-out';
                setTimeout(() => {
                    updateTimeElement.style.animation = '';
                }, 800);
            }
        }, 60000); // Visual update every minute
    </script>
</body>
</html>
`

// Web handler for the main page
func aircraftHandler(w http.ResponseWriter, r *http.Request) {
    aircraftMutex.RLock()
    data := struct {
        Aircraft   []WebAircraftInfo
        LastUpdate string
    }{
        Aircraft:   currentAircraft,
        LastUpdate: lastUpdate,
    }
    aircraftMutex.RUnlock()

    tmpl, err := template.New("aircraft").Parse(htmlTemplate)
    if err != nil {
        http.Error(w, "Template error", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "text/html")
    if err := tmpl.Execute(w, data); err != nil {
        http.Error(w, "Template execution error", http.StatusInternalServerError)
    }
}

// JSON API endpoint
func apiHandler(w http.ResponseWriter, r *http.Request) {
    aircraftMutex.RLock()
    data := struct {
        Aircraft   []WebAircraftInfo `json:"aircraft"`
        LastUpdate string            `json:"last_update"`
        Count      int               `json:"count"`
    }{
        Aircraft:   currentAircraft,
        LastUpdate: lastUpdate,
        Count:      len(currentAircraft),
    }
    aircraftMutex.RUnlock()

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(data)
}

// updateWebData updates the global aircraft data for the web server
func updateWebData(aircraftList []WebAircraftInfo, updateTime string) {
    aircraftMutex.Lock()
    currentAircraft = aircraftList
    lastUpdate = updateTime
    aircraftMutex.Unlock()
}

func main() {
	timeout := 10 * time.Second
	client := &http.Client{Timeout: timeout}
	ctx := context.Background()

	// Set up web server
	http.HandleFunc("/", aircraftHandler)
	http.HandleFunc("/api", apiHandler)
	
	// Start web server in a goroutine
	go func() {
		log.Printf("Starting web server on http://localhost:4545")
		log.Printf("Visit http://localhost:4545 to view aircraft data")
		log.Printf("API endpoint available at http://localhost:4545/api")
		if err := http.ListenAndServe(":4545", nil); err != nil {
			log.Fatal("Web server failed to start:", err)
		}
	}()

	fmt.Println("Starting aircraft monitoring over North London area...")
	fmt.Println("Checking every 5 minutes. Press Ctrl+C to stop.")
	fmt.Println("Web server running on http://localhost:4545")

	// Run initial check
	checkAircraftInArea(ctx, client)

	// Set up ticker for 5-minute intervals
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	// Run the check every 5 minutes
	for range ticker.C {
		checkAircraftInArea(ctx, client)
	}
}

