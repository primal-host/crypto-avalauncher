package server

const dashboardHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Avalauncher</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    background: #0f1117;
    color: #e4e4e7;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
    align-items: center;
  }
  header {
    width: 100%;
    padding: 1.5rem 2rem;
    background: #16181d;
    border-bottom: 1px solid #27272a;
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  header h1 { font-size: 1.25rem; font-weight: 600; }
  .header-right { display: flex; align-items: center; gap: 1rem; }
  .header-right .version { color: #71717a; font-size: 0.875rem; }
  .auth-status { font-size: 0.75rem; padding: 0.25rem 0.5rem; border-radius: 0.25rem; }
  .auth-status.ok { background: #14532d; color: #4ade80; }
  .auth-status.no { background: #451a03; color: #fb923c; }
  main {
    width: 100%;
    max-width: 72rem;
    padding: 2rem;
    flex: 1;
  }
  .cards {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(14rem, 1fr));
    gap: 1rem;
    margin-bottom: 2rem;
  }
  .card {
    background: #16181d;
    border: 1px solid #27272a;
    border-radius: 0.5rem;
    padding: 1.25rem;
  }
  .card h2 { font-size: 0.875rem; color: #71717a; margin-bottom: 0.5rem; }
  .card .value { font-size: 2rem; font-weight: 700; }
  .section { margin-bottom: 2rem; }
  .section-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 1rem;
  }
  .section-header h2 { font-size: 1.125rem; font-weight: 600; }
  table {
    width: 100%;
    border-collapse: collapse;
    background: #16181d;
    border: 1px solid #27272a;
    border-radius: 0.5rem;
    overflow: hidden;
  }
  th, td {
    padding: 0.75rem 1rem;
    text-align: left;
    border-bottom: 1px solid #27272a;
    font-size: 0.875rem;
  }
  th { color: #71717a; font-weight: 500; }
  .status-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-right: 0.5rem;
  }
  .status-running .status-dot { background: #4ade80; }
  .status-stopped .status-dot { background: #71717a; }
  .status-creating .status-dot { background: #facc15; animation: pulse 1.5s infinite; }
  .status-failed .status-dot { background: #f87171; }
  .status-unhealthy .status-dot { background: #fb923c; }
  @keyframes pulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.4; } }
  .btn {
    padding: 0.35rem 0.75rem;
    border: 1px solid #27272a;
    border-radius: 0.25rem;
    background: #27272a;
    color: #e4e4e7;
    font-size: 0.75rem;
    cursor: pointer;
    margin-right: 0.25rem;
  }
  .btn:hover { background: #3f3f46; }
  .btn-danger { border-color: #7f1d1d; }
  .btn-danger:hover { background: #7f1d1d; }
  .btn-create {
    padding: 0.5rem 1rem;
    background: #1d4ed8;
    border: none;
    border-radius: 0.375rem;
    color: white;
    font-size: 0.875rem;
    cursor: pointer;
  }
  .btn-create:hover { background: #2563eb; }
  .empty {
    text-align: center;
    color: #52525b;
    padding: 3rem;
  }
  .empty p { margin-top: 0.5rem; font-size: 0.875rem; }
  .mono { font-family: monospace; font-size: 0.8rem; color: #a1a1aa; }
  .node-cards { display: flex; flex-direction: column; gap: 1rem; }
  .node-card {
    background: #16181d;
    border: 1px solid #27272a;
    border-radius: 0.5rem;
    overflow: hidden;
  }
  .node-card-header {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.75rem;
    padding: 1rem 1.25rem;
    border-bottom: 1px solid #1e1e22;
  }
  .node-card-header .node-name { font-weight: 600; font-size: 1rem; }
  .node-card-header .node-meta {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    flex: 1;
    min-width: 0;
  }
  .node-card-header .node-actions { margin-left: auto; display: flex; gap: 0.25rem; }
  .node-card-body { padding: 0.75rem 1.25rem; }
  .l1-list { margin: 0; padding: 0; list-style: none; }
  .l1-list li {
    display: flex;
    align-items: center;
    gap: 0.75rem;
    padding: 0.35rem 0;
    font-size: 0.85rem;
  }
  .l1-none { color: #52525b; font-size: 0.85rem; }
  .tag {
    display: inline-block;
    font-size: 0.75rem;
    padding: 0.15rem 0.5rem;
    border-radius: 0.25rem;
    background: #27272a;
    color: #a1a1aa;
  }
  .modal-overlay {
    display: none;
    position: fixed;
    inset: 0;
    background: rgba(0,0,0,0.6);
    z-index: 100;
    align-items: center;
    justify-content: center;
  }
  .modal-overlay.active { display: flex; }
  .modal {
    background: #16181d;
    border: 1px solid #27272a;
    border-radius: 0.5rem;
    padding: 1.5rem;
    width: 24rem;
    max-width: 90vw;
  }
  .modal h3 { margin-bottom: 1rem; font-size: 1rem; }
  .modal label { display: block; font-size: 0.875rem; color: #a1a1aa; margin-bottom: 0.25rem; }
  .modal input {
    width: 100%;
    padding: 0.5rem;
    margin-bottom: 0.75rem;
    background: #0f1117;
    border: 1px solid #27272a;
    border-radius: 0.25rem;
    color: #e4e4e7;
    font-size: 0.875rem;
  }
  .modal-actions { display: flex; gap: 0.5rem; justify-content: flex-end; margin-top: 1rem; }
  .error-msg { color: #f87171; font-size: 0.8rem; margin-bottom: 0.5rem; display: none; }
</style>
</head>
<body>
  <header>
    <h1>Avalauncher</h1>
    <div class="header-right">
      <span id="auth-badge" class="auth-status no">no key</span>
      <span class="version">v{{VERSION}}</span>
    </div>
  </header>
  <main>
    <div class="cards">
      <div class="card">
        <h2>Hosts</h2>
        <div class="value" id="hosts">-</div>
      </div>
      <div class="card">
        <h2>Nodes</h2>
        <div class="value" id="nodes">-</div>
      </div>
      <div class="card">
        <h2>L1s</h2>
        <div class="value" id="l1s">-</div>
      </div>
      <div class="card">
        <h2>Events</h2>
        <div class="value" id="events">-</div>
      </div>
    </div>

    <div class="section">
      <div class="section-header">
        <h2>Nodes</h2>
        <button class="btn-create" onclick="showCreateModal()">Create Node</button>
      </div>
      <div id="node-table"></div>
    </div>
  </main>

  <div class="modal-overlay" id="create-modal">
    <div class="modal">
      <h3>Create Node</h3>
      <div class="error-msg" id="create-error"></div>
      <label for="node-name">Name</label>
      <input type="text" id="node-name" placeholder="mainnet-1">
      <label for="node-port">Staking Port</label>
      <input type="number" id="node-port" value="9651" placeholder="9651">
      <label for="node-image">Image (optional)</label>
      <input type="text" id="node-image" placeholder="avaplatform/avalanchego:latest">
      <div class="modal-actions">
        <button class="btn" onclick="hideCreateModal()">Cancel</button>
        <button class="btn-create" onclick="createNode()">Create</button>
      </div>
    </div>
  </div>

  <div class="modal-overlay" id="key-modal">
    <div class="modal">
      <h3>Enter Admin Key</h3>
      <label for="admin-key">Bearer Token</label>
      <input type="password" id="admin-key" placeholder="admin key">
      <div class="modal-actions">
        <button class="btn" onclick="hideKeyModal()">Cancel</button>
        <button class="btn-create" onclick="saveKey()">Save</button>
      </div>
    </div>
  </div>

  <script>
    let adminKey = sessionStorage.getItem('adminKey') || '';

    function headers() {
      const h = {'Content-Type': 'application/json'};
      if (adminKey) h['Authorization'] = 'Bearer ' + adminKey;
      return h;
    }

    function updateAuthBadge(authenticated) {
      const b = document.getElementById('auth-badge');
      if (authenticated) {
        b.textContent = 'authenticated';
        b.className = 'auth-status ok';
      } else {
        b.textContent = 'click for key';
        b.className = 'auth-status no';
        b.style.cursor = 'pointer';
        b.onclick = showKeyModal;
      }
    }

    function showKeyModal() {
      document.getElementById('key-modal').classList.add('active');
      document.getElementById('admin-key').focus();
    }
    function hideKeyModal() { document.getElementById('key-modal').classList.remove('active'); }
    function saveKey() {
      adminKey = document.getElementById('admin-key').value.trim();
      sessionStorage.setItem('adminKey', adminKey);
      hideKeyModal();
      refresh();
    }

    function showCreateModal() {
      if (!adminKey) { showKeyModal(); return; }
      document.getElementById('create-error').style.display = 'none';
      document.getElementById('create-modal').classList.add('active');
      document.getElementById('node-name').focus();
    }
    function hideCreateModal() { document.getElementById('create-modal').classList.remove('active'); }

    async function createNode() {
      const name = document.getElementById('node-name').value.trim();
      const port = parseInt(document.getElementById('node-port').value) || 9651;
      const image = document.getElementById('node-image').value.trim();
      if (!name) { showError('create-error', 'Name is required'); return; }
      try {
        const body = {name, staking_port: port};
        if (image) body.image = image;
        const r = await fetch('/api/nodes', {method: 'POST', headers: headers(), body: JSON.stringify(body)});
        const d = await r.json();
        if (!r.ok) { showError('create-error', d.error || 'Failed'); return; }
        hideCreateModal();
        document.getElementById('node-name').value = '';
        refresh();
      } catch(e) { showError('create-error', e.message); }
    }

    function showError(id, msg) {
      const el = document.getElementById(id);
      el.textContent = msg;
      el.style.display = 'block';
    }

    async function nodeAction(id, action) {
      if (!adminKey) { showKeyModal(); return; }
      const method = action === 'delete' ? 'DELETE' : 'POST';
      const path = action === 'delete' ? '/api/nodes/' + id + '?remove_volumes=false' : '/api/nodes/' + id + '/' + action;
      try {
        await fetch(path, {method, headers: headers()});
        setTimeout(refresh, 500);
      } catch(e) { console.error(e); }
    }

    function statusClass(s) { return 'status-' + s; }

    function truncate(s, n) { return s && s.length > n ? s.substring(0, n) + '...' : s; }

    function renderNodes(nodes) {
      const el = document.getElementById('node-table');
      if (!nodes || nodes.length === 0) {
        el.innerHTML = '<div class="empty"><h2>No nodes</h2><p>Create a node to get started.</p></div>';
        return;
      }
      let html = '<div class="node-cards">';
      for (const n of nodes) {
        const sc = statusClass(n.status);
        const nid = n.node_id ? '<span class="mono">' + truncate(n.node_id, 24) + '</span>' : '';
        let actions = '';
        if (n.status === 'running' || n.status === 'unhealthy') {
          actions += '<button class="btn" onclick="nodeAction('+n.id+',\'stop\')">Stop</button>';
        } else if (n.status === 'stopped' || n.status === 'failed') {
          actions += '<button class="btn" onclick="nodeAction('+n.id+',\'start\')">Start</button>';
        }
        actions += '<button class="btn btn-danger" onclick="if(confirm(\'Delete node ' + n.name + '?\'))nodeAction('+n.id+',\'delete\')">Delete</button>';

        html += '<div class="node-card">';
        html += '<div class="node-card-header">';
        html += '<span class="node-name">' + n.name + '</span>';
        html += '<div class="node-meta">';
        html += '<span class="' + sc + '"><span class="status-dot"></span>' + n.status + '</span>';
        html += '<span class="mono">' + truncate(n.image, 30) + '</span>';
        html += '<span class="tag">:' + n.staking_port + '</span>';
        if (nid) html += nid;
        html += '</div>';
        html += '<div class="node-actions">' + actions + '</div>';
        html += '</div>';

        html += '<div class="node-card-body">';
        const l1s = n.l1s || [];
        if (l1s.length === 0) {
          html += '<span class="l1-none">No L1s</span>';
        } else {
          html += '<ul class="l1-list">';
          for (const l of l1s) {
            html += '<li>';
            html += '<span>' + l.name + '</span>';
            html += '<span class="mono">' + truncate(l.subnet_id, 16) + '</span>';
            html += '<span class="tag">' + l.vm + '</span>';
            html += '<span class="' + statusClass(l.status) + '"><span class="status-dot"></span>' + l.status + '</span>';
            html += '</li>';
          }
          html += '</ul>';
        }
        html += '</div>';
        html += '</div>';
      }
      html += '</div>';
      el.innerHTML = html;
    }

    async function refresh() {
      try {
        const r = await fetch('/api/status', {headers: headers()});
        const d = await r.json();
        if (d.counts) {
          document.getElementById('hosts').textContent = d.counts.hosts;
          document.getElementById('nodes').textContent = d.counts.nodes;
          document.getElementById('l1s').textContent = d.counts.l1s;
          document.getElementById('events').textContent = d.counts.events;
        }
        updateAuthBadge(d.authenticated);
        renderNodes(d.nodes || []);
      } catch(e) { console.error(e); }
    }

    // Initial load + auto-refresh every 10s.
    refresh();
    setInterval(refresh, 10000);
  </script>
</body>
</html>`
