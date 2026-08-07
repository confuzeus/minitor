---
name: design-guidelines
description: "Design guidelines for Minitor. Use it when you need to make design decisions such as building or updating layouts, styling, or other design-related tasks."
---

## Minitor Design Guidelines v1.0

**Core Design Philosophy: Calm Control**
Solo founders live in a state of high alert. Minitor’s UI should be the calm, quiet corner of their digital life. It should provide maximum information clarity with minimal visual noise. The interface should feel lightweight, fast, and trustworthy. Every pixel should serve a purpose—no decorative elements, just pure utility presented beautifully.

### 1. Color Palette

The palette is anchored in a deep, slate-blue neutral to convey stability, paired with a bright, optimistic coral accent that represents proactive health and action. Status colors are functionally distinct and accessible.

| Token Name              | Hex Code  | Usage                                                                                                  |
| :---------------------- | :-------- | :----------------------------------------------------------------------------------------------------- |
| **Page Background**     | `#F8F9FB` | Main app background, creates a soft, paper-like feel.                                                  |
| **Surface Primary**     | `#FFFFFF` | Cards, panels, table headers. Clean and crisp.                                                         |
| **Surface Secondary**   | `#EEF1F5` | Sidebar, inactive tabs, input backgrounds. Subtle differentiation.                                     |
| **Border Default**      | `#DCE1E8` | Dividers, card borders, input outlines. High-contrast enough to define space.                          |
| **Text Primary**        | `#1E293B` | Headings, key body text. A deep, not pure black, slate for readability.                                |
| **Text Secondary**      | `#64748B` | Supporting text, labels, helper text.                                                                  |
| **Text Disabled**       | `#A0AEC0` | Placeholder text, disabled controls.                                                                   |
| **Interactive Primary** | `#FF6B57` | Primary buttons, selected states, active links. An energetic coral for a single, clear call-to-action. |
| **Interactive Hover**   | `#E55A48` | Hover state for the primary coral, 10% darker.                                                         |
| **Interactive Focus**   | `#FF6B57` | Focus ring, using a 3px outline with 40% opacity.                                                      |
| **Status Up/Healthy**   | `#10B981` | Endpoint status `UP`, success toasts, uptime charts. A vibrant, emerald green.                         |
| **Status Down/Error**   | `#EF4444` | Endpoint status `DOWN`, error toasts, critical metrics. A direct, clear red.                           |
| **Status Degraded**     | `#F59E0B` | Endpoint status `DEGRADED`, warnings, high latency. An amber for caution.                              |
| **Status Unknown**      | `#A0AEC0` | Initial state, paused monitors. Muted and neutral.                                                     |

**Palette Usage Principles:**

- **Backgrounds:** The UI is predominantly `Page Background` and `Surface Primary` to keep it light and fast-loading.
- **Text:** Strict hierarchy. `Text Primary` for all content, `Text Secondary` for meta-information like URLs or timestamps. Never use placeholders as labels.
- **Accent:** Use `Interactive Primary` sparingly as the single source of visual energy. Only one primary button per section. It’s the visual equivalent of a "push-to-start" button.
- **Status:** Status colors are for data, not decoration. They should only appear on status indicators, charts, and badges.

### 2. Typography

The font stack prioritizes legibility, fast loading, and a modern but neutral personality.

- **Font Family:** `'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif`
- **Rationale:** Inter is meticulously designed for screen legibility at small sizes, which is critical for a monitoring dashboard. It’s available as a variable font, keeping the single-binary philosophy lean.

**Type Scale:**

| Token Name        | Size / Line-height | Font Weight    | Usage                                                                                       |
| :---------------- | :----------------- | :------------- | :------------------------------------------------------------------------------------------ |
| `text-hero`       | 30px / 38px        | SemiBold (600) | Key dashboard metric (e.g., current overall uptime %).                                      |
| `text-heading`    | 20px / 28px        | SemiBold (600) | Card titles, modal headers.                                                                 |
| `text-subheading` | 16px / 24px        | Medium (500)   | Section titles within a card.                                                               |
| `text-body`       | 14px / 20px        | Regular (400)  | Primary body copy, table data, endpoint URLs.                                               |
| `text-small`      | 12px / 18px        | Medium (500)   | Labels, badges, timestamps, chart axes.                                                     |
| `text-code`       | 13px / 20px        | Regular (400)  | For monospaced data like IPs, ports, codes. Use `'JetBrains Mono', monospace` in the stack. |

### 3. Spacing & Layout System

A tight, 4px-based spacing system keeps the layout dense with information but never cramped. Solo founders value information density.

- **Base Unit:** 4px.
- **Scale:** `4, 8, 12, 16, 20, 24, 32, 40, 48`.
- **Layout:** A single-column, max-width container (1200px) for the configuration flow. A full-width, card-based grid for the dashboard. A persistent, collapsible left sidebar for main navigation.
- **Dashboard Grid:** Use a CSS Grid with `repeat(auto-fill, minmax(320px, 1fr))` for the endpoint status cards. This gives the solo founder a flexible, resilient overview whether they're monitoring 2 or 20 endpoints.
- **Component Padding:** Buttons get `8px 16px`. Cards get `24px` internal padding. Inputs get `8px 12px`. This consistency creates a structured, rational rhythm.

### 4. Core Component Principles

**Endpoint Status Card:**
The heart of the UI. A simple white surface with `Border Default`. It shows: Name, URL, Current Status (as a colored dot + text), and a sparkline of the last 24h response times. On hover, a subtle shadow elevates the card, and a single click-action appears (e.g., “View Details”). No frantic red flashing on failure—a solid, calm `Status Down` indicator with the exact downtime duration is more actionable.

**Health Check Configuration Form:**
Layout is strictly single-column for focus. Use clear, verb-led labels (“Check URL,” “Interval”). Advanced settings are hidden behind an expandable “Options” toggle to keep the default view clean. A small “Test” button next to the save button provides immediate, risk-free validation.

**Status Badge:**
A small pill shape (`border-radius: 12px`) with a 12px font. Uses a `10%` tint background of the status color and the status color itself for text and a matching dot. No text shadow, no gradients. Pure, flat information.

**Empty State:**
For a new user with zero endpoints configured, show a central, calm illustration-free panel. The text: “Monitor your first endpoint.” A single, coral “Add Endpoint” primary button. Below it, a `text-small` code snippet: `$ ./minitor check https://your-saas.com`. This directly connects the UI to the tool’s binary nature.

**Error/Down State:**
Never show raw stack traces. A dedicated card with the `Status Down` color on its left border will state: “Your endpoint is returning a 500 status code.” and provide a timestamp. A “View Recent Logs” button provides a path to diagnostics. The tone is informative, not alarming.

### 5. Imagery & Iconography

- **Icons:** Use a consistent open-source icon set, preferably `lucide` or `phosphor-icons`. They’re clean, stroke-based, and scale perfectly on a 24px grid.
- **No Illustrations:** A self-hosted, single-binary tool should not waste bandwidth on hero illustrations. Empty states use typography and spacing only.
- **Charts:** Tiny, text-based sparklines for trends. Simple donut or bar charts for aggregate data. Use a 2px stroke, no distracting gridlines, and subtle color fills. The goal is to show a trend in a 60x24 pixel space.
