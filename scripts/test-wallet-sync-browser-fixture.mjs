// Local-only browser fixture. No upstream network or real financial operation.
import { createServer } from 'node:http';
import { readFile, stat } from 'node:fs/promises';
import { resolve, extname, sep } from 'node:path';

for (const [surface, port, relative] of [['desktop', 9318, 'web-console/dist'], ['mobile', 9319, 'customer-mobile/build']]) {
  const root = resolve(relative);
  let created = 0, acceptedAt = 0, financeFailures = 0, financeReads = 0, refundReads = 0;
  const record = () => {
    const done = Date.now() - acceptedAt >= 4000;
    return { id: 2, refund_no: 'LOCAL-FIXTURE-ONLY', status: done ? 'success' : 'processing', amount_cents: 1000,
      refunded_cents: done ? 1000 : 0, frozen_cents: done ? 0 : 1000, released_cents: 0, created_at: new Date().toISOString(), items: [] };
  };
  const json = (res, body, status = 200) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store' }); res.end(JSON.stringify(body)); };
  createServer(async (req, res) => {
    const url = new URL(req.url, `http://127.0.0.1:${port}`);
    const p = url.pathname;
    if (p === '/_test/state') return json(res, { surface, created, financeFailures, financeReads, refundReads, accepted: created > 0, record: created ? record() : null });
    if (p.startsWith('/api/')) {
      if (p === '/api/v1/bootstrap') return json(res, { actor: { user_id: 90001, role: 'customer', tenant_id: 90001, username: 'local-fixture', display_name: '本地测试', phone: '13800000000', province: '四川省', city: '成都市', district: '武侯区', must_change_password: false }, tenants: [], environment: 'local-fixture' });
      if (p === '/api/v1/wallet/wechat-refunds' && req.method === 'POST') {
        created++; acceptedAt = Date.now(); return json(res, record(), 202);
      }
      if (p === '/api/v1/wallet/wechat-refunds') {
        refundReads++;
        const r = created ? record() : null;
        return json(res, { available_cents: created ? 1600 : 2600, frozen_cents: r?.frozen_cents || 0, refundable_cents: created ? 1600 : 2600, records: r ? [r] : [] });
      }
      if (p === '/api/v1/wallet/wechat-refunds/2/query') return json(res, record());
      if (p === '/api/v1/finance/dashboard') {
        financeReads++;
        // Exhaust the first read's three retries after acceptance; the next
        // automatic poll recovers, without the user reloading or submitting.
        if (created && financeFailures++ < 3) return json(res, { error: '读取财务信息失败' }, 500);
        return json(res, { cash_balance_cents: created ? 1600 : 2600, reward_balance_cents: 0, commission_balance_cents: 0,
          commission_frozen_cents: 0, bean_balance: 0, bean_frozen: 0, total_balance_cents: created ? 1600 : 2600,
          month_spent_cents: 0, available_seconds: 0, ledger: [], recharges: [], payments: [], purchases: [], refunds: [] });
      }
      if (/beans\/wallet|bean-wallet|finance\/beans$/.test(p)) return json(res, { wallet: { available_beans: 0, frozen_beans: 0 }, settings: { enabled: true, purchase_beans_per_yuan: 100, minimum_purchase_cents: 1 }, ledger: [] });
      if (/referral.*wallet/.test(p)) return json(res, { wallet: {}, ledger: [], withdrawals: [] });
      if (/withdrawals/.test(p)) return json(res, { items: [] });
      if (/public-config/.test(p)) return json(res, { site_name: '本地测试', client_agent_name: '小蓝', internal_agent_name: '小蓝' });
      if (/payments\/wechat\/status/.test(p)) return json(res, { enabled: true });
      if (/ui-preferences/.test(p)) return json(res, { sidebar_collapsed: true });
      if (/rooms$/.test(p)) return json(res, { items: [] });
      if (/runtime.*status/.test(p)) return json(res, { phase: 'online', status: 'online' });
      return json(res, { items: [], withdrawals: [], ledger: [], settings: { enabled: true }, wallet: { available_beans: 0, frozen_beans: 0 } }, req.method === 'GET' ? 200 : 405);
    }
    try {
      let file = resolve(root, '.' + decodeURIComponent(p));
      if (!file.startsWith(root + sep) && file !== root) return json(res, {}, 403);
      try { if (!(await stat(file)).isFile()) file = resolve(root, 'index.html'); } catch { file = resolve(root, 'index.html'); }
      const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.png': 'image/png', '.svg': 'image/svg+xml', '.json': 'application/json' }[extname(file)] || 'application/octet-stream';
      res.writeHead(200, { 'Content-Type': mime, 'Cache-Control': 'no-store' }); res.end(await readFile(file));
    } catch { res.writeHead(500); res.end('fixture failure'); }
  }).listen(port, '127.0.0.1', () => console.log(`LOCAL_FIXTURE ${surface} http://customer.localhost:${port}`));
}
