package zfsdataset

// Dataset is the realized dataset returned by the agent for both PUT and GET.
type Dataset struct {
	Dataset     string            `json:"dataset"`
	Pool        string            `json:"pool"`
	Name        string            `json:"name"`
	Quota       string            `json:"quota"`
	Recordsize  string            `json:"recordsize"`
	Compression string            `json:"compression"`
	Mountpoint  string            `json:"mountpoint"`
	Properties  map[string]string `json:"properties"`
	GUID        string            `json:"guid"`
	Used        int64             `json:"used"`
	Available   int64             `json:"available"`
	Mounted     bool              `json:"mounted"`
}

// Spec is the desired property set sent on PUT. Empty fields are omitted so the
// agent only applies the properties the resource manages.
type Spec struct {
	Quota       string            `json:"quota,omitempty"`
	Recordsize  string            `json:"recordsize,omitempty"`
	Compression string            `json:"compression,omitempty"`
	Mountpoint  string            `json:"mountpoint,omitempty"`
	Properties  map[string]string `json:"properties,omitempty"`
}

type errorEnvelope struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
