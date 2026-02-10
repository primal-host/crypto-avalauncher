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
  header .version { color: #71717a; font-size: 0.875rem; }
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
  .placeholder {
    background: #16181d;
    border: 1px solid #27272a;
    border-radius: 0.5rem;
    padding: 3rem;
    text-align: center;
    color: #52525b;
  }
  .placeholder p { margin-top: 0.5rem; font-size: 0.875rem; }
</style>
</head>
<body>
  <header>
    <h1>Avalauncher</h1>
    <span class="version">v{{VERSION}}</span>
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
    <div class="placeholder">
      <h2>Dashboard coming soon</h2>
      <p>Node management, L1 deployment, and cluster monitoring will appear here.</p>
    </div>
  </main>
  <script>
    fetch('/api/status').then(r => r.json()).then(d => {
      if (d.counts) {
        document.getElementById('hosts').textContent = d.counts.hosts;
        document.getElementById('nodes').textContent = d.counts.nodes;
        document.getElementById('l1s').textContent = d.counts.l1s;
        document.getElementById('events').textContent = d.counts.events;
      }
    }).catch(() => {});
  </script>
</body>
</html>`
