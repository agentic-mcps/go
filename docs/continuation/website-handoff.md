# Website Architecture & Handoff Reference

## Overview

The `agentic-go` product site and documentation hub are located within the `/site` directory of the `agentic-mcps/go` repository and published automatically through GitHub Pages at:

`https://agentic-mcps.github.io/go/`

## Directory Structure

```text
site/
├── index.html                   # Landing page (hero, quickstart, workflow, central interactive explorer, capabilities, evidence)
├── styles.css                   # Core design system & theme (dark-first, typography, tactile controls, accessibility, reduced motion)
├── app.js                       # Verification sequence interactive explorer, tab selectors, clipboard copy, mobile navigation
├── robots.txt                   # Search crawler directives and sitemap pointer
├── sitemap.xml                  # XML sitemap covering all 9 canonical routes
├── 404.html                     # Custom 404 error page
├── assets/                      # Self-contained brand assets, diagrams, and preview images
│   ├── brand/
│   │   ├── project-mark.svg
│   │   ├── social-preview.png
│   │   ├── organization-avatar.png
│   │   └── pills/
│   └── diagrams/
│       ├── agentic-go-loop.svg
│       └── trust-boundary.svg
└── docs/
    ├── index.html               # /go/docs/ - Documentation Overview & Core Concepts
    ├── install/index.html       # /go/docs/install/ - Installation (Brew, curl, binaries, Go 1.25–1.27)
    ├── connect/index.html       # /go/docs/connect/ - MCP Client Setup (Claude, Cursor, Cline, Claude Code)
    ├── verify/index.html        # /go/docs/verify/ - Verification Engine & CLI usage
    ├── safety/index.html        # /go/docs/safety/ - Safety, Containment & Trust boundaries
    ├── action/index.html        # /go/docs/action/ - GitHub Action in CI
    ├── contracts/index.html     # /go/docs/contracts/ - Protocol contracts, 14 tools & 3 schemas
    └── faq/index.html           # /go/docs/faq/ - Frequently Asked Questions
```

## GitHub Pages Deployment

Deployment is managed by `.github/workflows/pages.yml`:
- Triggers on push to `main` when files in `site/**` or `.github/workflows/pages.yml` change, or via manual `workflow_dispatch`.
- Concurrency group `pages` with `cancel-in-progress: false` to prevent race conditions during deployment.
- Deploys the self-contained `site/` folder directly using GitHub Pages official actions (`actions/upload-pages-artifact@v3` and `actions/deploy-pages@v4`).

## Google Search Console Verification

To verify site ownership on Search Console for property `https://agentic-mcps.github.io/go/`:
1. Obtain the verification HTML file or meta tag from Search Console.
2. If using HTML file verification: place the Google HTML file in `site/` (e.g. `site/google<token>.html`).
3. If using meta tag verification: uncomment and populate the `<meta name="google-site-verification" content="..." />` tag in `site/index.html` and docs templates.
4. Commit and push to `main`. Once GitHub Pages publishes, complete verification in Search Console and submit `https://agentic-mcps.github.io/go/sitemap.xml`.

## Local Testing & Quality Checklist

Before shipping changes to `/site`:
1. Verify static routes load cleanly using any local HTTP server (e.g., Python `http.server` or Node `serve`).
2. Verify all relative links, images, and script paths work without 404s.
3. Test keyboard navigation and tab selection across all code blocks and the interactive verification explorer.
4. Test `@media (prefers-reduced-motion: reduce)` in browser devtools to ensure smooth fallbacks.
5. Run repository verification gates:
   - `go test ./...`
   - `go test -race ./...`
   - `go vet ./...`
   - `go build ./...`
   - `git diff --check`
