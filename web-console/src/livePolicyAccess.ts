import type { Bootstrap } from './types'

function hasPermission(bootstrap: Bootstrap | null | undefined, permission: string): boolean {
  const access = bootstrap?.staff_access
  return bootstrap?.actor.role === 'platform_admin' ||
    access?.is_super_admin === true ||
    access?.permissions.includes('*') === true ||
    access?.permissions.includes(permission) === true
}

export function canManageLivePolicyL1(bootstrap: Bootstrap | null | undefined): boolean {
  return hasPermission(bootstrap, 'livepolicy.manage_l1')
}

export function canManageLivePolicyL2(bootstrap: Bootstrap | null | undefined): boolean {
  return canManageLivePolicyL1(bootstrap) || hasPermission(bootstrap, 'livepolicy.manage_l2')
}

// Customer ownership and the room-specific grant remain server-side checks.
// Staff-side eligibility requires both industry-layer management and the
// explicit customer-authorized user-layer support permission.
export function canDelegateLivePolicyL3(bootstrap: Bootstrap | null | undefined): boolean {
  const role = bootstrap?.actor.role
  return (role === 'staff' || role === 'sales_staff') &&
    canManageLivePolicyL2(bootstrap) &&
    hasPermission(bootstrap, 'livepolicy.manage_l3_authorized')
}
