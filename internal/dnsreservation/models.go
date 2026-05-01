package dnsreservation

// Reservation is the API's (hostname, MAC, IPv4) triple. Used for both PUT and
// GET response bodies.
type Reservation struct {
	Hostname string `json:"hostname"`
	MAC      string `json:"mac"`
	IPv4     string `json:"ipv4"`
}

type putRequest struct {
	MAC string `json:"mac"`
}

type errorEnvelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
