/**
 * agentic-go — Interactive behavior, typography switcher & WebMCP browser registry
 * Built for Apple 'MacBook Neo' design system.
 */

document.addEventListener('DOMContentLoaded', () => {
  initTypographySwitcher();
  initVerificationSequence();
  initTabSelectors();
  initCopyButtons();
  initMobileNav();
  initWebMCP();
});

/* --------------------------------------------------------------------------
   Typography Switcher (SF Pro / Inter)
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

function initVerificationSequence() {
  const container = document.getElementById('verification-sequence');
  if (!container) return;

  const nav = container.querySelector('.sequence-tabs');
  const explanation = container.querySelector('.sequence-explanation');
  const codeContent = container.querySelector('.sequence-code-content');
  const codeTitle = container.querySelector('.sequence-code-title');

  if (!nav || !explanation || !codeContent) return;

  function renderStep(index) {
    const step = VERIFICATION_STEPS[index];

    const buttons = nav.querySelectorAll('.seq-tab-btn');
    buttons.forEach((btn, i) => {
      btn.classList.toggle('active', i === index);
      btn.setAttribute('aria-selected', i === index ? 'true' : 'false');
    });

    let detailsHtml = step.details.map(d => `
      <li class="sequence-details-item">
        <span class="sequence-details-icon icon-${d.icon}">${d.icon === 'success' ? '✓' : d.icon === 'warn' ? '!' : 'i'}</span>
        <span>${d.text}</span>
      </li>
    `).join('');

    explanation.innerHTML = `
      <div style="display: inline-block; padding: 4px 10px; border-radius: 999px; background: rgba(0, 173, 216, 0.15); color: #00add8; font-size: 12px; font-weight: 600; margin-bottom: 12px;">
        ${step.badge}
      </div>
      <h4>${step.heading}</h4>
      <p>${step.desc}</p>
      <ul class="sequence-details-list">
        ${detailsHtml}
      </ul>
      <div style="display: flex; gap: 12px; margin-top: 20px;">
        <button type="button" class="copy-btn" id="seq-prev-btn" ${index === 0 ? 'disabled style="opacity:0.3;cursor:not-allowed;"' : ''}>← Previous</button>
        <button type="button" class="copy-btn" id="seq-next-btn" style="background: rgba(0, 173, 216, 0.2); border-color: #00add8; color: #38bdf8;" ${index === VERIFICATION_STEPS.length - 1 ? 'disabled style="opacity:0.3;cursor:not-allowed;"' : ''}>Next Step →</button>
      </div>
    `;

    const prevBtn = document.getElementById('seq-prev-btn');
    const nextBtn = document.getElementById('seq-next-btn');
    if (prevBtn && index > 0) prevBtn.addEventListener('click', () => renderStep(index - 1));
    if (nextBtn && index < VERIFICATION_STEPS.length - 1) nextBtn.addEventListener('click', () => renderStep(index + 1));

    if (codeTitle) codeTitle.textContent = step.codeTitle;
    codeContent.textContent = step.jsonPayload;
  }

  const buttons = nav.querySelectorAll('.seq-tab-btn');
  buttons.forEach((btn, i) => {
    btn.addEventListener('click', () => renderStep(i));
  });

  renderStep(0);
}

/* --------------------------------------------------------------------------
   Generic Tab Selectors
   -------------------------------------------------------------------------- */
function initTabSelectors() {
  document.querySelectorAll('[data-tabs]').forEach(tabContainer => {
    const tabs = tabContainer.querySelectorAll('.tab-btn');
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
        const pre = btn.closest('.quickstart-wrapper, .sequence-code-pane, .mac-window, pre, .terminal-body');
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
        navLinks.style.top = 'var(--header-height)';
        navLinks.style.left = '0';
        navLinks.style.right = '0';
        navLinks.style.background = 'var(--color-snow)';
        navLinks.style.flexDirection = 'column';
        navLinks.style.padding = 'var(--space-20)';
        navLinks.style.borderBottom = '1px solid var(--color-silver-mist)';
      }
    });
  }
}

/* --------------------------------------------------------------------------
   WebMCP In-Browser Tool Registry (Level 5 Agent Native)
   -------------------------------------------------------------------------- */
function initWebMCP() {
  const agentTools = [
    {
      name: "getQuickstart",
      description: "Returns ready-to-run installation commands, brew formula, and MCP config for agentic-go.",
      inputSchema: { type: "object", properties: {} },
      execute: async () => ({
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
      })
    },
    {
      name: "searchDocumentation",
      description: "Search agentic-go documentation topics, routes, and capabilities.",
      inputSchema: {
        type: "object",
        properties: {
          query: { type: "string", description: "Search keyword or topic" }
        },
        required: ["query"]
      },
      execute: async ({ query }) => {
        const q = String(query || "").toLowerCase();
        const routes = [
          { title: "Overview & Mental Model", path: "/go/docs/", keywords: "architecture, tools, gopls, model, snapshot" },
          { title: "Installation Guide", path: "/go/docs/install/", keywords: "brew, homebrew, curl, linux, macos, darwin, arm64, amd64" },
          { title: "MCP Client Connection", path: "/go/docs/connect/", keywords: "claude, cursor, cline, claude code, stdio, mcp-config" },
          { title: "Verification Engine", path: "/go/docs/verify/", keywords: "verify, test, race, coverage, findings, uncertainty, exit codes" },
          { title: "Safety & Containment", path: "/go/docs/safety/", keywords: "containment, sandbox, git, mutation, permissions, security" },
          { title: "GitHub Action in CI", path: "/go/docs/action/", keywords: "github actions, pr, pull request, ci, workflow" },
          { title: "Protocol Contracts & Schemas", path: "/go/docs/contracts/", keywords: "schemas, context/v1, change/v1, verify/v1, 14 tools" },
          { title: "Frequently Asked Questions", path: "/go/docs/faq/", keywords: "faq, questions, gopls, sandboxing, licenses" }
        ];
        return routes.filter(r => r.title.toLowerCase().includes(q) || r.keywords.includes(q) || r.path.includes(q));
      }
    },
    {
      name: "getToolCatalog",
      description: "Returns the frozen 14 MCP tools, 7 resources, and 6 prompts in agentic-go v1.0.0.",
      inputSchema: { type: "object", properties: {} },
      execute: async () => ({
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
      })
    },
    {
      name: "getVerificationReportSample",
      description: "Returns a canonical agentic.verify/v1 verification report payload with evidence and uncertainty.",
      inputSchema: { type: "object", properties: {} },
      execute: async () => ({
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
      })
    }
  ];

  const modelContext = {
    getTools: async () => agentTools.map(t => ({
      name: t.name,
      description: t.description,
      inputSchema: t.inputSchema
    })),
    executeTool: async (name, input = {}) => {
      const tool = agentTools.find(t => t.name === name);
      if (!tool) throw new Error(`Tool "${name}" not found in agentic-go WebMCP registry.`);
      return await tool.execute(input);
    },
    registerTool: (tool) => {
      agentTools.push(tool);
    }
  };

  if (typeof window !== 'undefined') window.modelContext = modelContext;
  if (typeof navigator !== 'undefined' && !navigator.modelContext) {
    try {
      Object.defineProperty(navigator, 'modelContext', { value: modelContext, writable: true, configurable: true });
    } catch {}
  }
  if (typeof document !== 'undefined' && !document.modelContext) {
    try {
      Object.defineProperty(document, 'modelContext', { value: modelContext, writable: true, configurable: true });
    } catch {}
  }
}
