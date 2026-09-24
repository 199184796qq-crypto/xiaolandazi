export type SystemAgentRoutingDomain =
  | 'system'
  | 'live-room'
  | 'live-strategy'
  | 'live-policy-admin'
  | 'live-support'

export function shouldRouteToSystemAgent(
  domain: SystemAgentRoutingDomain,
  options: {
    hasSystemTask: boolean
    systemCapabilityIntent: boolean
    clientBoundaryIntent: boolean
  },
) {
  if (options.hasSystemTask || options.clientBoundaryIntent) return true

  // The live-policy workbench owns ordinary conversation while the user is
  // editing L1/L2. Otherwise generic capability matching can steal phrases
  // like "添加一条规则" and incorrectly fall back to the general admin agent.
  if (domain === 'live-policy-admin') return false

  if (domain === 'system') return true
  return options.systemCapabilityIntent
}
