package backupcredential

// Credential is the API's credential record: the URL-key scope, server-minted
// bearer token, retention count, and credential kind. Used for both PUT and
// GET response bodies.
type Credential struct {
	Scope     string `json:"scope"`
	Token     string `json:"token"`
	Retention int    `json:"retention"`
	Kind      string `json:"kind"`
}

type putRequest struct {
	Retention int    `json:"retention"`
	Kind      string `json:"kind"`
}

type errorEnvelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
