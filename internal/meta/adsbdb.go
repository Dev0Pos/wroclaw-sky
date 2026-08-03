package meta

import (
	"encoding/json"
	"fmt"
	"strings"
)

const adsbdbBase = "https://api.adsbdb.com/v0"

type adsbdbCallsignResp struct {
	Response struct {
		Flightroute *adsbdbFlightroute `json:"flightroute"`
	} `json:"response"`
}

type adsbdbFlightroute struct {
	Callsign    string       `json:"callsign"`
	Airline     *adsbdbAirline `json:"airline"`
	Origin      *adsbdbAirport `json:"origin"`
	Destination *adsbdbAirport `json:"destination"`
}

type adsbdbAirline struct {
	Name string `json:"name"`
	ICAO string `json:"icao"`
}

type adsbdbAirport struct {
	ICAO         string `json:"icao_code"`
	IATA         string `json:"iata_code"`
	Name         string `json:"name"`
	Municipality string `json:"municipality"`
}

type adsbdbAircraftResp struct {
	Response struct {
		Aircraft *adsbdbAircraft `json:"aircraft"`
	} `json:"response"`
}

type adsbdbAircraft struct {
	Type           string `json:"type"`
	ICAOType       string `json:"icao_type"`
	Manufacturer   string `json:"manufacturer"`
	ModeS          string `json:"mode_s"`
	Registration   string `json:"registration"`
	RegisteredOwner string `json:"registered_owner"`
	URLPhoto       string `json:"url_photo_thumbnail"`
}

func (e *Enricher) enrichADSBdb(out Detail) Detail {
	var (
		ac    *adsbdbAircraft
		acErr error
		route *adsbdbFlightroute
		rtErr error
	)
	done := make(chan struct{}, 2)
	go func() {
		ac, acErr = e.fetchADSBdbAircraft(out.ICAO24)
		done <- struct{}{}
	}()
	go func() {
		cs := strings.TrimSpace(out.Callsign)
		if cs != "" && !strings.EqualFold(cs, out.ICAO24) {
			route, rtErr = e.fetchADSBdbRoute(cs)
		} else {
			rtErr = fmt.Errorf("no callsign")
		}
		done <- struct{}{}
	}()
	<-done
	<-done

	if acErr == nil && ac != nil {
		if out.Registration == "" {
			out.Registration = ac.Registration
		}
		if out.TypeCode == "" {
			out.TypeCode = ac.ICAOType
		}
		if out.TypeName == "" {
			out.TypeName = ac.Type
		}
		if out.Manufacturer == "" {
			out.Manufacturer = ac.Manufacturer
		}
		if out.Operator == "" {
			out.Operator = ac.RegisteredOwner
		}
		if out.PhotoURL == "" {
			out.PhotoURL = ac.URLPhoto
		}
	}
	if rtErr == nil && route != nil && route.Origin != nil && route.Destination != nil {
		origin := strings.ToUpper(route.Origin.ICAO)
		dest := strings.ToUpper(route.Destination.ICAO)
		out.Origin = origin
		out.Destination = dest
		out.Route = origin + "-" + dest
		out.RouteSource = "adsbdb"
		out.OriginName = route.Origin.Name
		out.DestName = route.Destination.Name
		out.OriginCity = route.Origin.Municipality
		out.DestCity = route.Destination.Municipality
		if out.Operator == "" && route.Airline != nil {
			out.Operator = route.Airline.Name
		}
	}
	return out
}

func (e *Enricher) fetchADSBdbAircraft(icao string) (*adsbdbAircraft, error) {
	icao = strings.ToUpper(strings.TrimSpace(icao))
	if icao == "" {
		return nil, fmt.Errorf("empty icao")
	}
	body, code, err := e.get(e.adsbdbBase() + "/aircraft/" + icao)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("adsbdb aircraft %d", code)
	}
	var resp adsbdbAircraftResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Response.Aircraft == nil {
		return nil, fmt.Errorf("aircraft not found")
	}
	return resp.Response.Aircraft, nil
}

func (e *Enricher) fetchADSBdbRoute(callsign string) (*adsbdbFlightroute, error) {
	callsign = strings.ToUpper(strings.TrimSpace(callsign))
	callsign = strings.ReplaceAll(callsign, " ", "")
	if callsign == "" {
		return nil, fmt.Errorf("empty callsign")
	}
	body, code, err := e.get(e.adsbdbBase() + "/callsign/" + callsign)
	if err != nil {
		return nil, err
	}
	if code != 200 {
		return nil, fmt.Errorf("adsbdb route %d", code)
	}
	var resp adsbdbCallsignResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Response.Flightroute == nil {
		return nil, fmt.Errorf("route not found")
	}
	return resp.Response.Flightroute, nil
}
