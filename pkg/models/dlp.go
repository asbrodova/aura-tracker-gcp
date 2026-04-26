package models

// DLPFinding is one matched sensitive value detected by the GCP DLP API.
type DLPFinding struct {
	InfoType string `json:"info_type"`
	// Offset is the byte offset of the match in the original string.
	Offset int `json:"offset"`
	Length int `json:"length"`
	// Quote is the matched substring, populated only in audit/inspect mode.
	Quote string `json:"quote,omitempty"`
}
