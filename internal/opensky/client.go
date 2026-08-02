package opensky

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://opensky-network.org/api"

// BBox is a WGS84 bounding box (decimal degrees).
type BBox struct {
	LaMin float64
	LoMin float64
	LaMax float64
	LoMax float64
}

// Wrocław metro + EPWR approaches (roughly).
var Wroclaw = BBox{
	LaMin: 50.90,
	LoMin: 16.70,
	LaMax: 51.30,
	LoMax: 17.40,
}

// Aircraft is a trimmed state vector for the UI.
type Aircraft struct {
	ICAO24    string  `json:"icao24"`
	Callsign  string  `json:"callsign"`
	Country   string  `json:"country"`
	Lon       float64 `json:"lon"`
	Lat       float64 `json:"lat"`
	AltitudeM float64 `json:"altitude_m"` // baro, meters; 0 if unknown
	Velocity  float64 `json:"velocity"`   // m/s
	Track     float64 `json:"track"`      // degrees
	Vertical  float64 `json:"vertical"`   // m/s climb rate
	OnGround  bool    `json:"on_ground"`
}

// Client talks to OpenSky /states/all.
type Client struct {
	HTTP    *http.Client
	BaseURL string
	// Optional basic auth (username/password) for higher limits.
	Username string
	Password string
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return c.BaseURL
	}
	return defaultBaseURL
}

type rawResponse struct {
	Time   int64           `json:"time"`
	States [][]json.RawMessage `json:"states"`
}

// FetchStates loads airborne (and optionally ground) traffic in bbox.
func (c *Client) FetchStates(bbox BBox) ([]Aircraft, time.Time, error) {
	u, err := url.Parse(c.base() + "/states/all")
	if err != nil {
		return nil, time.Time{}, err
	}
	q := u.Query()
	q.Set("lamin", fmt.Sprintf("%.4f", bbox.LaMin))
	q.Set("lomin", fmt.Sprintf("%.4f", bbox.LoMin))
	q.Set("lamax", fmt.Sprintf("%.4f", bbox.LaMax))
	q.Set("lomax", fmt.Sprintf("%.4f", bbox.LoMax))
	u.RawQuery = q.Encode()

	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("User-Agent", "wroclaw-sky")
	if c.Username != "" {
		req.SetBasicAuth(c.Username, c.Password)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("OpenSky API returned %s", resp.Status)
	}

	var raw rawResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, time.Time{}, err
	}

	out := make([]Aircraft, 0, len(raw.States))
	for _, row := range raw.States {
		ac, ok := parseState(row)
		if !ok {
			continue
		}
		out = append(out, ac)
	}
	ts := time.Unix(raw.Time, 0).UTC()
	if raw.Time == 0 {
		ts = time.Now().UTC()
	}
	return out, ts, nil
}

func parseState(row []json.RawMessage) (Aircraft, bool) {
	if len(row) < 12 {
		return Aircraft{}, false
	}
	var (
		icao, callsign, country string
		lon, lat                *float64
		baro, geo               *float64
		velocity, track, vert   *float64
		onGround                bool
	)
	_ = json.Unmarshal(row[0], &icao)
	_ = json.Unmarshal(row[1], &callsign)
	_ = json.Unmarshal(row[2], &country)
	_ = json.Unmarshal(row[5], &lon)
	_ = json.Unmarshal(row[6], &lat)
	_ = json.Unmarshal(row[7], &baro)
	_ = json.Unmarshal(row[8], &onGround)
	_ = json.Unmarshal(row[9], &velocity)
	_ = json.Unmarshal(row[10], &track)
	_ = json.Unmarshal(row[11], &vert)
	if len(row) > 13 {
		_ = json.Unmarshal(row[13], &geo)
	}
	if lon == nil || lat == nil {
		return Aircraft{}, false
	}

	alt := 0.0
	if baro != nil {
		alt = *baro
	} else if geo != nil {
		alt = *geo
	}

	ac := Aircraft{
		ICAO24:    strings.ToLower(strings.TrimSpace(icao)),
		Callsign:  strings.TrimSpace(callsign),
		Country:   country,
		Lon:       *lon,
		Lat:       *lat,
		AltitudeM: alt,
		OnGround:  onGround,
	}
	if velocity != nil {
		ac.Velocity = *velocity
	}
	if track != nil {
		ac.Track = *track
	}
	if vert != nil {
		ac.Vertical = *vert
	}
	if ac.Callsign == "" {
		ac.Callsign = ac.ICAO24
	}
	return ac, true
}

// AltFt returns altitude in feet.
func (a Aircraft) AltFt() int {
	return int(a.AltitudeM * 3.28084)
}

// SpeedKts returns ground speed in knots.
func (a Aircraft) SpeedKts() int {
	return int(a.Velocity * 1.94384)
}

// Format helpers kept for templates without methods on filtered slices.
func FormatAlt(m float64) string {
	if m <= 0 {
		return "—"
	}
	return strconv.Itoa(int(m*3.28084)) + " ft"
}

func FormatSpeed(ms float64) string {
	if ms <= 0 {
		return "—"
	}
	return strconv.Itoa(int(ms*1.94384)) + " kt"
}
