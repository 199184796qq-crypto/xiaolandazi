package model

import "context"

const MainlineGenerationCacheReuseSetting = "mainline_generation_cache_reuse_enabled"
const InteractionGenerationCacheReuseSetting = "interaction_generation_cache_reuse_enabled"

// Controls reuse of finished text/audio, not transport buffers or retry identity.
type GenerationCachePolicyReader interface {
	GenerationCacheReuseEnabled(context.Context, string) (bool, error)
}
