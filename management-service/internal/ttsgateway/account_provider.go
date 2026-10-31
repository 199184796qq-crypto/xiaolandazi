package ttsgateway

// Account injection belongs to each provider. Scheduling never constructs a
// vendor transport or substitutes a model; clients/endpoints remain intact.
type accountProvider interface{ withAccountKey(string) Provider }

func (p *qwenProvider) withAccountKey(key string) Provider {
	return NewQwenProvider(QwenConfig{APIKey: key, TTSBaseURL: p.ttsBaseURL, CustomizationURL: p.customizationURL, Client: p.client})
}
func (p *cosyVoiceProvider) withAccountKey(key string) Provider {
	q := p.transport
	return NewCosyVoiceProvider(QwenConfig{APIKey: key, TTSBaseURL: q.ttsBaseURL, CustomizationURL: q.customizationURL, Client: q.client})
}
