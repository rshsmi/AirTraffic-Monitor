# flight-over-my-hours

Demo Go program enriching Mode S aircraft codes using adsbdb public API.

## Prerequisites
- Go 1.22+ installed (ensure `go version` works). On macOS you can install via Homebrew: `brew install go`.
- A list of Mode S (hex) codes currently observed over London (from an ADS-B receiver or third-party feed).

## Usage
The program now automatically fetches live aircraft over a London bounding box using the OpenSky public states API, then enriches each Mode S (icao24) code via adsbdb.

Run:

```bash
cd flight-over-my-hours
go run .
```

Sample output:

```text
Found 12 aircraft over London (OpenSky). Enriching via adsbdb...
Mode S: 4008F6  Reg: G-VROS  Type: 747-443  ICAO Type: B744  Owner: CELESTIAL AVIATION TRADING 14 LTD  Manufacturer: Boeing Company
...
```

## Bounding box

Current box: lat 51.30–51.70, lon -0.50–0.30 (covers Heathrow / City vicinity). Adjust in `fetchOpenSkyLondon` if needed.

## Rate limits & reliability

OpenSky anonymous requests are rate limited; if you see empty results, wait a few seconds or create an account for authenticated calls. adsbdb enrich may return 404 for aircraft not in its database.

## Extending

Potential improvements:

1. Parallel enrichment with a worker pool.
2. Caching results (e.g. in-memory map keyed by Mode S).
3. Including altitude / speed directly from OpenSky output alongside metadata.
4. Export JSON or serve a small HTTP endpoint.

## Notes

adsbdb provides metadata (aircraft details, flight routes) but not real-time positional data. OpenSky supplies positions. Together they give both "where" and "what".
