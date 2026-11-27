param([string]$Label = "layout", [int]$Width = 375,
      [string]$Chrome = "C:\Program Files\Google\Chrome\Application\chrome.exe")

# Layout measurements (no screenshots) of the running app at http://localhost:8080.
# Each page is fetched and loaded into an iframe of the given width (a real
# 375px viewport; headless Chrome clamps windows to >= 500px). A script in the
# frame measures the layout and posts the numbers to the parent, which
# headless Chrome prints via --dump-dom.
#
#   .\scripts\measure-layout.ps1            # 375px
#   .\scripts\measure-layout.ps1 -Width 1000
$sp = Join-Path $env:TEMP "expense-log-measure"
New-Item -ItemType Directory -Force $sp | Out-Null
$chrome = $Chrome
$base = "http://localhost:8080"

$measureJs = @'
<script>
(function () {
  const r = el => el.getBoundingClientRect();
  const panel = document.querySelector('.panel'), h1 = panel.querySelector('h1');
  const header = document.querySelector('.site-header');
  const table = panel.querySelector('table');
  const desc = [...document.querySelectorAll('td.desc')].find(td => td.textContent.includes('https://'));
  parent.postMessage(JSON.stringify({
    viewportWidth: innerWidth,
    pageScrollWidth: document.documentElement.scrollWidth,
    pageOverflowsHorizontally: document.documentElement.scrollWidth > document.documentElement.clientWidth,
    panelRight: Math.round(r(panel).right),
    tableRight: table ? Math.round(r(table).right) : null,
    longDescCellWidth: desc ? Math.round(r(desc).width) : null,
    longDescCellHeight: desc ? Math.round(r(desc).height) : null,
    gapHeaderToPanel: Math.round(r(panel).top - r(header).bottom),
    gapPanelTopToHeading: Math.round(r(h1).top - r(panel).top),
    h1MarginTop: getComputedStyle(h1).marginTop,
    panelDisplay: getComputedStyle(panel).display
  }), '*');
})();
</script>
'@

function Get-LayoutMeasurement($name, $path) {
  $page = (Invoke-WebRequest -UseBasicParsing "$base$path").Content
  $page = $page.Replace('<head>', "<head><base href=""$base/"">").Replace('</body>', "$measureJs</body>")
  $pageJs = ConvertTo-Json $page -Compress
  $wrapper = @"
<!doctype html><html><body style="margin:0">
<iframe id="f" width="$Width" height="1200" style="border:0"></iframe>
<script>
addEventListener('message', e => { const p = document.createElement('pre'); p.id = 'measure'; p.textContent = e.data; document.body.appendChild(p); });
document.getElementById('f').srcdoc = $pageJs;
</script></body></html>
"@
  $file = Join-Path $sp "measure-$name.html"
  [IO.File]::WriteAllText($file, $wrapper, [Text.UTF8Encoding]::new($false))
  $dom = & $chrome --headless=new --disable-gpu --hide-scrollbars "--window-size=800,1300" "--user-data-dir=$sp\chrome-profile" --virtual-time-budget=5000 --dump-dom "file:///$($file.Replace('\','/'))" 2>$null | Out-String
  if ($dom -match '<pre id="measure">(.*?)</pre>') { "$Label $name ${Width}px: $($Matches[1])" } else { "$Label ${name}: no result" }
}

Get-LayoutMeasurement "list" "/expenses?month=2026-11"
Get-LayoutMeasurement "summary" "/summary?month=2026-11"
