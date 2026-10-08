package messagecaptain

const (
	CutRequested        = "interaction.cut.requested"
	CutOffered          = "mainline.cut.offered"
	InteractionPrepared = "interaction.audio.prepared"
)

// The request contains scheduling intent, never the viewers to be greeted.
type CutRequest struct {
	RequestID string `json:"request_id"`
	Epoch     string `json:"epoch"`
	LeadMS    int    `json:"lead_ms"`
}
type CutOffer struct {
	RequestID string `json:"request_id"`
	Epoch     string `json:"epoch"`
	CutMS     int    `json:"cut_ms"`
	LeadMS    int    `json:"lead_ms"`
}
type Preparation struct {
	DecisionID string `json:"decision_id"`
	ElapsedMS  int    `json:"elapsed_ms"`
}
