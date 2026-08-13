---
name: ProjectBoard
description: A calm, precise operations workspace for auditable human and Agent work.
colors:
  ink: "#17191f"
  ink-soft: "#606571"
  canvas: "#f4f5f7"
  surface: "#ffffff"
  surface-subtle: "#f8f9fb"
  line: "#dfe2e8"
  line-strong: "#c9cdd6"
  accent: "#536fd7"
  accent-soft: "#e9edfb"
  accent-ink: "#304cae"
  danger: "#b84646"
  health: "#2f8065"
typography:
  display:
    fontFamily: "Segoe UI, Microsoft YaHei UI, PingFang SC, sans-serif"
    fontSize: "clamp(2rem, 3vw, 3rem)"
    fontWeight: 760
    lineHeight: 1.05
    letterSpacing: "-0.03em"
  body:
    fontFamily: "Segoe UI, Microsoft YaHei UI, PingFang SC, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 450
    lineHeight: 1.6
  label:
    fontFamily: "Segoe UI, Microsoft YaHei UI, PingFang SC, sans-serif"
    fontSize: "0.6875rem"
    fontWeight: 650
    lineHeight: 1.3
rounded:
  control: "6px"
  surface: "10px"
  pill: "999px"
spacing:
  xs: "4px"
  sm: "8px"
  md: "16px"
  lg: "24px"
  xl: "40px"
  page: "56px"
---

# Design System: ProjectBoard

## Creative North Star: The Quiet Control Room

ProjectBoard is an engineering operations surface used for long sessions under ordinary office light. It should feel calm, factual, and immediately scannable: a fixed white navigation rail beside a cool-gray workspace, with one restrained blue signal for selection and action. Product truth comes from state, responsibility, evidence, and history—not decoration.

## Visual language

- Use generous whitespace, hairline dividers and strong alignment. Cards are reserved for bounded objects such as Kanban tasks, status summaries and grouped settings—not used as page-level decoration.
- Use near-black headings and conventional UI typography. Use the mono stack only for machine-addressable data such as IDs, paths, branches and logs.
- Reserve blue for selection, focus, primary actions and live state. Use green for healthy or complete state and red only for danger or blocking.
- Use Lucide icons throughout. Do not use emoji as interface icons.
- Resting content is flat; drawers and transient protected layers may use restrained elevation.

## Layout and routes

Desktop uses a 228 px full-height navigation rail and a cool-gray workspace with 56 px page gutters. At 840 px the rail becomes compact navigation; below 620 px secondary metadata collapses while primary identity, state and actions remain reachable.

The primary project routes are task queue, knowledge, Agent requests and messages. The settings route exposes account plus five administrator sections: local Agents, users, projects, audit and system service. Settings tabs remain horizontally reachable at narrow widths.

## Components

### Navigation and tabs

The active destination uses a pale accent field with accent icon and text. Hover uses a subtle neutral fill. Tabs communicate selection with text, border and shape rather than color alone, and must not fall back to browser-native button styling.

### Task board and detail

The standard task board has four columns: Created, In progress, Completed and Closed. Simple-conversation tasks skip Completed. Blocking is an independent warning and never replaces stage.

Task cards prioritize number, title, priority, assignee and blocking state. Full task detail uses a two-part workspace: chronological discussion/execution evidence and a stable context/configuration area for description, acceptance, relationships and actions.

### Knowledge and Agent requests

Knowledge uses a tree/document split view with search, node actions, lock state and revision history. Agent requests use a durable queue/list plus detail view for request state, lineage, final result, errors and raw logs.

### Management lists and drawers

Users, Agents, projects and audit events use full-width rows with hairline separators. A right-side drawer preserves list context for entity details and editing. Project drawers organize automation, repository and member settings; dangerous actions remain visually and spatially separate.

### Buttons and fields

Primary buttons use the accent color and white text. Secondary buttons use a neutral border. Inputs are at least 44 px high, with a visible focus ring. Disabled controls remain legible and clearly inactive.

## Motion and accessibility

Motion is limited to context changes: drawer entry, hover affordances and short state transitions. `prefers-reduced-motion` removes transform-based motion.

Keyboard focus is always visible. Status never relies on color alone. Text and controls target WCAG AA contrast. Drawers retain dialog semantics and close through a labeled control, backdrop or Escape. Responsive layouts preserve labels, scrollable tabs and reachable actions.
