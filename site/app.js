/**
 * agentic-go — Interactive behavior, typography switcher & WebMCP browser registry
 * Modern Apple Human Interface (macOS27 / iOS27) edition.
 */

document.addEventListener('DOMContentLoaded', () => {
  initTypographySwitcher();
  initVerificationSequence();
  initTabSelectors();
  initCopyButtons();
  initMobileNav();
  initSearchPalette();
  initInstallGenerator();
  initMcpConfigGenerator();
  initWebMCP();
});

/* --------------------------------------------------------------------------
   Typography Switcher (SF Pro / Inter Variable)
   -------------------------------------------------------------------------- */
function initTypographySwitcher() {
  const savedFont = localStorage.getItem('agentic-go-font') || 'sf-pro';
  document.documentElement.setAttribute('data-font', savedFont);

  const fontButtons = document.querySelectorAll('[data-font-btn]');
  fontButtons.forEach(btn => {
    const font = btn.getAttribute('data-font-btn');
    btn.classList.toggle('active', font === savedFont);

    btn.addEventListener('click', () => {
      document.documentElement.setAttribute('data-font', font);
      localStorage.setItem('agentic-go-font', font);
      fontButtons.forEach(b => b.classList.toggle('active', b.getAttribute('data-font-btn') === font));
    });
  });
}

/* --------------------------------------------------------------------------
   Verification Explorer Sequence State & Data
   -------------------------------------------------------------------------- */
const VERIFICATION_STEPS = [
  {
    id: 'snapshot',
    num: '01 / Snapshot',
    title: 'Workspace Snapshot',
    badgeClass: 'badge-azure',
    badge: 'Immutable Hash',
    heading: 'Content-Addressed Workspace Baseline',
    desc: 'Before reading or modifying code, agentic-go computes an immutable snapshot hash over workspace files and AST structures. All subsequent operations (briefs, symbols, refactoring, verification) are pinned to this Snapshot Ref. If source files change externally, stale references fail closed.',
    details: [
      { icon: 'info', text: 'Snapshot hash: <code>sha256:5e3037779544636ea1d...</code>' },
      { icon: 'info', text: 'Workspace containment: symlinks resolved within boundary' },
      { icon: 'info', text: 'Affected package closure: 6 packages computed' }
    ],
    codeTitle: 'Snapshot Metadata (agentic.context/v1)',
    jsonPayload: `{
  "snapshot_ref": "sha256:5e3037779544636ea1d0338cce1dc798bac1be4ad1f22d9622f68f95e1f45db7",
  "root_module": "github.com/agentic-mcps/go",
  "go_version": "1.27.0",
  "gopls_version": "v0.21.0",
  "contained_files_count": 142,
  "packages_loaded": 18,
  "staleness_detected": false
}`
  },
  {
    id: 'intent',
    num: '02 / Intent',
    title: 'Intent & Drift Check',
    badgeClass: 'badge-azure',
    badge: 'Change Continuity',
    heading: 'Tracking Drift Against Stated Goal',
    desc: 'A Change Contract records the intended goal, touched packages, and decisions. At each checkpoint, agentic-go compares the current AST against the baseline snapshot to catch exported API mutations, unintended dependency additions, generated-file modifications, or test deletions.',
    details: [
      { icon: 'info', text: 'Goal: "Add context-aware cancellation to worker pool"' },
      { icon: 'success', text: 'Exported API: Backward compatible (0 breaking changes)' },
      { icon: 'warn', text: 'Scope check: 1 touched package outside initial intent' }
    ],
    codeTitle: 'Change Contract (agentic.change/v1)',
    jsonPayload: `{
  "contract_id": "chg_8f2910ba4c92",
  "snapshot_ref": "sha256:5e3037779544636ea1d0338cce1dc798bac1be4ad1f22d9622f68f95e1f45db7",
  "goal": "Add context-aware cancellation to worker pool",
  "scope_packages": ["internal/execution", "internal/worker"],
  "drift_policy": {
    "exported_api_modified": false,
    "untracked_package_touched": true,
    "generated_files_touched": false,
    "tests_deleted": 0
  }
}`
  },
  {
    id: 'execute',
    num: '03 / Execution',
    title: 'Executed Evidence',
    badgeClass: 'badge-emerald',
    badge: 'Whole-Package Verification',
    heading: 'Targeted Compilation & Test Execution',
    desc: 'agentic-go executes whole-package tests, benchmarks, and the Go race detector over the full impacted package closure. It never selects cherry-picked individual test methods that could miss integration regressions or side effects.',
    details: [
      { icon: 'success', text: '198 tests passed across 6 impacted packages (0 failed, 7 skipped)' },
      { icon: 'success', text: 'Go race detector: 0 data races detected (-race active)' },
      { icon: 'success', text: 'Statement coverage on changed declarations: 69.2%' }
    ],
    codeTitle: 'Execution Evidence (agentic.verify/v1)',
    jsonPayload: `{
  "execution": {
    "toolchain": "go 1.27.0 (local)",
    "packages_tested": 6,
    "tests_total": 205,
    "tests_passed": 198,
    "tests_failed": 0,
    "tests_skipped": 7,
    "race_enabled": true,
    "data_races_detected": 0,
    "changed_statement_coverage_pct": 69.2,
    "duration_ms": 36420
  }
}`
  },
  {
    id: 'findings',
    num: '04 / Findings',
    title: 'Calibrated Findings',
    badgeClass: 'badge-emerald',
    badge: '0% False Positive Baseline',
    heading: 'High-Precision Analyzer Diagnostics',
    desc: 'Static analyzers evaluate concurrency safety (channel leaks, mutex copying, goroutine lifecycle) and error handling conventions. In release calibration over 10 repos and 467 reviewed findings, zero false positives were observed in the target corpus.',
    details: [
      { icon: 'success', text: 'Concurrency analyzer: 0 introduced issues (3 pre-existing noted)' },
      { icon: 'success', text: 'Error handling analyzer: 0 introduced issues' },
      { icon: 'info', text: 'Locations reported as workspace-relative byte offsets' }
    ],
    codeTitle: 'Analyzer Findings (agentic.verify/v1)',
    jsonPayload: `{
  "findings": {
    "introduced_total": 0,
    "preexisting_total": 3,
    "resolved_total": 0,
    "items": []
  },
  "diagnostics": {
    "compiler_errors": 0,
    "typecheck_errors": 0,
    "vet_diagnostics": 0
  }
}`
  },
  {
    id: 'uncertainty',
    num: '05 / Calibration',
    title: 'Explicit Uncertainty',
    badgeClass: 'badge-amber',
    badge: 'Honest Boundaries',
    heading: 'Calibrated Risk Lenses & Provenance',
    desc: 'agentic-go never claims that passing tests prove omitted code is completely safe. Instead, the verification report explicitly enumerates uncertainty lenses (e.g. unexercised branch statements, skipped integration suites, build constraints).',
    details: [
      { icon: 'info', text: 'Risk lenses: 4 identified (concurrency timing, edge coverage)' },
      { icon: 'info', text: 'Uncertainty points: 2 explicit caveats reported' },
      { icon: 'success', text: 'Policy result: PASS (Exit code 0)' }
    ],
    codeTitle: 'Uncertainty & Policy (agentic.verify/v1)',
    jsonPayload: `{
  "report_id": "verify_0cf9b0dc7f4cab1ee964a8a8fd20a532d8756cc84d728fb008bc7ce00338aa2f",
  "policy_result": "pass",
  "exit_code": 0,
  "risk_lenses": [
    { "type": "concurrency_boundary", "severity": "low", "note": "worker pool sync.WaitGroup modified" },
    { "type": "coverage_gap", "severity": "low", "note": "3 error recovery paths not hit by unit tests" }
  ],
  "uncertainties": [
    "Skipped 7 integration tests requiring remote network credentials",
    "Fuzz tests omitted during standard verification pass"
  ]
}`
  }
];

function selectVerificationStep(stepIdOrIndex) {
  let index = typeof stepIdOrIndex === 'number' ? stepIdOrIndex : VERIFICATION_STEPS.findIndex(s => s.id === stepIdOrIndex);
  if (index < 0 || index >= VERIFICATION_STEPS.length) index = 0;

  const container = document.getElementById('verification-sequence');
  if (!container) return;

  const nav = container.querySelector('.sequence-tabs');
  const explanation = container.querySelector('.sequence-explanation');
  const codeContent = container.querySelector('.sequence-code-content');
  const codeTitle = container.querySelector('.sequence-code-title');

  const step = VERIFICATION_STEPS[index];
  if (nav) {
    const buttons = nav.querySelectorAll('.seq-tab-btn');
    buttons.forEach((btn, i) => {
      btn.classList.toggle('active', i === index);
      btn.setAttribute('aria-selected', i === index ? 'true' : 'false');
    });
  }

  if (explanation) {
    let detailsHtml = step.details.map(d => `
      <li class="sequence-details-item">
        <span class="sequence-details-icon icon-${d.icon}">${d.icon === 'success' ? '✓' : d.icon === 'warn' ? '!' : 'i'}</span>
        <span>${d.text}</span>
      </li>
    `).join('');

    explanation.innerHTML = `
      <div class="sequence-badge ${step.badgeClass}">
        ${step.badge}
      </div>
      <h4>${step.heading}</h4>
      <p>${step.desc}</p>
      <ul class="sequence-details-list">
        ${detailsHtml}
      </ul>
      <div style="display: flex; gap: 10px; margin-top: 24px;">
        <button type="button" class="copy-btn" id="seq-prev-btn" ${index === 0 ? 'disabled style="opacity:0.3;cursor:not-allowed;"' : ''}>← Previous</button>
        <button type="button" class="copy-btn" id="seq-next-btn" style="background: var(--color-ink); color: #ffffff; border-color: var(--color-ink);" ${index === VERIFICATION_STEPS.length - 1 ? 'disabled style="opacity:0.3;cursor:not-allowed;"' : ''}>Next Step →</button>
      </div>
    `;

    const prevBtn = document.getElementById('seq-prev-btn');
    const nextBtn = document.getElementById('seq-next-btn');
    if (prevBtn && index > 0) prevBtn.addEventListener('click', () => selectVerificationStep(index - 1));
    if (nextBtn && index < VERIFICATION_STEPS.length - 1) nextBtn.addEventListener('click', () => selectVerificationStep(index + 1));
  }

  if (codeTitle) codeTitle.textContent = step.codeTitle;
  if (codeContent) codeContent.textContent = step.jsonPayload;

  const stepSelect = document.querySelector('form[toolname="inspect_verification_step"] select[name="step"]');
  if (stepSelect && stepSelect.value !== step.id) {
    stepSelect.value = step.id;
  }
}

function initVerificationSequence() {
  const container = document.getElementById('verification-sequence');
  if (!container) return;

  const nav = container.querySelector('.sequence-tabs');
  if (nav) {
    const buttons = nav.querySelectorAll('.seq-tab-btn');
    buttons.forEach((btn, i) => {
      btn.addEventListener('click', () => selectVerificationStep(i));
    });
  }

  selectVerificationStep(0);
}

/* --------------------------------------------------------------------------
   Generic Tab Selectors
   -------------------------------------------------------------------------- */
function initTabSelectors() {
  document.querySelectorAll('[data-tabs]').forEach(tabContainer => {
    const tabs = tabContainer.querySelectorAll('.tab-btn, .seq-tab-btn');
    const groupName = tabContainer.getAttribute('data-tabs');
    const contentPanels = document.querySelectorAll(`[data-tab-content="${groupName}"]`);

    tabs.forEach(tab => {
      tab.addEventListener('click', () => {
        const target = tab.getAttribute('data-target');

        tabs.forEach(t => t.classList.remove('active'));
        tab.classList.add('active');

        contentPanels.forEach(panel => {
          if (panel.getAttribute('data-panel') === target) {
            panel.style.display = 'block';
          } else {
            panel.style.display = 'none';
          }
        });
      });
    });
  });
}

/* --------------------------------------------------------------------------
   Copy to Clipboard
   -------------------------------------------------------------------------- */
function initCopyButtons() {
  document.querySelectorAll('.copy-btn').forEach(btn => {
    btn.addEventListener('click', async () => {
      let textToCopy = '';
      const targetId = btn.getAttribute('data-copy-target');

      if (targetId) {
        const targetElem = document.getElementById(targetId);
        if (targetElem) textToCopy = targetElem.innerText;
      } else {
        const pre = btn.closest('.sequence-code-pane, .mac-window, pre, .mac-body');
        if (pre) {
          const code = pre.querySelector('code') || pre;
          textToCopy = code.innerText;
        }
      }

      if (!textToCopy) return;
      textToCopy = textToCopy.replace(/^\$ /gm, '').trim();

      try {
        await navigator.clipboard.writeText(textToCopy);
        const originalText = btn.innerHTML;
        btn.classList.add('copied');
        btn.innerHTML = `✓ Copied`;
        setTimeout(() => {
          btn.classList.remove('copied');
          btn.innerHTML = originalText;
        }, 2000);
      } catch (err) {
        console.error('Failed to copy text: ', err);
      }
    });
  });
}

/* --------------------------------------------------------------------------
   Mobile Navigation
   -------------------------------------------------------------------------- */
function initMobileNav() {
  const menuBtn = document.querySelector('.mobile-menu-btn');
  const navLinks = document.querySelector('.nav-links');

  if (menuBtn && navLinks) {
    menuBtn.addEventListener('click', () => {
      const isVisible = navLinks.style.display === 'flex';
      navLinks.style.display = isVisible ? 'none' : 'flex';
      if (!isVisible) {
        navLinks.style.position = 'absolute';
        navLinks.style.top = '60px';
        navLinks.style.left = '16px';
        navLinks.style.right = '16px';
        navLinks.style.background = '#ffffff';
        navLinks.style.flexDirection = 'column';
        navLinks.style.padding = '16px';
        navLinks.style.borderRadius = '20px';
        navLinks.style.boxShadow = '0 20px 40px rgba(0,0,0,0.15)';
        navLinks.style.border = '1px solid #e5e7eb';
      }
    });
  }
}

/* --------------------------------------------------------------------------
   Documentation Search Index & Interactive Search Palette
   -------------------------------------------------------------------------- */
const DOCS_SEARCH_INDEX = [
  {
    title: "Documentation Overview & Mental Model",
    path: "/go/docs/",
    relPath: "docs/",
    summary: "High-level architecture, 5-stage agent loop, gopls sidecar containment, and MCP surface.",
    keywords: "overview, architecture, gopls, snapshot, mental model, tools, contracts, verify, loop"
  },
  {
    title: "Installation Guide",
    path: "/go/docs/install/",
    relPath: "docs/install/",
    summary: "Install agentic-go, agentic-go-gopls, and agentic-go-vet via Homebrew tap, curl script, or binary archives.",
    keywords: "install, installation, brew, homebrew, curl, script, binary, darwin, linux, arm64, amd64, go version"
  },
  {
    title: "MCP Client Connection",
    path: "/go/docs/connect/",
    relPath: "docs/connect/",
    summary: "Connect Claude Desktop, Cursor, Cline, and Claude Code over standard stdio transport.",
    keywords: "connect, mcp config, claude desktop, cursor, cline, claude code, stdio, mcpServers, configuration"
  },
  {
    title: "Verification Engine",
    path: "/go/docs/verify/",
    relPath: "docs/verify/",
    summary: "Whole-package test execution, Go race detector (-race), changed-statement coverage, and calibrated uncertainty.",
    keywords: "verify, verification, test, race, data race, coverage, findings, uncertainty, exit codes, policy"
  },
  {
    title: "Safety & Containment",
    path: "/go/docs/safety/",
    relPath: "docs/safety/",
    summary: "Workspace symlink resolution containment, non-destructive mutation guarantees, and execution permissions.",
    keywords: "safety, containment, sandbox, git mutation, permissions, security, read-only, boundaries"
  },
  {
    title: "GitHub Action in CI",
    path: "/go/docs/action/",
    relPath: "docs/action/",
    summary: "Automated PR gating and verification reports with agentic-mcps/go/action in CI workflows.",
    keywords: "github action, ci, continuous integration, pr, pull request, automated verification, workflow"
  },
  {
    title: "Protocol Contracts & Schemas",
    path: "/go/docs/contracts/",
    relPath: "docs/contracts/",
    summary: "Frozen specification for 14 MCP tools, 7 resources, 6 prompts, and JSON schemas (context, change, verify).",
    keywords: "contracts, schemas, protocol, tools, 14 tools, resources, prompts, agentic.context/v1, agentic.change/v1, agentic.verify/v1"
  },
  {
    title: "Frequently Asked Questions",
    path: "/go/docs/faq/",
    relPath: "docs/faq/",
    summary: "Detailed answers on gopls sidecar isolation, sandbox vs containment, open weights, and licensing.",
    keywords: "faq, questions, gopls, sandboxing, licenses, open source, models, deepseek, llama, qwen"
  }
];

function performDocSearch(query) {
  const q = String(query || "").trim().toLowerCase();
  if (!q) return [];

  const tokens = q.split(/\s+/);
  return DOCS_SEARCH_INDEX.map(item => {
    let score = 0;
    const titleLower = item.title.toLowerCase();
    const summaryLower = item.summary.toLowerCase();
    const keywordsLower = item.keywords.toLowerCase();
    const pathLower = item.path.toLowerCase();

    tokens.forEach(tok => {
      if (titleLower.includes(tok)) score += 10;
      if (keywordsLower.includes(tok)) score += 5;
      if (summaryLower.includes(tok)) score += 3;
      if (pathLower.includes(tok)) score += 2;
    });

    return { ...item, score };
  })
  .filter(r => r.score > 0)
  .sort((a, b) => b.score - a.score);
}

function initSearchPalette() {
  const isSubdir = window.location.pathname.includes('/docs/');
  const depth = (window.location.pathname.match(/\/docs\/[^\/]+/g) || []).length;
  const rootPrefix = depth > 0 ? '../../' : isSubdir ? '../' : './';

  let modal = document.getElementById('search-modal');
  if (!modal) {
    modal = document.createElement('div');
    modal.id = 'search-modal';
    modal.className = 'search-modal-backdrop';
    modal.setAttribute('role', 'dialog');
    modal.setAttribute('aria-modal', 'true');
    modal.setAttribute('aria-label', 'Documentation Search');
    modal.innerHTML = `
      <div class="search-modal-dialog">
        <form class="search-modal-form" toolname="search_documentation" tooldescription="Search agentic-go documentation topics, MCP connection guides, installation instructions, and schema definitions." toolautosubmit action="./" method="GET">
          <div class="search-modal-input-wrapper">
            <svg class="search-icon" width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="11" cy="11" r="8"/><path d="m21 21-4.35-4.35"/></svg>
            <input type="search" name="query" class="search-modal-input" placeholder="Search documentation... (e.g. verify, gopls, install, contracts)" autocomplete="off" toolparamdescription="Keywords or topics to search within the documentation (e.g., install, verify, gopls, mcp, contracts)" />
            <kbd class="search-modal-esc">ESC</kbd>
          </div>
        </form>
        <div class="search-modal-results" id="search-modal-results">
          <div class="search-empty-state">Type a keyword to search documentation routes, schemas, and tools.</div>
        </div>
      </div>
    `;
    document.body.appendChild(modal);
  }

  const input = modal.querySelector('.search-modal-input');
  const resultsContainer = modal.querySelector('#search-modal-results');
  const form = modal.querySelector('.search-modal-form');

  function openSearch(initialQuery = '') {
    modal.classList.add('active');
    document.body.style.overflow = 'hidden';
    if (input) {
      if (initialQuery) input.value = initialQuery;
      setTimeout(() => input.focus(), 50);
      renderResults(input.value);
    }
  }

  function closeSearch() {
    modal.classList.remove('active');
    document.body.style.overflow = '';
  }

  function renderResults(q) {
    const results = performDocSearch(q);
    if (!q.trim()) {
      resultsContainer.innerHTML = `<div class="search-empty-state">Type a keyword to search documentation routes, schemas, and tools.</div>`;
      return;
    }
    if (results.length === 0) {
      resultsContainer.innerHTML = `<div class="search-empty-state">No matching documentation topics found for "<strong>${escapeHtml(q)}</strong>".</div>`;
      return;
    }

    resultsContainer.innerHTML = results.map((r, i) => `
      <a href="${rootPrefix}${r.relPath}" class="search-result-item ${i === 0 ? 'focused' : ''}">
        <div class="search-result-title">${escapeHtml(r.title)}</div>
        <div class="search-result-summary">${escapeHtml(r.summary)}</div>
        <div class="search-result-path">${escapeHtml(r.path)}</div>
      </a>
    `).join('');
  }

  if (input) {
    input.addEventListener('input', (e) => renderResults(e.target.value));
  }

  if (form) {
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      const firstResult = resultsContainer.querySelector('.search-result-item');
      if (firstResult) window.location.href = firstResult.getAttribute('href');
    });
  }

  modal.addEventListener('click', (e) => {
    if (e.target === modal) closeSearch();
  });

  document.addEventListener('keydown', (e) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      if (modal.classList.contains('active')) closeSearch();
      else openSearch();
    } else if (e.key === 'Escape' && modal.classList.contains('active')) {
      closeSearch();
    }
  });

  document.querySelectorAll('[data-search-trigger], form[toolname="search_documentation"]:not(.search-modal-form)').forEach(elem => {
    if (elem.tagName === 'FORM') {
      elem.addEventListener('submit', (e) => {
        e.preventDefault();
        const qInput = elem.querySelector('input[name="query"]');
        openSearch(qInput ? qInput.value : '');
      });
    } else {
      elem.addEventListener('click', (e) => {
        e.preventDefault();
        openSearch();
      });
    }
  });
}

function escapeHtml(str) {
  return String(str || '').replace(/[&<>"']/g, m => ({
    '&': '&amp;',
    '<': '&lt;',
    '>': '&gt;',
    '"': '&quot;',
    "'": '&#39;'
  }[m]));
}

/* --------------------------------------------------------------------------
   Install Command Generator (WebMCP Form & Live UI Sync)
   -------------------------------------------------------------------------- */
function generateInstallCommandPayload(params = {}) {
  const method = params.method || 'brew';
  const os = params.os || 'darwin';
  const arch = params.arch || 'arm64';

  let command = '';
  let instructions = '';

  if (method === 'brew') {
    command = 'brew install agentic-mcps/tap/agentic-go\nagentic-go --version';
    instructions = 'Installs agentic-go, agentic-go-gopls companion, and agentic-go-vet via official Homebrew tap.';
  } else if (method === 'curl') {
    command = 'curl -fsSL https://raw.githubusercontent.com/agentic-mcps/go/v1.0.0/scripts/install.sh | bash -s -- 1.0.0';
    instructions = 'Downloads and installs the latest pinned v1.0.0 release binaries to /usr/local/bin or ~/.local/bin.';
  } else {
    const archiveName = `agentic-go_1.0.0_${os}_${arch}.tar.gz`;
    command = `curl -LO https://github.com/agentic-mcps/go/releases/download/v1.0.0/${archiveName}\ntar -xzf ${archiveName}\nsudo mv agentic-go agentic-go-gopls agentic-go-vet /usr/local/bin/`;
    instructions = `Direct binary archive installation for ${os}/${arch}.`;
  }

  return { method, os, arch, command, instructions };
}

function initInstallGenerator() {
  const form = document.querySelector('form[toolname="generate_install_command"]');
  if (!form) return;

  function update() {
    const method = form.elements['method'] ? form.elements['method'].value : 'brew';
    const os = form.elements['os'] ? form.elements['os'].value : 'darwin';
    const arch = form.elements['arch'] ? form.elements['arch'].value : 'arm64';

    const result = generateInstallCommandPayload({ method, os, arch });
    const codeBlock = document.getElementById('install-generated-command');
    if (codeBlock) {
      codeBlock.textContent = result.command;
    }
  }

  form.addEventListener('change', update);
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    update();
  });
}

/* --------------------------------------------------------------------------
   MCP Client Config Generator (WebMCP Form & Live UI Sync)
   -------------------------------------------------------------------------- */
function generateMcpConfigPayload(params = {}) {
  const client = params.client || 'claude';
  const workspace = params.workspace_path || params.workspace || '/path/to/your/go/project';

  let configObj = {};
  let targetFile = '';

  if (client === 'claude') {
    targetFile = '~/Library/Application Support/Claude/claude_desktop_config.json';
    configObj = {
      mcpServers: {
        "agentic-go": {
          command: "agentic-go",
          args: ["--workspace", workspace]
        }
      }
    };
  } else if (client === 'cursor') {
    targetFile = '.cursor/mcp.json';
    configObj = {
      mcpServers: {
        "agentic-go": {
          command: "agentic-go",
          args: ["--workspace", workspace]
        }
      }
    };
  } else if (client === 'cline') {
    targetFile = '~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json';
    configObj = {
      mcpServers: {
        "agentic-go": {
          command: "agentic-go",
          args: ["--workspace", workspace]
        }
      }
    };
  } else if (client === 'claudecode') {
    targetFile = 'Terminal CLI Flag / claude.json';
    configObj = {
      mcpServers: {
        "agentic-go": {
          command: "agentic-go",
          args: ["--workspace", workspace]
        }
      }
    };
  }

  return {
    client,
    workspace_path: workspace,
    target_config_file: targetFile,
    config_json: JSON.stringify(configObj, null, 2),
    config: configObj
  };
}

function initMcpConfigGenerator() {
  const form = document.querySelector('form[toolname="generate_mcp_config"]');
  if (!form) return;

  function update() {
    const client = form.elements['client'] ? form.elements['client'].value : 'claude';
    const workspace = form.elements['workspace'] ? form.elements['workspace'].value : '/path/to/your/go/project';

    const result = generateMcpConfigPayload({ client, workspace_path: workspace });
    const codeBlock = document.getElementById('mcp-generated-config');
    if (codeBlock) {
      codeBlock.textContent = result.config_json;
    }
  }

  form.addEventListener('input', update);
  form.addEventListener('change', update);
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    update();
  });
}

/* --------------------------------------------------------------------------
   WebMCP In-Browser Tool Registry (Level 5 Agent Native & Google WebMCP Standard)
   -------------------------------------------------------------------------- */
function initWebMCP() {
  const FAQ_DATABASE = [
    {
      topic: "gopls",
      question: "Does agentic-go replace gopls?",
      answer: "No. agentic-go bundles an exact, tested gopls v0.21.0 companion named agentic-go-gopls. It uses gopls as a private stdio sidecar for type analysis, definition lookups, and symbol references, while layering immutable snapshot lineage, change continuity contracts, guarded deterministic refactoring, and executed whole-package verification around it."
    },
    {
      topic: "open-weight",
      question: "How does it help open-weight models?",
      answer: "Open models (DeepSeek, Llama, Qwen, Mistral) excel at code generation, but often lack closed-lab tool harnesses. agentic-go gives them deterministic AST lookups, change contracts to prevent drift, and whole-package test verification—producing results on par with proprietary frontier setups."
    },
    {
      topic: "git",
      question: "Can agentic-go modify my files or Git history?",
      answer: "agentic-go will never mutate Git repository state (it never executes git add, git commit, git branch, or git stash). The only mutating tool is go_refactor, which applies deterministic AST edits only to existing contained non-generated files after checking file preimages and writing a recovery journal."
    },
    {
      topic: "sandbox",
      question: "What execution privileges does verification require?",
      answer: "Execution tools compile and run Go test suites using the local go toolchain with the caller's privileges (same as running go test ./...). All workspace paths are contained and symlink-resolved, but this is containment, not a sandbox. Do not run verification on untrusted code."
    },
    {
      topic: "toolchain",
      question: "Which Go versions are supported?",
      answer: "We explicitly support Go 1.25, Go 1.26, and Go 1.27 on macOS (Darwin) and Linux for both amd64 and arm64 architectures. Server startup verifies the host toolchain against the workspace's minimum Go requirement using GOTOOLCHAIN=local."
    },
    {
      topic: "network",
      question: "Does agentic-go embed an LLM or make external network calls?",
      answer: "No. agentic-go is a strictly local, deterministic developer tool that communicates over stdio using JSON-RPC Model Context Protocol. It embeds zero AI models, requires zero API keys, and transmits zero telemetry or code to external servers."
    }
  ];

  const agentTools = [
    {
      name: "search_documentation",
      description: "Search agentic-go documentation topics, routes, capabilities, and protocol contracts.",
      inputSchema: {
        type: "object",
        properties: {
          query: {
            type: "string",
            description: "Search keyword or topic (e.g. install, verify, gopls, mcp, contracts)"
          }
        },
        required: ["query"],
        additionalProperties: false
      },
      execute: async ({ query } = {}) => {
        const results = performDocSearch(query);
        return {
          content: [
            {
              type: "text",
              text: JSON.stringify(results, null, 2)
            }
          ],
          results
        };
      }
    },
    {
      name: "get_quickstart",
      description: "Returns ready-to-run installation commands, Homebrew formula, curl installer, and MCP stdio config for agentic-go.",
      inputSchema: {
        type: "object",
        properties: {},
        additionalProperties: false
      },
      execute: async () => {
        const data = {
          brew: "brew install agentic-mcps/tap/agentic-go",
          curl: "curl -fsSL https://raw.githubusercontent.com/agentic-mcps/go/v1.0.0/scripts/install.sh | bash -s -- 1.0.0",
          mcpConfig: {
            mcpServers: {
              "agentic-go": {
                command: "agentic-go",
                args: ["--workspace", "."]
              }
            }
          },
          supportedGoVersions: ["1.25", "1.26", "1.27"],
          platforms: ["darwin/arm64", "darwin/amd64", "linux/arm64", "linux/amd64"]
        };
        return {
          content: [{ type: "text", text: JSON.stringify(data, null, 2) }],
          data
        };
      }
    },
    {
      name: "generate_install_command",
      description: "Generate the terminal installation command for agentic-go based on package manager, operating system, and architecture.",
      inputSchema: {
        type: "object",
        properties: {
          method: {
            type: "string",
            enum: ["brew", "curl", "binary"],
            description: "Installation method: brew (Homebrew tap), curl (shell installer), or binary (direct release archive)"
          },
          os: {
            type: "string",
            enum: ["darwin", "linux"],
            description: "Target operating system (darwin for macOS, linux for Linux)"
          },
          arch: {
            type: "string",
            enum: ["arm64", "amd64"],
            description: "Target CPU architecture (arm64 for Apple Silicon/ARM64, amd64 for Intel/x86_64)"
          }
        },
        required: ["method"],
        additionalProperties: false
      },
      execute: async (params = {}) => {
        const payload = generateInstallCommandPayload(params);
        return {
          content: [{ type: "text", text: payload.command }],
          payload
        };
      }
    },
    {
      name: "generate_mcp_config",
      description: "Generate JSON configuration snippet for connecting agentic-go with AI coding clients including Claude Desktop, Cursor, Cline, or Claude Code.",
      inputSchema: {
        type: "object",
        properties: {
          client: {
            type: "string",
            enum: ["claude", "cursor", "cline", "claudecode"],
            description: "Target AI coding assistant client"
          },
          workspace_path: {
            type: "string",
            description: "Absolute file path to the target Go workspace directory (defaults to current directory .)"
          }
        },
        required: ["client"],
        additionalProperties: false
      },
      execute: async (params = {}) => {
        const payload = generateMcpConfigPayload(params);
        return {
          content: [{ type: "text", text: payload.config_json }],
          payload
        };
      }
    },
    {
      name: "get_tool_catalog",
      description: "Returns the complete frozen surface inventory of agentic-go v1.0.0: 14 tools, 7 resources, 1 resource template, and 6 prompts.",
      inputSchema: {
        type: "object",
        properties: {},
        additionalProperties: false
      },
      execute: async () => {
        const catalog = {
          toolsCount: 14,
          tools: [
            "go_workspace_brief", "go_search", "go_symbol_context",
            "go_begin_change", "go_checkpoint_change", "go_refactor",
            "go_verify_change", "go_test_structured", "go_race_report",
            "go_coverage_gaps", "go_benchmark_diff", "go_flake_finder",
            "go_audit_concurrency", "go_audit_errors"
          ],
          resourcesCount: 7,
          resources: [
            "agentic-go://analysis-rules", "agentic-go://capabilities",
            "agentic-go://change-contract/current", "agentic-go://module",
            "agentic-go://packages", "agentic-go://trace-summary",
            "agentic-go://verification/latest"
          ],
          resourceTemplates: ["agentic-go://artifact/{id}"],
          promptsCount: 6,
          prompts: [
            "audit-package", "bisect-flake", "pre-commit-check",
            "resume-change", "understand-change", "verify-change"
          ]
        };
        return {
          content: [{ type: "text", text: JSON.stringify(catalog, null, 2) }],
          catalog
        };
      }
    },
    {
      name: "get_verification_sample",
      description: "Returns a canonical agentic.verify/v1 verification report payload with whole-package test evidence, race reports, and calibrated uncertainty.",
      inputSchema: {
        type: "object",
        properties: {
          step: {
            type: "string",
            enum: ["snapshot", "intent", "execute", "findings", "uncertainty"],
            description: "Optional verification pipeline stage to inspect"
          }
        },
        additionalProperties: false
      },
      execute: async ({ step } = {}) => {
        if (step) {
          const stepObj = VERIFICATION_STEPS.find(s => s.id === step);
          if (stepObj) {
            return {
              content: [{ type: "text", text: stepObj.jsonPayload }],
              step: stepObj
            };
          }
        }
        const sample = {
          schema: "agentic.verify/v1",
          report_id: "verify_0cf9b0dc7f4cab1ee964a8a8fd20a532d8756cc84d728fb008bc7ce00338aa2f",
          snapshot_ref: "sha256:5e3037779544636ea1d0338cce1dc798bac1be4ad1f22d9622f68f95e1f45db7",
          policy_result: "pass",
          exit_code: 0,
          tests_passed: 198,
          data_races: 0,
          changed_statement_coverage_pct: 69.2,
          findings_count: 0,
          risk_lenses_count: 4,
          uncertainties_count: 2
        };
        return {
          content: [{ type: "text", text: JSON.stringify(sample, null, 2) }],
          sample
        };
      }
    },
    {
      name: "inspect_verification_step",
      description: "Inspect and navigate to a specific step of the interactive verification pipeline explorer on the page.",
      inputSchema: {
        type: "object",
        properties: {
          step: {
            type: "string",
            enum: ["snapshot", "intent", "execute", "findings", "uncertainty"],
            description: "The verification pipeline stage to inspect"
          }
        },
        required: ["step"],
        additionalProperties: false
      },
      execute: async ({ step } = {}) => {
        const found = VERIFICATION_STEPS.find(s => s.id === step);
        if (!found) {
          return {
            isError: true,
            content: [{ type: "text", text: `Unknown step "${step}". Allowed values: snapshot, intent, execute, findings, uncertainty.` }]
          };
        }
        selectVerificationStep(step);
        return {
          content: [{ type: "text", text: `Selected verification step "${found.num}": ${found.heading}` }],
          step: found
        };
      }
    },
    {
      name: "get_faq_answers",
      description: "Query frequently asked questions and official architectural answers regarding agentic-go, gopls integration, sandboxing, and refactoring safety.",
      inputSchema: {
        type: "object",
        properties: {
          topic: {
            type: "string",
            description: "Optional FAQ keyword or topic filter (e.g. gopls, git, sandbox, toolchain, open-weight)"
          }
        },
        additionalProperties: false
      },
      execute: async ({ topic } = {}) => {
        let results = FAQ_DATABASE;
        if (topic && topic.trim()) {
          const t = topic.trim().toLowerCase();
          results = FAQ_DATABASE.filter(f => f.topic.includes(t) || f.question.toLowerCase().includes(t) || f.answer.toLowerCase().includes(t));
        }
        return {
          content: [{ type: "text", text: JSON.stringify(results, null, 2) }],
          count: results.length,
          faq: results
        };
      }
    },
    {
      name: "get_contract_specification",
      description: "Returns the official schema specification, invariants, and purpose of an agentic-go protocol contract.",
      inputSchema: {
        type: "object",
        properties: {
          contract: {
            type: "string",
            enum: ["context", "change", "verify"],
            description: "Protocol contract to retrieve (context: agentic.context/v1, change: agentic.change/v1, verify: agentic.verify/v1)"
          }
        },
        required: ["contract"],
        additionalProperties: false
      },
      execute: async ({ contract } = {}) => {
        const specs = {
          context: {
            schema: "agentic.context/v1",
            title: "Context Pack Contract",
            purpose: "Source-grounded workspace context, package APIs, and call graphs for coding agents without blowing token budgets.",
            producers: ["go_workspace_brief", "go_symbol_context"],
            invariants: ["Pinned to immutable Snapshot Ref", "One-based byte columns", "Deterministic output"]
          },
          change: {
            schema: "agentic.change/v1",
            title: "Change Contract",
            purpose: "Change continuity across model checkpoints, preventing silent scope creep, exported API breakage, and test deletion.",
            producers: ["go_begin_change", "go_checkpoint_change"],
            invariants: ["Exact snapshot lineage", "Stale ref rejection", "Private user cache storage"]
          },
          verify: {
            schema: "agentic.verify/v1",
            title: "Verification Report Contract",
            purpose: "Evidence-backed verification combining whole-package test runs, race detection, AST drift, and calibrated analyzers.",
            producers: ["go_verify_change", "agentic-go-vet CLI", "GitHub Action"],
            invariants: ["Never cherry-picks unit tests", "Explicit risk lenses & uncertainties", "0% false positive baseline"]
          }
        };
        const selected = specs[contract];
        if (!selected) {
          return {
            isError: true,
            content: [{ type: "text", text: `Unknown contract "${contract}". Allowed values: context, change, verify.` }]
          };
        }
        return {
          content: [{ type: "text", text: JSON.stringify(selected, null, 2) }],
          contract: selected
        };
      }
    },
    {
      name: "set_page_font",
      description: "Set the typographic system font on the agentic-go documentation website (SF Pro for Apple Human Interface or Inter Variable).",
      inputSchema: {
        type: "object",
        properties: {
          font: {
            type: "string",
            enum: ["sf-pro", "inter"],
            description: "Typography system font family"
          }
        },
        required: ["font"],
        additionalProperties: false
      },
      execute: async ({ font } = {}) => {
        if (font !== "sf-pro" && font !== "inter") {
          return {
            isError: true,
            content: [{ type: "text", text: `Invalid font "${font}". Allowed values: "sf-pro", "inter".` }]
          };
        }
        document.documentElement.setAttribute('data-font', font);
        try { localStorage.setItem('agentic-go-font', font); } catch {}
        const fontButtons = document.querySelectorAll('[data-font-btn]');
        fontButtons.forEach(b => b.classList.toggle('active', b.getAttribute('data-font-btn') === font));
        return {
          content: [{ type: "text", text: `Typography font updated to "${font}".` }],
          font
        };
      }
    }
  ];

  // Native WebMCP API registration if supported by host browser/agent
  if (typeof document !== 'undefined' && document.modelContext && typeof document.modelContext.registerTool === 'function') {
    try {
      agentTools.forEach(tool => {
        document.modelContext.registerTool({
          name: tool.name,
          description: tool.description,
          inputSchema: tool.inputSchema,
          execute: tool.execute
        });
      });
    } catch (err) {
      console.warn('Native WebMCP registerTool warning:', err);
    }
  }

  // ModelContext Protocol Dual Array/Object Registry Handler
  function createToolList() {
    const list = agentTools.map(t => ({
      name: t.name,
      description: t.description,
      inputSchema: t.inputSchema
    }));
    // Provide both array indexing and .tools property for cross-client compatibility
    list.tools = list;
    return list;
  }

  const modelContextRegistry = {
    listTools: async () => createToolList(),
    getTools: async () => createToolList(),
    callTool: async (nameOrParams, maybeParams = {}) => {
      let name, params;
      if (typeof nameOrParams === 'object' && nameOrParams !== null) {
        name = nameOrParams.name;
        params = nameOrParams.arguments || nameOrParams.params || {};
      } else {
        name = nameOrParams;
        params = maybeParams || {};
      }
      return modelContextRegistry.executeTool(name, params);
    },
    executeTool: async (name, input = {}) => {
      const tool = agentTools.find(t => t.name === name);
      if (!tool) {
        return {
          isError: true,
          content: [{ type: "text", text: `Tool "${name}" not found in agentic-go WebMCP registry.` }]
        };
      }
      try {
        return await tool.execute(input || {});
      } catch (err) {
        return {
          isError: true,
          content: [{ type: "text", text: `Execution error in "${name}": ${err.message}` }]
        };
      }
    },
    registerTool: (tool) => {
      if (!tool || !tool.name) return;
      const idx = agentTools.findIndex(t => t.name === tool.name);
      if (idx >= 0) agentTools[idx] = tool;
      else agentTools.push(tool);
    },
    unregisterTool: (name) => {
      const idx = agentTools.findIndex(t => t.name === name);
      if (idx >= 0) agentTools.splice(idx, 1);
    }
  };

  // Expose registry across all standard agent detection targets
  if (typeof window !== 'undefined') window.modelContext = modelContextRegistry;

  if (typeof navigator !== 'undefined') {
    try { Object.defineProperty(navigator, 'modelContext', { value: modelContextRegistry, writable: true, configurable: true }); } catch { navigator.modelContext = modelContextRegistry; }
    try { Object.defineProperty(navigator, 'modelContextTesting', { value: modelContextRegistry, writable: true, configurable: true }); } catch { navigator.modelContextTesting = modelContextRegistry; }
  }

  if (typeof document !== 'undefined') {
    try { Object.defineProperty(document, 'modelContext', { value: modelContextRegistry, writable: true, configurable: true }); } catch { document.modelContext = modelContextRegistry; }
  }

  // Declarative Form Auto-binding
  document.querySelectorAll('form[toolname]').forEach(form => {
    const toolName = form.getAttribute('toolname');
    const hasAutoSubmit = form.hasAttribute('toolautosubmit');

    function handleFormExecution() {
      const formData = new FormData(form);
      const params = {};
      for (const [key, val] of formData.entries()) {
        params[key] = val;
      }
      modelContextRegistry.executeTool(toolName, params).catch(console.error);
    }

    form.addEventListener('submit', (e) => {
      e.preventDefault();
      handleFormExecution();
    });

    if (hasAutoSubmit) {
      form.addEventListener('change', () => {
        handleFormExecution();
      });
    }
  });
}
