// Terminal customers use order/support progress pages, not a staff work inbox.
// Preserve existing non-customer profiles; business permissions remain separate.
export function canUseWorkInbox(role?:string):boolean {
 return ['platform_admin','staff','sales_staff','agent_admin'].includes(role || '')
}

// Only explicit operational permissions can cross a department's default navigation.
// This is a UI gate; each target still enforces its existing server-side authorization.
export function canOpenWorkInboxRoute(path:string,role:string,has:(permission:string)=>boolean):boolean {
 if(role!=='staff'&&role!=='sales_staff')return false
 if(path==='/staff/after-sales')return has('inventory.after_sales.view')&&has('inventory.after_sales.manage')
 if(path==='/resources/inventory')return has('inventory.view')&&has('inventory.manage')
 if(path==='/resources/logistics')return has('logistics.view')&&has('logistics.manage')
 if(path==='/operations/support')return role!=='sales_staff'&&(has('liveops.configure')||has('liveops.ticket.manage'))
 return false
}
