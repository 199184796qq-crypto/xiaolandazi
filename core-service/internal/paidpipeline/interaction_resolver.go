package paidpipeline

import "net/http"

// HTTPPurposeResolver only speaks the current one-to-ten-person batch protocol.
type HTTPPurposeResolver struct {
	URL, Token string
	Client     *http.Client
}

func (p *Processor) SetPurposeResolver(resolver HTTPPurposeResolver) { p.setInteractionV2(resolver) }
